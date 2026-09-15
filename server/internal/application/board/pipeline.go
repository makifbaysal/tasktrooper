package board

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	githubapi "github.com/makifbaysal/tasktrooper/server/internal/adapter/github"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/rs/zerolog/log"
)

// QADispatcher hands a built task off to its review stage. gateReason is "" on
// the ordinary path and a domain.PipelineGateReason* code when the gate was
// opened without a result (see Dispatcher.DispatchQA).
type QADispatcher interface {
	DispatchQA(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask, pipelineID uuid.UUID, gateReason string) error
}

// TokenSource returns the stored GitHub token ("" = not connected).
type TokenSource func(ctx context.Context) (string, error)

// StageVerifier stamps the task whose stage deploy just succeeded. It is what
// opens the production gate for a schema-changing task: the migration is only
// trusted once it has actually applied somewhere.
type StageVerifier interface {
	MarkTaskStageVerified(ctx context.Context, taskID uuid.UUID) error
}

// StoreSubmitter records that a prod deploy handed a build to an app store
// for review. The store-shipping prod workflows (mobile-prod-app-store.yml,
// mobile-prod-google-play.yml) ARE the submit, and nothing else in the system
// writes an open review_state — without this call the release monitor's whole
// review-polling path never runs.
type StoreSubmitter interface {
	MarkSubmitted(ctx context.Context, repositoryID uuid.UUID, platform, version string) error
}

// TaskRunbookReader re-reads a task when its deploy finishes. The pipeline job
// carries the task as it looked when the deploy was queued, which can be half
// an hour earlier — long enough for someone to have written or corrected the
// post-deploy steps that are about to be posted.
type TaskRunbookReader interface {
	GetTask(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.BoardTask, error)
}

// IncidentReporter records a failed deploy as a production incident. A deploy
// that dies half-way is a production event even when no external alert fires,
// and the incident is what carries the rollback proposal.
type IncidentReporter interface {
	IngestDeployFailure(ctx context.Context, repositoryID uuid.UUID, env, taskKey, detail string) (domain.Incident, error)
}

// PipelineGit is the slice of the git client the pipeline needs to locate a
// task's GitHub Actions runs and open its PR.
type PipelineGit interface {
	TaskGitInfo(ctx context.Context, workspacePath string) (domain.TaskGitInfo, error)
	EnsurePullRequest(ctx context.Context, workspacePath string) (string, error)
	// PushBranch publishes the task branch so the PR can exist at all. The
	// developer's run pushes at its end; a pipeline that started from the column
	// move can get here first.
	PushBranch(ctx context.Context, workspacePath string) error
}

const (
	pipelinePollInterval = 20 * time.Second
	pipelineMaxWait      = 30 * time.Minute
	jobLogTailLimit      = 10000
)

// PipelineRunnerDeps wires the collaborators the GitHub Actions-backed pipeline
// runner needs.
type PipelineRunnerDeps struct {
	Store port.TaskPipelineStore
	Jobs  port.RepositoryPipelineJobStore
	// DeployTargets tells a finished prod deploy which provider it deployed
	// to — the only way to know a run was an app store submit. Optional.
	DeployTargets port.DeployTargetStore
	Repos         RepositoryResolver
	Tasks         TaskUpdater
	QA            QADispatcher
	Git           PipelineGit
	Tokens        TokenSource
	WorkspaceRoot string
	// TaskPRs records the pull request this path opens for a task. Optional; a
	// build without a board task store still opens PRs, it just cannot remember
	// which one belongs to which task.
	TaskPRs TaskPRRecorder
}

// pipelineJob is the unit of work handed from Trigger to the worker pool.
type pipelineJob struct {
	Pipeline     domain.TaskPipeline
	RepositoryID uuid.UUID
	Task         domain.BoardTask
}

// PipelineRunner reads the QA-gate build/test results (and dispatches deploy
// workflows) from GitHub Actions in the background, off the request path.
type PipelineRunner struct {
	store         port.TaskPipelineStore
	jobs          port.RepositoryPipelineJobStore
	deployTargets port.DeployTargetStore
	repos         RepositoryResolver
	tasks         TaskUpdater
	qa            QADispatcher
	git           PipelineGit
	tokens        TokenSource
	workspaceRoot string

	queue  chan pipelineJob
	wg     sync.WaitGroup
	cancel context.CancelFunc

	triggerMu sync.Mutex

	// inflight is the set of pipeline ids this process is currently polling,
	// keyed by uuid.UUID with an empty value. PipelineGateSweeper reads it to
	// leave those alone: they already have somebody watching them, and a second
	// resolver would write a second set of job rows for the same run.
	inflight sync.Map

	incidents  IncidentReporter
	stage      StageVerifier
	stores     StoreSubmitter
	prRecorder TaskPRRecorder
	taskReader TaskRunbookReader
	// bounces refuses a second bounce off the SAME failed commit. Nil keeps the
	// old behaviour: every failed pipeline sends the task back, however many
	// times the identical commit has already done so.
	bounces *PipelineBounceGuard
}

// SetBounceGuard enables the same-commit re-bounce brake (see
// PipelineBounceGuard). Optional, and late-wired like the setters above because
// the parker it needs is the board task store, which is assembled elsewhere.
func (p *PipelineRunner) SetBounceGuard(g *PipelineBounceGuard) { p.bounces = g }

// SetStageVerifier enables the stage-deploy → migration-gate stamp.
func (p *PipelineRunner) SetStageVerifier(v StageVerifier) { p.stage = v }

// SetTaskReader enables the post-deploy runbook comment. Optional: without it a
// successful prod deploy still releases the task, it just does not surface the
// task's after_deploy steps.
func (p *PipelineRunner) SetTaskReader(r TaskRunbookReader) { p.taskReader = r }

// SetIncidentReporter enables deploy-failure → incident feedback. Optional:
// without it a failed deploy only bounces the task, as before.
func (p *PipelineRunner) SetIncidentReporter(r IncidentReporter) { p.incidents = r }

// SetStoreSubmitter enables the prod-deploy → store-review handoff. Optional:
// without it (and without Deps.DeployTargets) a store prod deploy still
// releases the task, it just isn't tracked through review.
func (p *PipelineRunner) SetStoreSubmitter(s StoreSubmitter) { p.stores = s }

func NewPipelineRunner(deps PipelineRunnerDeps) *PipelineRunner {
	return &PipelineRunner{
		store:         deps.Store,
		jobs:          deps.Jobs,
		deployTargets: deps.DeployTargets,
		repos:         deps.Repos,
		tasks:         deps.Tasks,
		qa:            deps.QA,
		git:           deps.Git,
		tokens:        deps.Tokens,
		workspaceRoot: deps.WorkspaceRoot,
		prRecorder:    deps.TaskPRs,
		queue:         make(chan pipelineJob, 64),
	}
}

// Trigger (re-)starts the QA-gate pipeline for a task: supersedes any pending
// pipeline, creates a new pending one, and enqueues it. Manual/retry triggers
// are rejected while a pipeline is still pending/running for the task.
func (p *PipelineRunner) Trigger(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask, trigger domain.PipelineTrigger) (domain.TaskPipeline, error) {
	return p.trigger(ctx, repositoryID, task, trigger, true)
}

// TriggerDeploy records and enqueues a stage/prod deploy pipeline. Unlike the
// QA gate it does not supersede other pipelines (a deploy is additive), but it
// still rejects a second concurrent deploy of the same kind.
func (p *PipelineRunner) TriggerDeploy(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask, trigger domain.PipelineTrigger) (domain.TaskPipeline, error) {
	return p.trigger(ctx, repositoryID, task, trigger, false)
}

func (p *PipelineRunner) trigger(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask, trigger domain.PipelineTrigger, supersede bool) (domain.TaskPipeline, error) {
	p.triggerMu.Lock()

	if trigger == domain.PipelineTriggerManual || trigger == domain.PipelineTriggerRetry {
		if latest, latestErr := p.store.LatestByTask(ctx, task.ID); latestErr == nil {
			if latest.Status == domain.PipelineStatusPending || latest.Status == domain.PipelineStatusRunning {
				p.triggerMu.Unlock()
				return domain.TaskPipeline{}, domain.ErrPipelineActive
			}
		} else if !errors.Is(latestErr, domain.ErrPipelineNotFound) {
			log.Warn().Err(latestErr).Str("task_id", task.ID.String()).Msg("check active pipeline before trigger failed")
		}
	}

	if supersede {
		if err := p.store.SupersedePending(ctx, task.ID); err != nil {
			log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("supersede pending pipelines failed")
		}
	}

	created, err := p.store.Create(ctx, domain.TaskPipeline{
		TaskID:       task.ID,
		RepositoryID: repositoryID,
		Trigger:      trigger,
		Status:       domain.PipelineStatusPending,
	})
	p.triggerMu.Unlock()
	if err != nil {
		return domain.TaskPipeline{}, err
	}

	select {
	case p.queue <- pipelineJob{Pipeline: created, RepositoryID: repositoryID, Task: task}:
	default:
		log.Warn().Str("task_id", task.ID.String()).Msg("pipeline queue full, dropping job")
	}

	return created, nil
}

// Start fails every pipeline left pending/running from a previous process, then
// starts the worker pool.
func (p *PipelineRunner) Start(ctx context.Context) {
	ctx, p.cancel = context.WithCancel(ctx)

	// No boot-time FailStaleRunning any more, and its removal is the point.
	//
	// It ran with a cutoff of zero minutes — "fail every pending or running
	// pipeline" — on the premise that a process starting up was
	// the only process there was, so anything unfinished had been abandoned by
	// the process it replaced. With several replicas that premise inverts into
	// the worst thing a new pod can do: every deploy would mark every pipeline
	// its siblings were actively polling as 'interrupted', bouncing live cards
	// to need_revision on somebody else's behalf.
	//
	// Nothing is lost by dropping it. PipelineGateSweeper already walks every
	// unfinished pipeline, asks GitHub what actually happened and opens the
	// gate with a reason when nothing can answer — a genuinely abandoned
	// pipeline is recovered there, with evidence, instead of here, by
	// assumption.
	for i := 0; i < 2; i++ {
		p.wg.Add(1)
		go p.worker(ctx)
	}
}

func (p *PipelineRunner) Stop() {
	if p.cancel != nil {
		p.cancel()
	}
	p.wg.Wait()
}

func (p *PipelineRunner) worker(ctx context.Context) {
	defer p.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-p.queue:
			if err := p.execute(ctx, job); err != nil {
				log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("pipeline execution failed")
			}
		}
	}
}

// execute drives a pipeline: it marks it running, resolves the task's GitHub
// coordinates, then either reads the QA-gate jobs (validate/build/test) or
// dispatches+watches a deploy workflow, persisting job/pipeline state and
// firing side effects (QA dispatch on gate success, need_revision on failure).
func (p *PipelineRunner) execute(ctx context.Context, job pipelineJob) error {
	pipeline := job.Pipeline

	// Claim the pipeline for this process. PipelineGateSweeper skips anything
	// claimed here, which is the whole division of labour between them: the
	// in-process poll is the fast path and owns every pipeline it started, the
	// sweeper exists for the ones nobody owns — started by a pod that has since
	// been replaced, or dropped when the queue was full.
	p.inflight.Store(pipeline.ID, struct{}{})
	defer p.inflight.Delete(pipeline.ID)

	// Freshness: a newer Trigger may have superseded this while it was queued.
	if current, getErr := p.store.Get(ctx, pipeline.ID); getErr != nil {
		log.Warn().Err(getErr).Str("pipeline_id", pipeline.ID.String()).Msg("pipeline freshness check failed; proceeding")
	} else if current.Status != domain.PipelineStatusPending {
		log.Info().Str("pipeline_id", pipeline.ID.String()).Str("status", string(current.Status)).Msg("pipeline no longer pending; skipping execution")
		return nil
	}

	startedAt := time.Now()
	pipeline.Status = domain.PipelineStatusRunning
	pipeline.StartedAt = &startedAt
	// Not `pipeline, err := …`: a failed Update returns the zero value, so
	// assigning through it made the very log line reporting the failure name
	// the all-zero uuid instead of the pipeline that failed.
	running, err := p.store.Update(ctx, pipeline)
	if err != nil {
		log.Warn().Err(err).Str("pipeline_id", pipeline.ID.String()).Msg("mark pipeline running failed")
		return err
	}
	pipeline = running

	gitInfo, gitErr := p.resolveGitInfo(ctx, job)
	if gitErr != nil {
		return p.finishNoWorkspace(ctx, job, pipeline, "could not resolve git info: "+gitErr.Error())
	}
	// The commit this pipeline is about, recorded the moment it is known. It is
	// the only coordinate anything outside this process can use afterwards: the
	// task workspace is deleted when the run ends, so a sweeper or a webhook
	// arriving later has no other way to ask GitHub what happened.
	pipeline = p.markHeadSHA(ctx, pipeline, gitInfo.HeadSHA)
	token := ""
	if p.tokens != nil {
		if t, terr := p.tokens(ctx); terr == nil {
			token = strings.TrimSpace(t)
		}
	}
	if token == "" {
		return p.finishNoWorkspace(ctx, job, pipeline, "GitHub is not connected — the pipeline cannot run")
	}

	var repo domain.Repository
	if fetched, repoErr := p.repos.ResolveRepository(ctx, job.RepositoryID); repoErr != nil {
		log.Warn().Err(repoErr).Str("repository_id", job.RepositoryID.String()).Msg("resolve repository for pipeline failed; using defaults")
	} else {
		repo = fetched
	}

	var mappings []domain.RepositoryPipelineJob
	if p.jobs != nil {
		if m, jerr := p.jobs.ListByRepository(ctx, job.RepositoryID); jerr != nil {
			log.Warn().Err(jerr).Str("repository_id", job.RepositoryID.String()).Msg("list pipeline job mappings failed")
		} else {
			mappings = m
		}
	}

	switch pipeline.Trigger {
	case domain.PipelineTriggerStageDeploy, domain.PipelineTriggerPreProdDeploy, domain.PipelineTriggerProdDeploy:
		return p.runDeploy(ctx, job, pipeline, repo, gitInfo, token, mappings)
	default:
		return p.runQAGate(ctx, job, pipeline, gitInfo, token, mappings)
	}
}

// resolveGitInfo ensures a PR exists (so validate/build/test read PR-linked
// runs) and returns the task branch's GitHub coordinates.
func (p *PipelineRunner) resolveGitInfo(ctx context.Context, job pipelineJob) (domain.TaskGitInfo, error) {
	if p.git == nil {
		return domain.TaskGitInfo{}, fmt.Errorf("git client is not configured")
	}
	dir, dirErr := workspace.TaskDir(p.workspaceRoot, job.Task.ID)
	if dirErr != nil {
		return domain.TaskGitInfo{}, dirErr
	}
	if _, statErr := os.Stat(dir); statErr != nil {
		return domain.TaskGitInfo{}, fmt.Errorf("task workspace not found: %s", dir)
	}
	// Open the PR if it is not already open: the QA gate wants PR-linked runs
	// and the reviewer after it reviews the PR, not a local branch. The usual
	// reason this fails is a branch origin has not seen yet — the developer's
	// run pushes at its end and the column move can beat it here — so publish
	// the branch and try once more. A failure after that is still non-fatal:
	// the Actions run may exist from the push event, and the code_review run
	// makes the missing PR explicit before any review happens.
	//
	// Whichever attempt produced a URL, the task records it: for most tasks this
	// is the earliest moment the PR exists, and the board must be able to name it
	// afterwards without re-deriving it from a working copy.
	if prURL, prErr := p.git.EnsurePullRequest(ctx, dir); prErr != nil {
		if pushErr := p.git.PushBranch(ctx, dir); pushErr != nil {
			log.Warn().Err(pushErr).Str("task_id", job.Task.ID.String()).Msg("publishing the task branch before the pipeline failed")
		} else if retryURL, retryErr := p.git.EnsurePullRequest(ctx, dir); retryErr != nil {
			log.Warn().Err(retryErr).Str("task_id", job.Task.ID.String()).Msg("ensure pull request before pipeline failed")
		} else {
			recordTaskPR(ctx, p.prRecorder, job.Task.ID, retryURL)
		}
	} else {
		recordTaskPR(ctx, p.prRecorder, job.Task.ID, prURL)
	}
	return p.git.TaskGitInfo(ctx, dir)
}

// runQAGate polls GitHub Actions for each mapped validate/build/test job on the
// task branch's HEAD commit, up to pipelineMaxWait.
func (p *PipelineRunner) runQAGate(ctx context.Context, job pipelineJob, pipeline domain.TaskPipeline, gitInfo domain.TaskGitInfo, token string, mappings []domain.RepositoryPipelineJob) error {
	// mutation_test rides along as a fourth job. It cannot redden the gate on its
	// own: the workflow side runs it with continue-on-error, so a surviving
	// mutant still concludes `success` and only shows up in the run summary.
	targets := filterMappings(mappings, domain.PipelineTargetJob,
		domain.PipelineCategoryValidate, domain.PipelineCategoryBuild,
		domain.PipelineCategoryTest, domain.PipelineCategoryMutationTest)

	if len(targets) == 0 {
		return p.finishNoChecks(ctx, job, pipeline, "no checks configured")
	}

	pipeline = p.markProvider(ctx, pipeline, domain.PipelineProviderGitHubActions)
	jobs, status := p.pollJobs(ctx, pipeline.ID, gitInfo, token, targets)
	if ctx.Err() != nil {
		return p.finishInterrupted(ctx, pipeline, jobs)
	}
	return p.finalize(ctx, job, pipeline, status, jobs)
}

// runDeploy dispatches every mapped deploy workflow for this stage/prod trigger
// and waits for their runs to conclude.
func (p *PipelineRunner) runDeploy(ctx context.Context, job pipelineJob, pipeline domain.TaskPipeline, repo domain.Repository, gitInfo domain.TaskGitInfo, token string, mappings []domain.RepositoryPipelineJob) error {
	category := domain.PipelineCategoryStageDeploy
	ref := gitInfo.Branch
	switch pipeline.Trigger {
	case domain.PipelineTriggerPreProdDeploy:
		category = domain.PipelineCategoryPreProdDeploy
		ref = deployRef(ctx, token, gitInfo, job.Task)
	case domain.PipelineTriggerProdDeploy:
		category = domain.PipelineCategoryProdDeploy
		ref = deployRef(ctx, token, gitInfo, job.Task)
	}
	targets := filterMappings(mappings, domain.PipelineTargetWorkflow, category)
	if len(targets) == 0 {
		return p.finishNoChecks(ctx, job, pipeline, "no "+category+" workflow configured")
	}

	pipeline = p.markProvider(ctx, pipeline, domain.PipelineProviderGitHubActions)
	jobs, status := p.pollDeploys(ctx, pipeline.ID, gitInfo, token, ref, targets)
	if ctx.Err() != nil {
		return p.finishInterrupted(ctx, pipeline, jobs)
	}
	return p.finalize(ctx, job, pipeline, status, jobs)
}

// markProvider persists where this run executes as soon as it is known, so the
// UI can show "GitHub Actions" while the pipeline is still running instead of
// only after it finishes. A write failure is non-fatal: the value is written
// again by finalize.
func (p *PipelineRunner) markProvider(ctx context.Context, pipeline domain.TaskPipeline, provider string) domain.TaskPipeline {
	if pipeline.Provider == provider {
		return pipeline
	}
	pipeline.Provider = provider
	updated, err := p.store.Update(context.WithoutCancel(ctx), pipeline)
	if err != nil {
		log.Warn().Err(err).Str("pipeline_id", pipeline.ID.String()).Msg("persist pipeline provider failed")
		return pipeline
	}
	updated.Jobs = pipeline.Jobs
	return updated
}

// markHeadSHA persists the commit a pipeline is about. Non-fatal on failure:
// the in-process poll already holds gitInfo and does not need the column — it
// is written for everything that arrives after this process is gone.
func (p *PipelineRunner) markHeadSHA(ctx context.Context, pipeline domain.TaskPipeline, headSHA string) domain.TaskPipeline {
	headSHA = strings.TrimSpace(headSHA)
	if headSHA == "" || pipeline.HeadSHA == headSHA {
		return pipeline
	}
	pipeline.HeadSHA = headSHA
	updated, err := p.store.Update(context.WithoutCancel(ctx), pipeline)
	if err != nil {
		log.Warn().Err(err).Str("pipeline_id", pipeline.ID.String()).Msg("persist pipeline head sha failed")
		return pipeline
	}
	updated.Jobs = pipeline.Jobs
	return updated
}

// mappingTarget is a flattened (label, ref) the pipeline must resolve.
type mappingTarget struct {
	label    string // job name shown in the pipeline (e.g. "backend:test")
	ref      string // GitHub job name, or workflow file
	category string // validate | build | test | ... — drives per-category behavior (e.g. coverage parsing)
}

func filterMappings(mappings []domain.RepositoryPipelineJob, targetKind string, categories ...string) []mappingTarget {
	want := map[string]bool{}
	for _, c := range categories {
		want[c] = true
	}
	var out []mappingTarget
	for _, m := range mappings {
		if m.TargetKind != targetKind || !want[m.Category] || m.TargetRef == "" {
			continue
		}
		label := m.Category
		if m.SubRepoKind != "" {
			label = m.SubRepoKind + ":" + m.Category
		}
		out = append(out, mappingTarget{label: label, ref: m.TargetRef, category: m.Category})
	}
	return out
}

// pollJobs waits until every target job appears in a HEAD-SHA run and completes
// (or the deadline passes). Returns the persisted jobs and overall status.
func (p *PipelineRunner) pollJobs(ctx context.Context, pipelineID uuid.UUID, gitInfo domain.TaskGitInfo, token string, targets []mappingTarget) ([]domain.TaskPipelineJob, domain.PipelineStatus) {
	deadline := time.Now().Add(pipelineMaxWait)
	for {
		byName := p.collectHeadJobs(ctx, gitInfo, token)
		if done, jobs, status := p.evaluate(ctx, pipelineID, gitInfo, token, targets, byName, time.Now().After(deadline)); done {
			return jobs, status
		}
		if !sleepCtx(ctx, pipelinePollInterval) {
			return nil, domain.PipelineStatusFailed
		}
	}
}

func (p *PipelineRunner) pollDeploys(ctx context.Context, pipelineID uuid.UUID, gitInfo domain.TaskGitInfo, token, ref string, targets []mappingTarget) ([]domain.TaskPipelineJob, domain.PipelineStatus) {
	dispatchedAt := time.Now()
	dispatchErr := map[string]string{}
	for _, t := range targets {
		if err := githubapi.DispatchWorkflow(ctx, token, gitInfo.Owner, gitInfo.Repo, t.ref, ref); err != nil {
			log.Warn().Err(err).Str("workflow", t.ref).Msg("dispatch deploy workflow failed")
			dispatchErr[t.ref] = err.Error()
		}
	}
	deadline := time.Now().Add(pipelineMaxWait)
	for {
		byName := p.collectDeployJobs(ctx, gitInfo, token, ref, targets, dispatchedAt)
		// A dispatch that errored and produced no run is a failed deploy, not
		// a reason to wait out the deadline (or worse, match an older run).
		for wf, msg := range dispatchErr {
			if _, ok := byName[wf]; !ok {
				byName[wf] = githubapi.RunJob{Name: wf, Status: "completed", Conclusion: "dispatch failed: " + msg}
			}
		}
		if done, jobs, status := p.evaluate(ctx, pipelineID, gitInfo, token, targets, byName, time.Now().After(deadline)); done {
			return jobs, status
		}
		if !sleepCtx(ctx, pipelinePollInterval) {
			return nil, domain.PipelineStatusFailed
		}
	}
}

// collectHeadJobs gathers every Actions job across the runs of the branch HEAD
// commit, keyed by job name (a completed job wins over an in-progress one).
func (p *PipelineRunner) collectHeadJobs(ctx context.Context, gitInfo domain.TaskGitInfo, token string) map[string]githubapi.RunJob {
	byName := map[string]githubapi.RunJob{}
	runs, err := githubapi.ListRunsByHeadSHA(ctx, token, gitInfo.Owner, gitInfo.Repo, gitInfo.HeadSHA)
	if err != nil {
		log.Warn().Err(err).Str("sha", gitInfo.HeadSHA).Msg("list runs by head sha failed")
		return byName
	}
	for _, run := range runs {
		runJobs, jerr := githubapi.ListRunJobs(ctx, token, gitInfo.Owner, gitInfo.Repo, run.ID)
		if jerr != nil {
			continue
		}
		for _, rj := range runJobs {
			mergeJob(byName, rj)
		}
	}
	return byName
}

// collectDeployJobs finds the dispatched deploy runs (by workflow file + branch,
// created at/after dispatch) and gathers their jobs keyed by the mapping label.
func (p *PipelineRunner) collectDeployJobs(ctx context.Context, gitInfo domain.TaskGitInfo, token, ref string, targets []mappingTarget, after time.Time) map[string]githubapi.RunJob {
	byName := map[string]githubapi.RunJob{}
	for _, t := range targets {
		runs, err := githubapi.ListWorkflowRunsByFileBranch(ctx, token, gitInfo.Owner, gitInfo.Repo, t.ref, ref)
		if err != nil || len(runs) == 0 {
			continue
		}
		// Runs are newest-first, but "most recent" is not enough: when the
		// dispatch failed or the run hasn't appeared yet, runs[0] is some
		// OLDER run of the same workflow, and treating its success as ours
		// would stamp a deploy that never happened. Only accept a run created
		// at/after our dispatch (small slack for clock skew).
		var run githubapi.WorkflowRun
		found := false
		for _, cand := range runs {
			if !cand.CreatedAt.Before(after.Add(-time.Minute)) {
				run = cand
				found = true
				break
			}
		}
		if !found {
			continue
		}
		runJobs, jerr := githubapi.ListRunJobs(ctx, token, gitInfo.Owner, gitInfo.Repo, run.ID)
		if jerr != nil || len(runJobs) == 0 {
			// No jobs yet: represent the run itself as an in-progress job under
			// the mapping label so evaluate() keeps waiting.
			byName[t.ref] = githubapi.RunJob{Name: t.ref, Status: run.Status, Conclusion: run.Conclusion, HTMLURL: run.HTMLURL}
			continue
		}
		// Fold the run's jobs into a single result keyed by the workflow file:
		// failed if any job failed, success only if all completed successfully.
		byName[t.ref] = foldRunJobs(t.ref, run, runJobs)
	}
	return byName
}

// evaluate checks whether all targets are terminal. When done it persists the
// job rows and returns the overall status; otherwise (done=false) polling
// continues unless timedOut, in which case unresolved targets fail.
func (p *PipelineRunner) evaluate(ctx context.Context, pipelineID uuid.UUID, gitInfo domain.TaskGitInfo, token string, targets []mappingTarget, byName map[string]githubapi.RunJob, timedOut bool) (bool, []domain.TaskPipelineJob, domain.PipelineStatus) {
	allTerminal := true
	for _, t := range targets {
		rj, ok := byName[t.ref]
		if !ok || rj.Status != "completed" {
			allTerminal = false
			break
		}
	}
	if !allTerminal && !timedOut {
		return false, nil, ""
	}

	jobs := make([]domain.TaskPipelineJob, 0, len(targets))
	status := domain.PipelineStatusSuccess
	for i, t := range targets {
		rj, ok := byName[t.ref]
		pj := domain.TaskPipelineJob{
			PipelineID: pipelineID,
			Name:       t.label,
			Command:    t.ref,
			Position:   i,
			RunURL:     rj.HTMLURL,
		}
		switch {
		case !ok:
			pj.Status = domain.PipelineJobStatusFailed
			pj.Output = "GitHub Actions job/workflow not found: " + t.ref
			status = domain.PipelineStatusFailed
		case rj.Status != "completed":
			pj.Status = domain.PipelineJobStatusFailed
			pj.Output = "timed out: the job did not finish within " + pipelineMaxWaitLabel
			status = domain.PipelineStatusFailed
		case rj.Conclusion == "success":
			pj.Status = domain.PipelineJobStatusSuccess
			if rj.StartedAt != nil && rj.CompletedAt != nil {
				pj.DurationMS = rj.CompletedAt.Sub(*rj.StartedAt).Milliseconds()
			}
			if t.category == domain.PipelineCategoryTest {
				pj.CoveragePct = p.testCoverage(ctx, gitInfo, token, rj)
			}
		default:
			pj.Status = domain.PipelineJobStatusFailed
			pj.Output = p.jobLog(ctx, gitInfo, token, rj)
			status = domain.PipelineStatusFailed
		}
		jobs = append(jobs, p.persistJob(ctx, pj))
	}
	return true, jobs, status
}

func (p *PipelineRunner) jobLog(ctx context.Context, gitInfo domain.TaskGitInfo, token string, rj githubapi.RunJob) string {
	if rj.ID == 0 {
		return "job failed (conclusion=" + rj.Conclusion + ")"
	}
	logs, err := githubapi.GetJobLogs(ctx, token, gitInfo.Owner, gitInfo.Repo, rj.ID)
	if err != nil {
		return "job failed (conclusion=" + rj.Conclusion + "); could not fetch logs: " + err.Error()
	}
	if len(logs) > jobLogTailLimit {
		logs = truncateTail(logs, jobLogTailLimit)
	}
	return strings.TrimSpace(logs)
}

// testCoverage fetches a successful test job's log and parses a coverage
// percentage from it. The failure path already fetches the log for the job's
// Output; a success needs its own fetch here since nothing else reads a
// successful job's log, and (per output semantics) it must not be stored
// there. A fetch failure only means "no coverage reported" — it must not fail
// the otherwise-successful job or pipeline.
func (p *PipelineRunner) testCoverage(ctx context.Context, gitInfo domain.TaskGitInfo, token string, rj githubapi.RunJob) *float64 {
	if rj.ID == 0 {
		return nil
	}
	logs, err := githubapi.GetJobLogs(ctx, token, gitInfo.Owner, gitInfo.Repo, rj.ID)
	if err != nil {
		log.Warn().Err(err).Int64("job_id", rj.ID).Msg("fetch successful test job log for coverage failed")
		return nil
	}
	return ParseCoverage(logs)
}

// pipelineCoverage rolls up a pipeline's coverage from its test job(s): the
// mean of every test job that reported a value, or nil when none did.
func pipelineCoverage(jobs []domain.TaskPipelineJob) *float64 {
	var sum float64
	var count int
	for _, j := range jobs {
		if j.CoveragePct == nil {
			continue
		}
		sum += *j.CoveragePct
		count++
	}
	if count == 0 {
		return nil
	}
	mean := sum / float64(count)
	return &mean
}

func (p *PipelineRunner) persistJob(ctx context.Context, pj domain.TaskPipelineJob) domain.TaskPipelineJob {
	created, err := p.store.CreateJob(ctx, pj)
	if err != nil {
		log.Warn().Err(err).Str("pipeline_id", pj.PipelineID.String()).Str("job", pj.Name).Msg("persist pipeline job failed")
		return pj
	}
	return created
}

func isDeployTrigger(t domain.PipelineTrigger) bool {
	return t == domain.PipelineTriggerStageDeploy ||
		t == domain.PipelineTriggerPreProdDeploy ||
		t == domain.PipelineTriggerProdDeploy
}

// hasRealSuccessJob reports whether at least one job actually ran and
// succeeded — a pipeline whose only job is a "no workflow configured" skip
// proves nothing about the deploy.
func hasRealSuccessJob(jobs []domain.TaskPipelineJob) bool {
	for _, j := range jobs {
		if j.Status == domain.PipelineJobStatusSuccess {
			return true
		}
	}
	return false
}

// finalize persists the terminal pipeline state and fires side effects unless a
// newer pipeline superseded this one.
func (p *PipelineRunner) finalize(ctx context.Context, job pipelineJob, pipeline domain.TaskPipeline, status domain.PipelineStatus, jobs []domain.TaskPipelineJob) error {
	// context.WithoutCancel: this has to outlive a cancelled run context (a
	// pipeline that settles during shutdown still owes its row a terminal
	// status) while keeping the run context's values.
	finCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()

	finishedAt := time.Now()
	pipeline.Status = status
	pipeline.FinishedAt = &finishedAt
	pipeline.Jobs = jobs
	pipeline.CoveragePct = pipelineCoverage(jobs)
	// ClaimTerminal, not Update: this replica and any other may both have
	// decided the pipeline is finished — the in-process poll on one, the gate
	// sweeper on another, whose inflight map cannot see this one's work. The
	// guarded transition means exactly one of them writes the row and therefore
	// exactly one of them runs everything below: the QA dispatch, the move to
	// released, the need_revision bounce and its comment.
	claimed, won, err := p.store.ClaimTerminal(finCtx, pipeline)
	if err == nil && !won {
		log.Info().Str("pipeline_id", pipeline.ID.String()).Str("status", string(status)).
			Msg("pipeline already finalized elsewhere in the fleet, skipping side effects")
		return nil
	}
	if err == nil {
		claimed.Jobs = jobs
		pipeline = claimed
	}
	if err != nil {
		log.Warn().Err(err).Str("pipeline_id", pipeline.ID.String()).Msg("persist pipeline result failed")
		return err
	}
	pipeline.Jobs = jobs

	isDeploy := isDeployTrigger(pipeline.Trigger)

	// Supersede-while-running: results are persisted, but side effects belong
	// to the newest pipeline of the SAME class. Deploy pipelines coexist with
	// the QA gate (per_step creates both in one request), so a deploy must
	// never mute the QA gate's hand-off, nor the other way around.
	if all, listErr := p.store.ListByTask(finCtx, job.Task.ID); listErr != nil {
		log.Warn().Err(listErr).Str("task_id", job.Task.ID.String()).Msg("list pipelines for supersede check failed")
	} else {
		for _, other := range all {
			if other.ID == pipeline.ID || isDeployTrigger(other.Trigger) != isDeploy {
				continue
			}
			if other.CreatedAt.After(pipeline.CreatedAt) {
				log.Info().Str("pipeline_id", pipeline.ID.String()).Msg("pipeline superseded while running; skipping side effects")
				return nil
			}
		}
	}

	switch {
	case domain.PipelineStatusOpensGate(pipeline.Status):
		switch {
		case pipeline.Trigger == domain.PipelineTriggerStageDeploy:
			// Stage ran this task's code (and its migration) for real, which is
			// exactly what the release gate asks for. A "no workflow
			// configured" skip is NOT that: nothing ran anywhere, so the
			// migration gate must stay closed.
			if p.stage != nil && hasRealSuccessJob(jobs) {
				if err := p.stage.MarkTaskStageVerified(finCtx, job.Task.ID); err != nil {
					log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("stage verification stamp failed")
				}
			}
		case pipeline.Trigger == domain.PipelineTriggerProdDeploy:
			// For a store target the prod workflow IS the submit for review,
			// so hand the row to the release monitor before releasing.
			p.markStoreSubmitted(finCtx, job.RepositoryID, jobs)
			if hasRealSuccessJob(jobs) {
				// A successful prod deploy releases the task.
				p.moveTask(finCtx, job, domain.TaskColumnReleased, "Prod deploy succeeded — task released.")
			} else {
				// Skipped: no prod_deploy workflow is mapped, so nothing
				// actually built or shipped this task. The gate still opens
				// (an unconfigured repo cannot be held hostage), but the card
				// must say so instead of reading like a real deploy.
				p.reportDeploySkip(finCtx, job, jobs)
				p.moveTask(finCtx, job, domain.TaskColumnReleased, "No prod deploy workflow configured — task released without a verified deploy.")
			}
			// …and the change is live, which is when the post-deploy steps
			// actually apply. Posted after the move so the two comments read in
			// the order the operator does them.
			p.postDeployNotes(finCtx, job)
		case pipeline.Trigger == domain.PipelineTriggerPreProdDeploy:
			// Preprod is clean → promote to prod. If prod isn't configured,
			// preprod is the highest enabled env, so it releases the task.
			if p.deployMapped(finCtx, job.RepositoryID, domain.PipelineCategoryProdDeploy) {
				if _, derr := p.TriggerDeploy(finCtx, job.RepositoryID, job.Task, domain.PipelineTriggerProdDeploy); derr != nil {
					log.Warn().Err(derr).Str("pipeline_id", pipeline.ID.String()).Msg("prod deploy dispatch after preprod failed")
				}
			} else if hasRealSuccessJob(jobs) {
				p.moveTask(finCtx, job, domain.TaskColumnReleased, "Preprod deploy succeeded (prod not configured) — task released.")
			} else {
				p.reportDeploySkip(finCtx, job, jobs)
				p.moveTask(finCtx, job, domain.TaskColumnReleased, "No preprod deploy workflow configured — task released without a verified deploy.")
			}
		case !isDeploy && p.qa != nil:
			// The QA gate dispatches the reviewer. pipeline.GateReason is ""
			// for a pipeline that really reported, and a gate-open code for one
			// the sweeper gave up on — the dispatch is the same, what the board
			// records about it is not.
			if derr := p.qa.DispatchQA(finCtx, job.RepositoryID, job.Task, pipeline.ID, pipeline.GateReason); derr != nil {
				log.Warn().Err(derr).Str("pipeline_id", pipeline.ID.String()).Msg("QA dispatch failed")
			}
		}
	case pipeline.Status == domain.PipelineStatusFailed:
		if isDeploy {
			p.reportDeployIncident(finCtx, job, pipeline)
			p.reportPipelineFailure(finCtx, job, pipeline)
			return nil
		}
		// The QA gate only. A red build on a commit the board has ALREADY sent
		// back once carries nothing new, and reporting it again is what turned
		// a billing-blocked CI into eleven identical review cycles. The guard
		// takes over completely when it holds: it comments once and parks the
		// card, so there is no failure report and no move from here.
		if p.bounces.Hold(finCtx, job.RepositoryID, job.Task, pipeline) {
			return nil
		}
		p.reportPipelineFailure(finCtx, job, pipeline)
	}
	return nil
}

// markStoreSubmitted flips the repository's mobile store row into
// waiting_for_review after a prod deploy that really ran a store submit. This
// is the ONLY writer of an open review_state, and storeops.Monitor polls no
// row without one — so without this call the review tracking, the rejection
// incident and last_released_version are all unreachable in production.
//
// A non-store prod target is a no-op, and so is a "no workflow configured"
// skip: hasRealSuccessJob is the same guard the stage-verification stamp
// uses, because a pipeline that ran nothing submitted nothing, and claiming
// otherwise would have the monitor poll a review that does not exist and
// re-report the previous submission's verdict.
//
// The marketing version is not passed: the workflow reads it out of the
// project (xcodebuild -showBuildSettings, pubspec.yaml) after dispatch, so
// the control plane genuinely does not know it here. The store's own reported
// version is what lands in last_released_version on approval.
func (p *PipelineRunner) markStoreSubmitted(ctx context.Context, repositoryID uuid.UUID, jobs []domain.TaskPipelineJob) {
	if p.stores == nil || p.deployTargets == nil || !hasRealSuccessJob(jobs) {
		return
	}
	target, err := p.deployTargets.Get(ctx, repositoryID, "", domain.DeployEnvProd)
	if err != nil {
		if !errors.Is(err, port.ErrNotFound) {
			log.Warn().Err(err).Str("repository_id", repositoryID.String()).Msg("resolve prod deploy target for store submit failed")
		}
		return
	}
	if !domain.IsStoreProvider(target.Provider) {
		return
	}
	if err := p.stores.MarkSubmitted(ctx, repositoryID, domain.StoreProviderPlatform(target.Provider), ""); err != nil {
		log.Warn().Err(err).Str("repository_id", repositoryID.String()).Msg("record store submit failed")
	}
}

// postDeployNotes puts the task's after_deploy steps on the task the moment
// production reports success — cache warms, feature-flag flips, the smoke check
// somebody has to run by hand. They used to live in whatever comment the
// releasing agent thought to write, which meant a pipeline-driven release
// (nobody watching) produced none at all.
//
// The task is re-read rather than taken from the queued job: a deploy can sit
// in flight for half an hour, and the steps posted must be the current ones.
// A read failure falls back to the snapshot — stale steps beat no steps.
func (p *PipelineRunner) postDeployNotes(ctx context.Context, job pipelineJob) {
	if p.tasks == nil {
		return
	}
	task := job.Task
	if p.taskReader != nil {
		if fresh, err := p.taskReader.GetTask(ctx, job.RepositoryID, job.Task.ID); err == nil {
			task = fresh
		} else {
			log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("re-read task for post-deploy notes failed")
		}
	}
	if task.AfterDeploy == nil {
		return
	}
	after := strings.TrimSpace(*task.AfterDeploy)
	if after == "" {
		return
	}
	if _, err := p.tasks.AddComment(ctx, job.RepositoryID, job.Task.ID, domain.CreateTaskCommentRequest{
		AuthorType: "system",
		Content:    "Deploy sonrası yapılacaklar:\n" + after,
	}); err != nil {
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("post-deploy notes comment failed")
	}
}

// reportDeployIncident opens a production incident for a failed deploy so the
// remedy engine can propose the rollback instead of leaving the environment in
// an unknown state with only a red pipeline to show for it.
func (p *PipelineRunner) reportDeployIncident(ctx context.Context, job pipelineJob, pipeline domain.TaskPipeline) {
	if p.incidents == nil {
		return
	}
	env := domain.DeployEnvStage
	switch pipeline.Trigger {
	case domain.PipelineTriggerPreProdDeploy:
		env = domain.DeployEnvPreProd
	case domain.PipelineTriggerProdDeploy:
		env = domain.DeployEnvProd
	}
	detail := pipelineFailureReport(pipeline)
	if len(detail) > 4000 {
		detail = truncateTail(detail, 4000)
	}
	if _, err := p.incidents.IngestDeployFailure(ctx, job.RepositoryID, env, job.Task.Key, detail); err != nil {
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("deploy failure incident ingest failed")
	}
}

// deployMapped reports whether the repo has a non-empty workflow mapping for a
// deploy category. Used to decide, at each step of the stage→preprod→prod
// chain, whether to promote to the next env or release now.
func (p *PipelineRunner) deployMapped(ctx context.Context, repositoryID uuid.UUID, category string) bool {
	if p.jobs == nil {
		return false
	}
	mappings, err := p.jobs.ListByRepository(ctx, repositoryID)
	if err != nil {
		log.Warn().Err(err).Str("repository_id", repositoryID.String()).Msg("deployMapped mapping list failed")
		return false
	}
	for _, m := range mappings {
		if m.Category == category && m.TargetKind == domain.PipelineTargetWorkflow && strings.TrimSpace(m.TargetRef) != "" {
			return true
		}
	}
	return false
}

// moveTask moves the task to col (the prod-deploy → released transition).
//
// It used to post a system comment saying the deploy succeeded as well. It no
// longer does: the move itself is the news, the board history renders it with
// its own reason (MoveReasonDeployReleased → "Deploy succeeded — task
// released"), and a comment repeating that is one more line to scroll past on a
// card whose comment thread is meant to carry the things that went WRONG. The
// note is kept as an argument because it is what the log records; nothing else
// reads it.
func (p *PipelineRunner) moveTask(ctx context.Context, job pipelineJob, col domain.TaskColumn, note string) {
	if p.tasks == nil {
		return
	}
	log.Info().Str("task_id", job.Task.ID.String()).Str("column", string(col)).Str("note", note).
		Msg("deploy finished; moving the task")
	if _, err := p.tasks.UpdateTask(ctx, job.RepositoryID, job.Task.ID, domain.UpdateBoardTaskRequest{
		Column:       &col,
		SystemReason: domain.MoveReasonDeployReleased,
	}); err != nil {
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("release task move failed")
	}
}

// reportDeploySkip comments on the task when a deploy pipeline released it
// without ever running: the released column otherwise reads identically for
// a real prod deploy and for a repo with no workflow mapped, and only this
// comment tells the two apart.
func (p *PipelineRunner) reportDeploySkip(ctx context.Context, job pipelineJob, jobs []domain.TaskPipelineJob) {
	if p.tasks == nil {
		return
	}
	note := "no deploy workflow configured"
	if len(jobs) > 0 && strings.TrimSpace(jobs[0].Name) != "" {
		note = jobs[0].Name
	}
	if _, err := p.tasks.AddComment(ctx, job.RepositoryID, job.Task.ID, domain.CreateTaskCommentRequest{
		AuthorType: "system",
		Content:    deploySkipComment(note),
	}); err != nil {
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("deploy skip comment failed")
	}
}

func deploySkipComment(note string) string {
	return "Released without a verified deploy: " + note + ", so nothing was actually built or shipped by CI for this task. " +
		"Configure a deploy workflow mapping for this repository, or deploy and verify manually."
}

// finishNoChecks records a single skipped job and finalizes as SKIPPED so a
// repo without a configured mapping still flows through the gate (see
// PipelineStatusOpensGate) without claiming a green build it never ran. It
// used to finalize as success, which showed a "Success" badge next to a
// provider of "Did not run" — a contradiction that made the QA gate look
// satisfied when nothing had executed.
func (p *PipelineRunner) finishNoChecks(ctx context.Context, job pipelineJob, pipeline domain.TaskPipeline, note string) error {
	finCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	pipeline.Provider = domain.PipelineProviderNone
	noop := domain.TaskPipelineJob{PipelineID: pipeline.ID, Name: note, Status: domain.PipelineJobStatusSkipped, Position: 0}
	created, err := p.store.CreateJob(finCtx, noop)
	if err != nil {
		created = noop
	}
	return p.finalize(ctx, job, pipeline, domain.PipelineStatusSkipped, []domain.TaskPipelineJob{created})
}

// finishNoWorkspace fails the pipeline with a note and moves nothing: with no
// workspace or token there is nowhere safe to run, and bouncing the task would
// spawn a revision run that would hit the same wall.
//
// It does comment on the task, though. Entering code_review gates dispatch on
// the pipeline, so a silent failure here left the card sitting in code_review
// with no agent, no error and no hint — the user just saw work stop.
func (p *PipelineRunner) finishNoWorkspace(ctx context.Context, job pipelineJob, pipeline domain.TaskPipeline, note string) error {
	if p.tasks != nil {
		cmtCtx, cancelCmt := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		if _, err := p.tasks.AddComment(cmtCtx, job.RepositoryID, job.Task.ID, domain.CreateTaskCommentRequest{
			AuthorType: "system",
			Content:    "Pipeline could not start: " + note,
		}); err != nil {
			log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("pipeline failure comment failed")
		}
		cancelCmt()
	}
	return p.persistNoWorkspace(ctx, pipeline, note)
}

func (p *PipelineRunner) persistNoWorkspace(ctx context.Context, pipeline domain.TaskPipeline, note string) error {
	finCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	finishedAt := time.Now()
	pipeline.Status = domain.PipelineStatusFailed
	pipeline.Provider = domain.PipelineProviderNone
	pipeline.Note = note
	pipeline.FinishedAt = &finishedAt
	pipeline.Jobs = nil
	if _, err := p.store.Update(finCtx, pipeline); err != nil {
		return err
	}
	log.Warn().Str("pipeline_id", pipeline.ID.String()).Str("note", note).Msg("pipeline failed without side effects")
	return nil
}

// finishInterrupted persists the pipeline as interrupted with no side effects
// (a plain app shutdown must not spawn spurious revision runs).
func (p *PipelineRunner) finishInterrupted(ctx context.Context, pipeline domain.TaskPipeline, jobs []domain.TaskPipelineJob) error {
	finCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	finishedAt := time.Now()
	pipeline.Status = domain.PipelineStatusFailed
	pipeline.Note = "interrupted"
	pipeline.FinishedAt = &finishedAt
	pipeline.Jobs = jobs
	if _, err := p.store.Update(finCtx, pipeline); err != nil {
		return err
	}
	log.Info().Str("pipeline_id", pipeline.ID.String()).Msg("pipeline interrupted by shutdown; skipping side effects")
	return nil
}

// pipelineFailureReport tail-truncates each failed job's output and joins them.
func pipelineFailureReport(p domain.TaskPipeline) string {
	var parts []string
	for _, j := range p.Jobs {
		if j.Status != domain.PipelineJobStatusFailed {
			continue
		}
		out := j.Output
		if len(out) > 4000 {
			out = truncateTail(out, 4000)
		}
		parts = append(parts, fmt.Sprintf("%s:\n%s", j.Name, strings.TrimSpace(out)))
	}
	return strings.Join(parts, "\n\n")
}

// reportPipelineFailure posts a Turkish system comment with the failing job's
// tail-truncated log, then moves the task back to need_revision.
//
// One failure does NOT move the card: a DEPLOY whose workflow could never be
// dispatched because Actions is unavailable on the account (billing, spending
// limit, Actions disabled). Nothing about the change is wrong there, so
// need_revision would send a developer to fix code that is fine — the same
// eleven-cycle loop PipelineBounceGuard was written for on the build side. The
// card stays in `done`, where the QA run that merged it is the one that decides
// what happens next: run the repository's own local deploy procedure if it has
// one, and move the task to `blocked` if it has not.
func (p *PipelineRunner) reportPipelineFailure(ctx context.Context, job pipelineJob, pipeline domain.TaskPipeline) {
	if p.tasks == nil {
		return
	}
	stage := "pipeline"
	for _, j := range pipeline.Jobs {
		if j.Status == domain.PipelineJobStatusFailed {
			stage = j.Name
			break
		}
	}
	report := pipelineFailureReport(pipeline)
	blockedCI := isDeployTrigger(pipeline.Trigger) && githubapi.IsCIUnavailableText(report)
	comment := fmt.Sprintf("Pipeline failed — %s:\n\n```\n%s\n```", stage, report)
	if blockedCI {
		comment = fmt.Sprintf("Deploy could not run — %s:\n\n```\n%s\n```\n\n"+
			"GitHub Actions is unavailable for this repository (billing, spending limit or Actions disabled), so this is not a code problem and the task is NOT being sent back to need_revision. "+
			"Deploy it the way this repository documents doing it locally, or move the task to `blocked` if it has no local deploy path.", stage, report)
	}
	if len(comment) > 3000 {
		comment = truncateHead(comment, 3000) + "\n…(truncated)"
	}
	if _, err := p.tasks.AddComment(ctx, job.RepositoryID, job.Task.ID, domain.CreateTaskCommentRequest{
		AuthorType: "system",
		Content:    comment,
	}); err != nil {
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("pipeline failure comment failed")
	}
	if blockedCI {
		log.Warn().Str("task_id", job.Task.ID.String()).Str("trigger", string(pipeline.Trigger)).
			Msg("deploy dispatch refused by GitHub (CI unavailable); leaving the task in place instead of bouncing it to need_revision")
		return
	}
	col := domain.TaskColumnNeedRevision
	if _, err := p.tasks.UpdateTask(ctx, job.RepositoryID, job.Task.ID, domain.UpdateBoardTaskRequest{
		Column:       &col,
		SystemReason: domain.MoveReasonPipelineFailed,
	}); err != nil {
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("pipeline failure task move failed")
	}
}

const pipelineMaxWaitLabel = "30m"

// deployRef resolves the ref a task's production deploy is dispatched at.
//
// It used to be the repository's DEFAULT BRANCH, unconditionally, and that was
// the drift bug recorded in todo.md and named again in migration 104's comment:
// every gate above this line proves something about THIS task's commit — the
// release gate re-resolves the task branch and compares it against the commit
// signed off at done — and then the dispatch shipped a branch that carries
// everyone else's merges too. A release could therefore be gated on one commit
// and deploy another, with nothing anywhere reporting a difference.
//
// With board_tasks.merge_commit_sha recorded (migration 104), the commit is
// known exactly, so the deploy is dispatched at THAT commit. It has to be
// dispatched by NAME rather than by SHA — workflow_dispatch accepts only a
// branch or a tag, which is the same constraint deployops.Service.Rollback
// works around — so a lightweight tag is created at the merge commit and the
// tag is the ref.
//
// The tag name is derived from the commit, not from the clock: re-releasing the
// same commit reuses the same tag instead of littering the repository, and
// CreateTag's "already exists" is therefore a success, not a failure.
//
// Falling back to the default branch when any of that is unavailable is
// deliberate. A task merged before this change has no recorded commit; a
// repository whose token cannot create tags would otherwise be unable to
// release at all. The fallback is the old behaviour and it is logged as a
// warning naming the task, so the drift is visible rather than silent.
func deployRef(ctx context.Context, token string, gitInfo domain.TaskGitInfo, task domain.BoardTask) string {
	if sha := strings.TrimSpace(task.MergeCommitSHA); sha != "" {
		tag := domain.ReleaseTagForCommit(sha)
		err := createReleaseTag(ctx, token, gitInfo.Owner, gitInfo.Repo, tag, sha)
		if err == nil || githubapi.IsRefAlreadyExists(err) {
			log.Info().Str("task_id", task.ID.String()).Str("ref", tag).Str("sha", domain.ShortSHA(sha)).
				Msg("release dispatching at the task's merge commit")
			return tag
		}
		log.Warn().Err(err).Str("task_id", task.ID.String()).Str("sha", domain.ShortSHA(sha)).
			Msg("tagging the merge commit for release failed; falling back to the default branch, which may carry other merges")
	}
	if branch := repoDefaultBranch(ctx, token, gitInfo.Owner, gitInfo.Repo); branch != "" {
		return branch
	}
	return "main"
}

// The two GitHub calls deployRef makes, behind package vars.
//
// They are indirected for exactly one reason: the decision deployRef makes —
// dispatch the task's own merge commit, or fall back to a branch that carries
// everyone else's — is the bug this function was rewritten to fix, and a
// decision that important has to be assertable without an HTTP round-trip. The
// production values are the free functions themselves; only the internal test
// replaces them.
var (
	createReleaseTag = githubapi.CreateTag

	repoDefaultBranch = func(ctx context.Context, token, owner, repo string) string {
		if r, err := githubapi.GetRepo(ctx, token, owner, repo); err == nil {
			return r.DefaultBranch
		}
		return ""
	}
)

func mergeJob(byName map[string]githubapi.RunJob, rj githubapi.RunJob) {
	prev, ok := byName[rj.Name]
	if !ok || (prev.Status != "completed" && rj.Status == "completed") {
		byName[rj.Name] = rj
	}
}

// foldRunJobs collapses a deploy run's jobs into one result keyed by the
// workflow file: in-progress while any job runs, failed if any failed, success
// only when all succeed.
func foldRunJobs(label string, run githubapi.WorkflowRun, jobs []githubapi.RunJob) githubapi.RunJob {
	// The fold keeps the run page as its link so the UI can open the deploy
	// even when the interesting detail is spread over several jobs.
	out := githubapi.RunJob{Name: label, Status: "completed", Conclusion: "success", HTMLURL: run.HTMLURL}
	for _, j := range jobs {
		if j.Status != "completed" {
			out.Status = "in_progress"
			out.Conclusion = ""
			return out
		}
		if j.Conclusion != "success" {
			// Surface the failing job (its id/log/link) as the fold result.
			url := j.HTMLURL
			if url == "" {
				url = run.HTMLURL
			}
			return githubapi.RunJob{ID: j.ID, Name: label, Status: "completed", Conclusion: j.Conclusion, HTMLURL: url, StartedAt: j.StartedAt, CompletedAt: j.CompletedAt}
		}
	}
	return out
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}
