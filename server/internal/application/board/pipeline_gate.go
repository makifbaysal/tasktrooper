package board

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	githubapi "github.com/makifbaysal/tasktrooper/server/internal/adapter/vcs/github"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// 45 = 30min poll budget + 15min slack for pod replacement and GitHub queue; the window must strictly outlive the poller.
const PipelineGateWindow = 45 * time.Minute

const PipelineGateSweeperInterval = 2 * time.Minute

const pipelineNoRunGrace = 5 * time.Minute

const pipelineGateSweepBatch = 100

type UnfinishedPipelineLister interface {
	ListUnfinished(ctx context.Context, limit int) ([]domain.TaskPipeline, error)
}

// Both GitHub reads sit behind package vars: this decision is the bug the file was written to fix.
var (
	gateListRunsByHeadSHA = githubapi.ListRunsByHeadSHA
	gateListRunJobs       = githubapi.ListRunJobs
)

type PipelineGateSweeper struct {
	store    UnfinishedPipelineLister
	resolver *PipelineRunner
	window   time.Duration
}

func NewPipelineGateSweeper(store UnfinishedPipelineLister, resolver *PipelineRunner, window time.Duration) *PipelineGateSweeper {
	if window <= 0 {
		window = PipelineGateWindow
	}
	return &PipelineGateSweeper{store: store, resolver: resolver, window: window}
}

func (s *PipelineGateSweeper) Start(ctx context.Context, interval time.Duration) {
	if s == nil || s.store == nil || s.resolver == nil {
		return
	}
	if interval <= 0 {
		interval = PipelineGateSweeperInterval
	}
	go func() {
		s.sweep(ctx)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.sweep(ctx)
			}
		}
	}()
	log.Info().Dur("interval", interval).Dur("gate_window", s.window).Msg("pipeline gate sweeper started")
}

func (s *PipelineGateSweeper) sweep(ctx context.Context) {
	unfinished, err := s.store.ListUnfinished(ctx, pipelineGateSweepBatch)
	if err != nil {
		log.Warn().Err(err).Msg("pipeline gate sweeper: listing unfinished pipelines failed")
		return
	}
	for _, p := range unfinished {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if err := s.resolver.ResolveUnfinished(ctx, p, s.window); err != nil {
			log.Warn().Err(err).Str("pipeline_id", p.ID.String()).
				Msg("pipeline gate sweeper: resolving a pipeline failed")
		}
	}
}

func (p *PipelineRunner) ResolveByHeadSHA(ctx context.Context, repositoryID uuid.UUID, headSHA string) (int, error) {
	headSHA = strings.TrimSpace(headSHA)
	if p == nil || p.store == nil || headSHA == "" {
		return 0, nil
	}
	pipelines, err := p.store.ListUnfinishedByHeadSHA(ctx, repositoryID, headSHA)
	if err != nil {
		return 0, err
	}
	for _, pipeline := range pipelines {
		if rerr := p.ResolveUnfinished(ctx, pipeline, PipelineGateWindow); rerr != nil {
			log.Warn().Err(rerr).Str("pipeline_id", pipeline.ID.String()).
				Msg("webhook: resolving a pipeline from a workflow event failed")
		}
	}
	return len(pipelines), nil
}

func (p *PipelineRunner) ResolveUnfinished(ctx context.Context, pipeline domain.TaskPipeline, window time.Duration) error {
	if p == nil || p.store == nil {
		return nil
	}
	if window <= 0 {
		window = PipelineGateWindow
	}
	if _, busy := p.inflight.Load(pipeline.ID); busy {
		return nil
	}
	if isDeployTrigger(pipeline.Trigger) {
		return nil
	}

	fresh, err := p.store.Get(ctx, pipeline.ID)
	if err != nil {
		if errors.Is(err, domain.ErrPipelineNotFound) {
			return nil
		}
		return err
	}
	if fresh.Status != domain.PipelineStatusPending && fresh.Status != domain.PipelineStatusRunning {
		return nil
	}

	if p.taskReader == nil {
		return fmt.Errorf("pipeline gate: no task reader wired, cannot resolve pipeline %s", fresh.ID)
	}
	task, err := p.taskReader.GetTask(ctx, fresh.RepositoryID, fresh.TaskID)
	if err != nil {
		return fmt.Errorf("pipeline gate: read task: %w", err)
	}
	job := pipelineJob{Pipeline: fresh, RepositoryID: fresh.RepositoryID, Task: task}

	if wf, ok := p.workflowFor(ctx, task.TaskType); ok && !wf.Has(task.Column, domain.BehaviourWaitForCI) {
		return p.settleQuietly(ctx, fresh, "the task left the CI-gated column before this pipeline reported")
	}

	expired := time.Since(fresh.CreatedAt) >= window

	var mappings []domain.RepositoryPipelineJob
	if p.jobs != nil {
		if m, jerr := p.jobs.ListByRepository(ctx, fresh.RepositoryID); jerr != nil {
			log.Warn().Err(jerr).Str("repository_id", fresh.RepositoryID.String()).
				Msg("pipeline gate: listing job mappings failed")
		} else {
			mappings = m
		}
	}
	targets := filterMappings(mappings, domain.PipelineTargetJob,
		domain.PipelineCategoryValidate, domain.PipelineCategoryBuild,
		domain.PipelineCategoryTest, domain.PipelineCategoryMutationTest)
	if len(targets) == 0 {
		return p.openGate(ctx, job, fresh, domain.PipelineGateReasonNoCI,
			"no validate/build/test job is mapped for this repository, so there was never a check to wait for")
	}

	token := ""
	if p.tokens != nil {
		if t, terr := p.tokens(ctx); terr == nil {
			token = strings.TrimSpace(t)
		}
	}
	if token == "" {
		if expired {
			return p.openGate(ctx, job, fresh, domain.PipelineGateReasonCIUnavailable,
				"GitHub is not connected, so no build result can reach the board")
		}
		return nil
	}
	owner, repoName, ok := "", "", false
	if repo, rerr := p.repos.ResolveRepository(ctx, fresh.RepositoryID); rerr == nil {
		owner, repoName, ok = githubapi.ParseOwnerRepo(repo.RemoteURL)
	}
	if !ok {
		if expired {
			return p.openGate(ctx, job, fresh, domain.PipelineGateReasonCIUnavailable,
				"this repository has no GitHub remote, so its Actions runs cannot be read")
		}
		return nil
	}
	if strings.TrimSpace(fresh.HeadSHA) == "" {
		if expired {
			return p.openGate(ctx, job, fresh, domain.PipelineGateReasonTimeout,
				"this pipeline never recorded the commit it was about, so its result cannot be looked up")
		}
		return nil
	}
	gitInfo := domain.TaskGitInfo{Owner: owner, Repo: repoName, HeadSHA: fresh.HeadSHA}

	runs, err := gateListRunsByHeadSHA(ctx, token, owner, repoName, fresh.HeadSHA)
	if err != nil {
		if githubapi.IsCIUnavailable(err) {
			return p.openGate(ctx, job, fresh, domain.PipelineGateReasonCIUnavailable,
				"GitHub refused to report Actions runs for this repository ("+err.Error()+")")
		}
		log.Warn().Err(err).Str("sha", fresh.HeadSHA).Msg("pipeline gate: listing runs by head sha failed")
		if expired {
			return p.openGate(ctx, job, fresh, domain.PipelineGateReasonTimeout,
				"no build result arrived within "+window.String()+" and GitHub could not be reached to ask why")
		}
		return nil
	}
	if len(runs) == 0 {
		if time.Since(fresh.CreatedAt) >= pipelineNoRunGrace {
			return p.openGate(ctx, job, fresh, domain.PipelineGateReasonCIUnavailable,
				"no GitHub Actions run exists for "+domain.ShortSHA(fresh.HeadSHA)+
					" — CI is disabled, out of quota, or no workflow triggers on this branch")
		}
		return nil
	}

	byName := map[string]githubapi.RunJob{}
	for _, run := range runs {
		runJobs, jerr := gateListRunJobs(ctx, token, owner, repoName, run.ID)
		if jerr != nil {
			continue
		}
		for _, rj := range runJobs {
			mergeJob(byName, rj)
		}
	}

	if done, jobs, status := p.evaluate(ctx, fresh.ID, gitInfo, token, targets, byName, false); done {
		fresh = p.markProvider(ctx, fresh, domain.PipelineProviderGitHubActions)
		return p.finalize(ctx, job, fresh, status, jobs)
	}
	if !expired {
		return nil
	}
	if anyReportedFailure(targets, byName) {
		done, jobs, status := p.evaluate(ctx, fresh.ID, gitInfo, token, targets, byName, true)
		if done {
			fresh = p.markProvider(ctx, fresh, domain.PipelineProviderGitHubActions)
			return p.finalize(ctx, job, fresh, status, jobs)
		}
	}
	return p.openGate(ctx, job, fresh, domain.PipelineGateReasonTimeout,
		"no build result arrived within "+window.String())
}

func (p *PipelineRunner) workflowFor(ctx context.Context, taskType domain.TaskType) (domain.Workflow, bool) {
	if p.workflows == nil {
		return domain.Workflow{}, false
	}
	wf, err := p.workflows.Workflow(ctx, taskType)
	if err != nil {
		log.Warn().Err(err).Str("task_type", string(taskType)).
			Msg("pipeline gate: workflow lookup failed, failing closed")
		return domain.Workflow{}, false
	}
	return wf, true
}

func anyReportedFailure(targets []mappingTarget, byName map[string]githubapi.RunJob) bool {
	for _, t := range targets {
		rj, ok := byName[t.ref]
		if ok && rj.Status == "completed" && rj.Conclusion != "success" {
			return true
		}
	}
	return false
}

func (p *PipelineRunner) openGate(ctx context.Context, job pipelineJob, pipeline domain.TaskPipeline, reason, note string) error {
	finCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()

	pipeline.Provider = domain.PipelineProviderNone
	pipeline.GateReason = reason
	pipeline.Note = note

	marker := domain.TaskPipelineJob{
		PipelineID: pipeline.ID,
		Name:       gateReasonLabel(reason),
		Command:    reason,
		Status:     domain.PipelineJobStatusSkipped,
		Output:     note,
		Position:   0,
	}
	created, err := p.store.CreateJob(finCtx, marker)
	if err != nil {
		created = marker
	}

	if p.tasks != nil {
		if _, cerr := p.tasks.AddComment(finCtx, job.RepositoryID, job.Task.ID, domain.CreateTaskCommentRequest{
			AuthorType: "system",
			Content: "Code review başlatıldı ama arkasında yeşil bir pipeline YOK — " +
				gateReasonLabel(reason) + ": " + note + ".",
		}); cerr != nil {
			log.Warn().Err(cerr).Str("task_id", job.Task.ID.String()).Msg("pipeline gate: comment failed")
		}
	}

	log.Warn().
		Str("task_id", job.Task.ID.String()).
		Str("pipeline_id", pipeline.ID.String()).
		Str("gate_reason", reason).
		Str("note", note).
		Msg("code review gate opened without a build result")

	return p.finalize(ctx, job, pipeline, domain.PipelineStatusSkipped, []domain.TaskPipelineJob{created})
}

func (p *PipelineRunner) settleQuietly(ctx context.Context, pipeline domain.TaskPipeline, note string) error {
	finCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	finishedAt := time.Now()
	pipeline.Status = domain.PipelineStatusSkipped
	pipeline.Note = note
	pipeline.FinishedAt = &finishedAt
	pipeline.Jobs = nil
	if _, err := p.store.Update(finCtx, pipeline); err != nil {
		return err
	}
	log.Info().Str("pipeline_id", pipeline.ID.String()).Str("note", note).
		Msg("pipeline settled without side effects")
	return nil
}

func gateReasonLabel(reason string) string {
	switch reason {
	case domain.PipelineGateReasonNoCI:
		return "no CI configured"
	case domain.PipelineGateReasonCIUnavailable:
		return "CI unavailable"
	case domain.PipelineGateReasonTimeout:
		return "CI did not report in time"
	default:
		return "gate opened"
	}
}

var _ UnfinishedPipelineLister = (port.TaskPipelineStore)(nil)
