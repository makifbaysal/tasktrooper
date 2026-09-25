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
	githubapi "github.com/makifbaysal/tasktrooper/server/internal/adapter/vcs/github"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/rs/zerolog/log"
)

type QADispatcher interface {
	DispatchQA(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask, pipelineID uuid.UUID, gateReason string) error
}

type TokenSource func(ctx context.Context) (string, error)

type StageVerifier interface {
	MarkTaskStageVerified(ctx context.Context, taskID uuid.UUID) error
}

// The store-shipping prod workflows ARE the submit; without this call the release monitor's review-polling never runs.
type StoreSubmitter interface {
	MarkSubmitted(ctx context.Context, repositoryID uuid.UUID, platform, version string) error
}

// The deploy-job snapshot can be half an hour old - never post runbook steps from it.
type TaskRunbookReader interface {
	GetTask(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.BoardTask, error)
}

type IncidentReporter interface {
	IngestDeployFailure(ctx context.Context, repositoryID uuid.UUID, env, taskKey, detail string) (domain.Incident, error)
}

type PipelineGit interface {
	TaskGitInfo(ctx context.Context, workspacePath string) (domain.TaskGitInfo, error)
	EnsurePullRequest(ctx context.Context, workspacePath string) (string, error)
	PushBranch(ctx context.Context, workspacePath string) error
}

const (
	pipelinePollInterval = 20 * time.Second
	pipelineMaxWait      = 30 * time.Minute
	jobLogTailLimit      = 10000
)

type PipelineRunnerDeps struct {
	Store         port.TaskPipelineStore
	Jobs          port.RepositoryPipelineJobStore
	DeployTargets port.DeployTargetStore
	Repos         RepositoryResolver
	Tasks         TaskUpdater
	QA            QADispatcher
	Git           PipelineGit
	Tokens        TokenSource
	WorkspaceRoot string
	TaskPRs       TaskPRRecorder
}

type pipelineJob struct {
	Pipeline     domain.TaskPipeline
	RepositoryID uuid.UUID
	Task         domain.BoardTask
}

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

	// inflight ids are skipped by the sweeper: two resolvers would write two job-row sets for one run.
	inflight sync.Map

	incidents  IncidentReporter
	stage      StageVerifier
	stores     StoreSubmitter
	prRecorder TaskPRRecorder
	taskReader TaskRunbookReader
	workflows  port.WorkflowReader
	components ComponentPaths
	// Nil bounce guard keeps old behaviour: every failed pipeline sends the task back.
	bounces *PipelineBounceGuard
}

func (p *PipelineRunner) SetBounceGuard(g *PipelineBounceGuard) { p.bounces = g }

func (p *PipelineRunner) SetStageVerifier(v StageVerifier) { p.stage = v }

func (p *PipelineRunner) SetTaskReader(r TaskRunbookReader) { p.taskReader = r }

func (p *PipelineRunner) SetIncidentReporter(r IncidentReporter) { p.incidents = r }

func (p *PipelineRunner) SetStoreSubmitter(s StoreSubmitter) { p.stores = s }

func (p *PipelineRunner) SetWorkflows(w port.WorkflowReader) { p.workflows = w }

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

func (p *PipelineRunner) Trigger(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask, trigger domain.PipelineTrigger) (domain.TaskPipeline, error) {
	return p.trigger(ctx, repositoryID, task, trigger, true)
}

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

func (p *PipelineRunner) Start(ctx context.Context) {
	ctx, p.cancel = context.WithCancel(ctx)

	// No boot-time FailStaleRunning any more, and its removal is the point.
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

func (p *PipelineRunner) execute(ctx context.Context, job pipelineJob) error {
	pipeline := job.Pipeline

	p.inflight.Store(pipeline.ID, struct{}{})
	defer p.inflight.Delete(pipeline.ID)

	if current, getErr := p.store.Get(ctx, pipeline.ID); getErr != nil {
		log.Warn().Err(getErr).Str("pipeline_id", pipeline.ID.String()).Msg("pipeline freshness check failed; proceeding")
	} else if current.Status != domain.PipelineStatusPending {
		log.Info().Str("pipeline_id", pipeline.ID.String()).Str("status", string(current.Status)).Msg("pipeline no longer pending; skipping execution")
		return nil
	}

	startedAt := time.Now()
	pipeline.Status = domain.PipelineStatusRunning
	pipeline.StartedAt = &startedAt
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
	// A prod/preprod deploy runs after the merge, whose branch delete already
	// happened; ensuring/pushing a PR here recreated the deleted task branch
	// and opened a fresh PR against a merge that was already done.
	if !isProdOrPreProdDeployTrigger(job.Pipeline.Trigger) {
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
	}
	return p.git.TaskGitInfo(ctx, dir)
}

func isProdOrPreProdDeployTrigger(t domain.PipelineTrigger) bool {
	return t == domain.PipelineTriggerPreProdDeploy || t == domain.PipelineTriggerProdDeploy
}

func (p *PipelineRunner) runQAGate(ctx context.Context, job pipelineJob, pipeline domain.TaskPipeline, gitInfo domain.TaskGitInfo, token string, mappings []domain.RepositoryPipelineJob) error {
	componentPath := p.resolveComponentPath(ctx, job.RepositoryID, job.Task.ComponentID)
	targets := filterMappings(scopeMappingsToComponent(mappings, componentPath), domain.PipelineTargetJob,
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

func (p *PipelineRunner) runDeploy(ctx context.Context, job pipelineJob, pipeline domain.TaskPipeline, repo domain.Repository, gitInfo domain.TaskGitInfo, token string, mappings []domain.RepositoryPipelineJob) error {
	category := domain.PipelineCategoryStageDeploy
	ref := gitInfo.Branch
	if isProdOrPreProdDeployTrigger(pipeline.Trigger) {
		if pipeline.Trigger == domain.PipelineTriggerPreProdDeploy {
			category = domain.PipelineCategoryPreProdDeploy
		} else {
			category = domain.PipelineCategoryProdDeploy
		}
		resolved, err := deployRef(ctx, token, gitInfo, job.Task)
		if err != nil {
			return p.finishNoWorkspace(ctx, job, pipeline, err.Error())
		}
		ref = resolved
	}
	componentPath := p.resolveComponentPath(ctx, job.RepositoryID, job.Task.ComponentID)
	targets := filterMappings(scopeMappingsToComponent(mappings, componentPath), domain.PipelineTargetWorkflow, category)
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

// ComponentPaths resolves a task's component to the path pipeline mappings
// scope on ("" for the repository itself). Optional and nil-safe: without it
// wired, mappings run unfiltered by component, same as before this existed.
type ComponentPaths interface {
	ComponentPath(ctx context.Context, repositoryID, componentID uuid.UUID) (string, error)
}

func (p *PipelineRunner) SetComponentPaths(c ComponentPaths) { p.components = c }

// resolveComponentPath answers nil when the task is not scoped to one
// component (the whole repository, every mapping applies) or the resolver
// is not wired/fails — a monorepo that filtered out every mapping because a
// path could not be read would silently stop deploying and testing.
func (p *PipelineRunner) resolveComponentPath(ctx context.Context, repositoryID uuid.UUID, componentID *uuid.UUID) *string {
	if componentID == nil || p.components == nil {
		return nil
	}
	path, err := p.components.ComponentPath(ctx, repositoryID, *componentID)
	if err != nil {
		log.Warn().Err(err).Str("component_id", componentID.String()).Msg("pipeline: resolving the task's component path failed; running every mapped check")
		return nil
	}
	return &path
}

// normalizeComponentPath treats "." (Component.Path's root marker) and ""
// (RepositoryPipelineJob.SubProjectPath's) as the same thing: the repository
// itself.
func normalizeComponentPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "." {
		return ""
	}
	return p
}

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

type mappingTarget struct {
	label    string
	ref      string
	category string
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

// scopeMappingsToComponent narrows mappings to one monorepo sub-project's
// before filterMappings runs — componentPath nil (a root task, or a
// component whose path could not be resolved) runs every mapping unchanged.
func scopeMappingsToComponent(mappings []domain.RepositoryPipelineJob, componentPath *string) []domain.RepositoryPipelineJob {
	if componentPath == nil {
		return mappings
	}
	want := normalizeComponentPath(*componentPath)
	out := make([]domain.RepositoryPipelineJob, 0, len(mappings))
	for _, m := range mappings {
		if normalizeComponentPath(m.SubProjectPath) == want {
			out = append(out, m)
		}
	}
	return out
}

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

func (p *PipelineRunner) collectDeployJobs(ctx context.Context, gitInfo domain.TaskGitInfo, token, ref string, targets []mappingTarget, after time.Time) map[string]githubapi.RunJob {
	byName := map[string]githubapi.RunJob{}
	for _, t := range targets {
		runs, err := githubapi.ListWorkflowRunsByFileBranch(ctx, token, gitInfo.Owner, gitInfo.Repo, t.ref, ref)
		if err != nil || len(runs) == 0 {
			continue
		}
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
			byName[t.ref] = githubapi.RunJob{Name: t.ref, Status: run.Status, Conclusion: run.Conclusion, HTMLURL: run.HTMLURL}
			continue
		}
		byName[t.ref] = foldRunJobs(t.ref, run, runJobs)
	}
	return byName
}

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

func hasRealSuccessJob(jobs []domain.TaskPipelineJob) bool {
	for _, j := range jobs {
		if j.Status == domain.PipelineJobStatusSuccess {
			return true
		}
	}
	return false
}

func (p *PipelineRunner) finalize(ctx context.Context, job pipelineJob, pipeline domain.TaskPipeline, status domain.PipelineStatus, jobs []domain.TaskPipelineJob) error {
	finCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()

	finishedAt := time.Now()
	pipeline.Status = status
	pipeline.FinishedAt = &finishedAt
	pipeline.Jobs = jobs
	pipeline.CoveragePct = pipelineCoverage(jobs)
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
			if p.stage != nil && hasRealSuccessJob(jobs) {
				if err := p.stage.MarkTaskStageVerified(finCtx, job.Task.ID); err != nil {
					log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("stage verification stamp failed")
				}
			}
		case pipeline.Trigger == domain.PipelineTriggerProdDeploy:
			p.markStoreSubmitted(finCtx, job.RepositoryID, jobs)
			if hasRealSuccessJob(jobs) {
				p.moveTask(finCtx, job, domain.TaskColumnReleased, "Prod deploy succeeded — task released.")
			} else {
				p.reportDeploySkip(finCtx, job, jobs)
				p.moveTask(finCtx, job, domain.TaskColumnReleased, "No prod deploy workflow configured — task released without a verified deploy.")
			}
			p.postDeployNotes(finCtx, job)
		case pipeline.Trigger == domain.PipelineTriggerPreProdDeploy:
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
		if p.bounces.Hold(finCtx, job.RepositoryID, job.Task, pipeline) {
			return nil
		}
		p.reportPipelineFailure(finCtx, job, pipeline)
	}
	return nil
}

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

// deployRef refuses rather than falling back to the default branch when the
// task carries no merge commit: dispatching the default branch would ship
// whatever else has landed there since, not this task's change — the caller
// fails the pipeline with the returned reason instead of guessing a ref.
func deployRef(ctx context.Context, token string, gitInfo domain.TaskGitInfo, task domain.BoardTask) (string, error) {
	sha := strings.TrimSpace(task.MergeCommitSHA)
	if sha == "" {
		return "", fmt.Errorf("task %s has no merge commit recorded — a prod/preprod deploy ships the exact commit a task merged, not whatever the default branch currently points at", task.Key)
	}
	tag := domain.ReleaseTagForCommit(sha)
	err := createReleaseTag(ctx, token, gitInfo.Owner, gitInfo.Repo, tag, sha)
	if err == nil || githubapi.IsRefAlreadyExists(err) {
		log.Info().Str("task_id", task.ID.String()).Str("ref", tag).Str("sha", domain.ShortSHA(sha)).
			Msg("release dispatching at the task's merge commit")
		return tag, nil
	}
	log.Warn().Err(err).Str("task_id", task.ID.String()).Str("sha", domain.ShortSHA(sha)).
		Msg("tagging the merge commit for release failed; falling back to the default branch, which may carry other merges")
	if branch := repoDefaultBranch(ctx, token, gitInfo.Owner, gitInfo.Repo); branch != "" {
		return branch, nil
	}
	return "main", nil
}

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

func foldRunJobs(label string, run githubapi.WorkflowRun, jobs []githubapi.RunJob) githubapi.RunJob {
	out := githubapi.RunJob{Name: label, Status: "completed", Conclusion: "success", HTMLURL: run.HTMLURL}
	for _, j := range jobs {
		if j.Status != "completed" {
			out.Status = "in_progress"
			out.Conclusion = ""
			return out
		}
		if j.Conclusion != "success" {
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
