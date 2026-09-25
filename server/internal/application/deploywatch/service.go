package deploywatch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const DefaultHealthWindow = 15 * time.Minute

const resolveTimeout = 30 * time.Second

const logFetchTimeout = 10 * time.Second

const maxLogBytes = 1 << 20

type TaskStore interface {
	Get(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.BoardTask, error)

	FindTaskByMergeCommit(ctx context.Context, repositoryID uuid.UUID, sha string) (domain.BoardTask, error)
}

type Commenter interface {
	AddComment(ctx context.Context, repositoryID, taskID uuid.UUID, req domain.CreateTaskCommentRequest) (domain.TaskComment, error)
}

type RepositoryResolver interface {
	Get(ctx context.Context, id uuid.UUID) (domain.Repository, error)
}

type Deps struct {
	Tasks     TaskStore
	Comments  Commenter
	Targets   port.DeployTargetStore
	Repos     RepositoryResolver
	Runs      port.DeploymentRunStore
	Pipeline  port.RepositoryPipelineJobStore
	Actions   port.ActionsClient
	Rollbacks Rollbacker
	Git       GitReverter
	Incidents IncidentIngester

	RepoCoordinates func(ctx context.Context, repo domain.Repository) (owner, name string, err error)

	HealthWindow time.Duration
}

type Service struct {
	tasks     TaskStore
	comments  Commenter
	targets   port.DeployTargetStore
	repos     RepositoryResolver
	runs      port.DeploymentRunStore
	pipeline  port.RepositoryPipelineJobStore
	actions   port.ActionsClient
	rollbacks Rollbacker
	git       GitReverter
	incidents IncidentIngester
	coords    func(ctx context.Context, repo domain.Repository) (string, string, error)

	healthWindow time.Duration

	policy urlguard.Policy
	now    func() time.Time
}

func New(deps Deps) *Service {
	window := deps.HealthWindow
	if window <= 0 {
		window = DefaultHealthWindow
	}
	return &Service{
		tasks:        deps.Tasks,
		comments:     deps.Comments,
		targets:      deps.Targets,
		repos:        deps.Repos,
		runs:         deps.Runs,
		pipeline:     deps.Pipeline,
		actions:      deps.Actions,
		rollbacks:    deps.Rollbacks,
		git:          deps.Git,
		incidents:    deps.Incidents,
		coords:       deps.RepoCoordinates,
		healthWindow: window,
		policy:       urlguard.Default(),
		now:          time.Now,
	}
}

func (s *Service) SetClock(now func() time.Time) { s.now = now }

func (s *Service) SetURLPolicy(p urlguard.Policy) { s.policy = p }

func (s *Service) HealthWindow() time.Duration { return s.healthWindow }

var ErrNotConfigured = errors.New("the deploy watch is not configured on this deployment (GitHub is not connected)")

func (s *Service) Status(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.DeployWatchStatus, error) {
	if s.tasks == nil || s.actions == nil || s.coords == nil {
		return domain.DeployWatchStatus{}, ErrNotConfigured
	}
	task, err := s.tasks.Get(ctx, repositoryID, taskID)
	if err != nil {
		return domain.DeployWatchStatus{}, err
	}
	return s.statusForTask(ctx, task)
}

func (s *Service) StatusForTask(ctx context.Context, task domain.BoardTask) (domain.DeployWatchStatus, error) {
	if s.actions == nil || s.coords == nil {
		return domain.DeployWatchStatus{}, ErrNotConfigured
	}
	return s.statusForTask(ctx, task)
}

// StatusForCommit is the same resolution as StatusForTask, keyed on a commit
// instead of a task — a release has no single owning card. workflow, when
// set, narrows Actions runs to one workflow file (the release's frozen
// delivery profile) so an unrelated run for the same commit cannot stand in
// for it; "" keeps the task-watch behaviour of considering every run.
func (s *Service) StatusForCommit(ctx context.Context, repositoryID uuid.UUID, sha, workflow string) (domain.DeployWatchStatus, error) {
	if s.actions == nil || s.coords == nil {
		return domain.DeployWatchStatus{}, ErrNotConfigured
	}
	sha = strings.TrimSpace(sha)
	out := domain.DeployWatchStatus{
		RepositoryID: repositoryID,
		Env:          domain.DeployEnvProd,
		MergeSHA:     sha,
		State:        domain.DeployWatchUnknown,
		Signal:       domain.DeploySignalNone,
		CheckedAt:    s.now(),
	}
	if s.targets != nil {
		if target, terr := s.targets.Get(ctx, repositoryID, "", out.Env); terr == nil {
			out.HealthURL = target.HealthURL
			out.LogsURL = target.LogsURL
			out.AutoRollback = target.AutoRollback
		} else if !errors.Is(terr, port.ErrNotFound) {
			log.Warn().Err(terr).Str("repository_id", repositoryID.String()).Msg("deploy watch: reading deploy target failed")
		}
	}
	if sha == "" {
		out.Detail = "No commit sha was given to watch."
		return out, nil
	}

	resolved, err := s.resolveForCommit(ctx, repositoryID, sha, workflow, time.Time{})
	if err != nil {
		return out, err
	}
	return s.finish(mergeStatus(out, resolved)), nil
}

// StatusForCommitSince is StatusForCommit narrowed to Actions runs started at
// or after since. A rollback redeploys the same sha/workflow an earlier
// release already ran (dispatch mode reuses the previous release's tag, and a
// retry reuses the failed attempt's own tag), so watching by sha alone would
// read that OTHER run's outcome as if it were the rollback's; since pins the
// watch to a run that only exists because the rollback dispatched it. It does
// not fall back to the commit-status signal (StatusForCommit's plain-commit
// fallback carries no timestamp of its own, so it cannot be pinned to since
// either) — no qualifying run is reported pending, exactly like a deploy that
// has dispatched but not shown up yet.
func (s *Service) StatusForCommitSince(ctx context.Context, repositoryID uuid.UUID, sha, workflow string, since time.Time) (domain.DeployWatchStatus, error) {
	if s.actions == nil || s.coords == nil {
		return domain.DeployWatchStatus{}, ErrNotConfigured
	}
	sha = strings.TrimSpace(sha)
	out := domain.DeployWatchStatus{
		RepositoryID: repositoryID,
		Env:          domain.DeployEnvProd,
		MergeSHA:     sha,
		State:        domain.DeployWatchPending,
		Signal:       domain.DeploySignalNone,
		CheckedAt:    s.now(),
	}
	if sha == "" {
		out.State = domain.DeployWatchUnknown
		out.Detail = "No commit sha was given to watch."
		return out, nil
	}

	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return domain.DeployWatchStatus{}, fmt.Errorf("deploy watch: loading repository: %w", err)
	}
	owner, name, err := s.coords(ctx, repo)
	if err != nil {
		return domain.DeployWatchStatus{}, fmt.Errorf("deploy watch: resolving repository coordinates: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()

	resolved, ok, err := s.actionsSignal(ctx, repositoryID, owner, name, sha, workflow, since)
	if err != nil {
		return domain.DeployWatchStatus{}, err
	}
	if !ok {
		out.Detail = fmt.Sprintf("No Actions run for %s has started since the rollback began.", domain.ShortSHA(sha))
		return out, nil
	}
	return s.finish(mergeStatus(out, resolved)), nil
}

func (s *Service) statusForTask(ctx context.Context, task domain.BoardTask) (domain.DeployWatchStatus, error) {
	out := domain.DeployWatchStatus{
		TaskID:       task.ID,
		TaskKey:      task.Key,
		RepositoryID: task.RepositoryID,
		Env:          domain.DeployEnvProd,
		MergeSHA:     strings.TrimSpace(task.MergeCommitSHA),
		State:        domain.DeployWatchUnknown,
		Signal:       domain.DeploySignalNone,
		CheckedAt:    s.now(),
	}

	if s.targets != nil {
		if target, terr := s.targets.Get(ctx, task.RepositoryID, "", out.Env); terr == nil {
			out.HealthURL = target.HealthURL
			out.LogsURL = target.LogsURL
			out.AutoRollback = target.AutoRollback
		} else if !errors.Is(terr, port.ErrNotFound) {
			log.Warn().Err(terr).Str("task_id", task.ID.String()).Msg("deploy watch: reading deploy target failed")
		}
	}

	if out.MergeSHA == "" {
		out.Detail = "This task has no merge commit recorded — its pull request has not been merged, so nothing of it can be in production yet."
		return out, nil
	}

	resolved, err := s.resolveForCommit(ctx, task.RepositoryID, out.MergeSHA, "", time.Time{})
	if err != nil {
		return out, err
	}
	return s.finish(mergeStatus(out, resolved)), nil
}

// resolveForCommit is the Actions-run / commit-status resolution shared by
// statusForTask and StatusForCommit; only the workflow filter differs between
// the two callers. since is always zero here — StatusForCommitSince does its
// own resolution because a since-narrowed miss must not fall back to the
// commit-status signal (see its doc comment).
func (s *Service) resolveForCommit(ctx context.Context, repositoryID uuid.UUID, sha, workflow string, since time.Time) (domain.DeployWatchStatus, error) {
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return domain.DeployWatchStatus{}, fmt.Errorf("deploy watch: loading repository: %w", err)
	}
	owner, name, err := s.coords(ctx, repo)
	if err != nil {
		return domain.DeployWatchStatus{}, fmt.Errorf("deploy watch: resolving repository coordinates: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()

	if resolved, ok, rerr := s.actionsSignal(ctx, repositoryID, owner, name, sha, workflow, since); rerr != nil {
		return domain.DeployWatchStatus{}, rerr
	} else if ok {
		return resolved, nil
	}

	signal, err := s.actions.CommitDeployStatus(ctx, owner, name, sha)
	if err != nil {
		return domain.DeployWatchStatus{}, fmt.Errorf("deploy watch: reading commit deploy status: %w", err)
	}
	var out domain.DeployWatchStatus
	if signal.Kind == "" {
		out.State = domain.DeployWatchNoSignal
		out.Signal = domain.DeploySignalNone
		out.Detail = fmt.Sprintf("Nothing reports a deploy of %s: no Actions run carries a deploy job for it, and no commit status or GitHub Deployment was written against it. "+
			"This repository does not deploy on merge (or its deploy has not started yet and has left no trace).", domain.ShortSHA(sha))
		return out, nil
	}
	out.Signal = signal.Kind
	out.Contexts = signal.Contexts
	switch signal.State {
	case "success":
		out.State = domain.DeployWatchSuccess
	case "failure":
		out.State = domain.DeployWatchFailure
	default:
		out.State = domain.DeployWatchPending
	}
	out.Detail = describeCommitSignal(signal, sha)
	return out, nil
}

// actionsSignal resolves Actions runs for sha into a status, or (_, false,
// nil) when nothing there speaks to a deploy at all. workflow == "" considers
// every run for the commit, matching jobs by name and — for a run whose own
// workflow file is one of the repository's mapped prod/preprod deploy targets
// — the whole run's conclusion when no job matches by name. workflow != ""
// restricts to that workflow's runs outright, so every one of them counts as
// a deploy run by the same whole-run rule. since, when non-zero, drops runs
// started before it (StatusForCommitSince).
func (s *Service) actionsSignal(ctx context.Context, repositoryID uuid.UUID, owner, name, sha, workflow string, since time.Time) (domain.DeployWatchStatus, bool, error) {
	runs, err := s.deployRunsForCommit(ctx, owner, name, sha, workflow, since)
	if err != nil {
		return domain.DeployWatchStatus{}, false, err
	}
	if len(runs) == 0 {
		return domain.DeployWatchStatus{}, false, nil
	}

	var mappedRunIDs map[int64]bool
	if workflow == "" {
		mappedRunIDs = s.mappedDeployRunIDs(ctx, repositoryID, owner, name, sha)
	}

	out := domain.DeployWatchStatus{Signal: domain.DeploySignalActionsRun}
	found := false
	pending := false
	for _, run := range runs {
		jobs, jerr := s.actions.ListRunJobs(ctx, owner, name, run.ID)
		if jerr != nil {
			// A jobs-list failure must never read as "nothing here" — that
			// silently drops back to the commit-status fallback while an
			// Actions deploy may well be running.
			log.Warn().Err(jerr).Int64("run_id", run.ID).Msg("deploy watch: listing run jobs failed")
			out.State = domain.DeployWatchPending
			out.RunID = run.ID
			out.RunURL = run.HTMLURL
			out.Detail = "could not read the run's jobs"
			return out, true, nil
		}
		matchedJob := false
		for _, job := range jobs {
			if !matchesDeployJobName(job.Name) {
				continue
			}
			matchedJob = true
			found = true
			out.RunID = run.ID
			out.RunURL = run.HTMLURL
			switch {
			case job.Status != "completed":
				pending = true
			case job.Conclusion == "success":

			default:
				failed := domain.DeployWatchJob{
					ID: job.ID, Name: job.Name, Status: job.Status, Conclusion: job.Conclusion, URL: job.HTMLURL,
				}
				if failed.URL == "" {
					failed.URL = run.HTMLURL
				}
				out.State = domain.DeployWatchFailure
				out.FailedJob = &failed
				out.Detail = fmt.Sprintf("The deploy job %q of %s concluded %q.", job.Name, domain.ShortSHA(sha), job.Conclusion)
				return out, true, nil
			}
		}
		if matchedJob {
			continue
		}
		if workflow == "" && !mappedRunIDs[run.ID] {
			continue
		}
		found = true
		out.RunID = run.ID
		out.RunURL = run.HTMLURL
		switch {
		case run.Status != "completed":
			pending = true
		case run.Conclusion == "success":

		default:
			out.State = domain.DeployWatchFailure
			out.Detail = fmt.Sprintf("The run for %s concluded %q.", domain.ShortSHA(sha), run.Conclusion)
			return out, true, nil
		}
	}
	if !found {
		return domain.DeployWatchStatus{}, false, nil
	}
	if pending {
		out.State = domain.DeployWatchPending
		out.Detail = fmt.Sprintf("The deploy of %s is still running.", domain.ShortSHA(sha))
		return out, true, nil
	}
	out.State = domain.DeployWatchSuccess
	out.Detail = fmt.Sprintf("The deploy job for %s finished successfully.", domain.ShortSHA(sha))
	return out, true, nil
}

// deployRunsForCommit lists the Actions runs to inspect for sha: every run
// for the commit when workflow is unset, or just that workflow file's runs —
// listed by file rather than filtered by name afterwards, because a run does
// not otherwise carry its workflow file — narrowed to the ones whose head sha
// is the commit, and, when since is non-zero, to the ones started at or after
// it.
func (s *Service) deployRunsForCommit(ctx context.Context, owner, name, sha, workflow string, since time.Time) ([]port.ActionsRun, error) {
	var runs []port.ActionsRun
	if workflow == "" {
		found, err := s.actions.ListRunsForCommit(ctx, owner, name, sha)
		if err != nil {
			return nil, fmt.Errorf("deploy watch: listing runs for commit: %w", err)
		}
		runs = found
	} else {
		found, err := s.actions.ListWorkflowRuns(ctx, owner, name, workflow, "")
		if err != nil {
			return nil, fmt.Errorf("deploy watch: listing %s runs: %w", workflow, err)
		}
		out := make([]port.ActionsRun, 0, len(found))
		for _, r := range found {
			if strings.EqualFold(strings.TrimSpace(r.HeadSHA), sha) {
				out = append(out, r)
			}
		}
		runs = out
	}
	if since.IsZero() {
		return runs, nil
	}
	out := make([]port.ActionsRun, 0, len(runs))
	for _, r := range runs {
		if !r.RunStartedAt.Before(since) {
			out = append(out, r)
		}
	}
	return out, nil
}

// mappedDeployRunIDs answers, for the repository's mapped prod/preprod deploy
// workflows, which of their runs are for sha — the whole-run rule's other
// half: a run's own workflow file counting as a deploy run even when none of
// its jobs matches by name.
func (s *Service) mappedDeployRunIDs(ctx context.Context, repositoryID uuid.UUID, owner, name, sha string) map[int64]bool {
	out := map[int64]bool{}
	if s.pipeline == nil {
		return out
	}
	jobs, err := s.pipeline.ListByRepository(ctx, repositoryID)
	if err != nil {
		log.Warn().Err(err).Str("repository_id", repositoryID.String()).Msg("deploy watch: reading pipeline mappings failed")
		return out
	}
	seen := map[string]bool{}
	for _, j := range jobs {
		if j.Category != domain.PipelineCategoryProdDeploy && j.Category != domain.PipelineCategoryPreProdDeploy {
			continue
		}
		ref := strings.TrimSpace(j.TargetRef)
		key := strings.ToLower(ref)
		if ref == "" || seen[key] {
			continue
		}
		seen[key] = true
		runs, rerr := s.actions.ListWorkflowRuns(ctx, owner, name, ref, "")
		if rerr != nil {
			log.Warn().Err(rerr).Str("workflow", ref).Msg("deploy watch: listing mapped deploy workflow runs failed")
			continue
		}
		for _, r := range runs {
			if strings.EqualFold(strings.TrimSpace(r.HeadSHA), sha) {
				out[r.ID] = true
			}
		}
	}
	return out
}

// deployJobTokens / deployJobExcludedTokens are whole tokens, not substrings —
// "deployment-docs" must not match on "deploy" bleeding into "deployment"
// while missing that "docs" disqualifies the whole job.
var deployJobTokens = map[string]bool{
	"deploy": true, "deployment": true, "release": true, "publish": true, "ship": true, "rollout": true,
}

var deployJobExcludedTokens = map[string]bool{
	"docs": true, "doc": true, "drafter": true, "notes": true, "changelog": true, "storybook": true, "preview": true,
}

func matchesDeployJobName(jobName string) bool {
	matched := false
	for _, token := range tokenizeJobName(jobName) {
		if deployJobExcludedTokens[token] {
			return false
		}
		if deployJobTokens[token] {
			matched = true
		}
	}
	return matched
}

func tokenizeJobName(name string) []string {
	return strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	})
}

func mergeStatus(base, resolved domain.DeployWatchStatus) domain.DeployWatchStatus {
	base.State = resolved.State
	base.Signal = resolved.Signal
	base.Detail = resolved.Detail
	base.RunID = resolved.RunID
	base.RunURL = resolved.RunURL
	base.FailedJob = resolved.FailedJob
	if len(resolved.Contexts) > 0 {
		base.Contexts = resolved.Contexts
	}
	return base
}

func (s *Service) finish(status domain.DeployWatchStatus) domain.DeployWatchStatus {
	if status.State != domain.DeployWatchSuccess {
		return status
	}
	until := s.now().Add(s.healthWindow)
	status.HealthWindowUntil = &until
	status.Detail = strings.TrimSpace(status.Detail + fmt.Sprintf(
		" Production is now running this task's code; watch %s until %s — an incident opened before then is this release's.",
		healthLabel(status.HealthURL), until.Format(time.RFC3339)))
	return status
}

func healthLabel(healthURL string) string {
	if strings.TrimSpace(healthURL) == "" {
		return "the environment (no health_url is recorded — record one with update_deploy_target)"
	}
	return urlguard.LogRaw(healthURL)
}

func describeCommitSignal(signal port.CommitDeploySignal, sha string) string {
	who := strings.Join(signal.Contexts, ", ")
	if who == "" {
		who = signal.Environment
	}
	if who == "" {
		who = "the deploy provider"
	}
	verb := map[string]string{
		"success": "reported this deploy successful",
		"failure": "reported this deploy FAILED",
	}[signal.State]
	if verb == "" {
		verb = "has not finished this deploy yet"
	}
	out := fmt.Sprintf("%s %s for %s (no Actions deploy job exists — this repository deploys on push).",
		who, verb, domain.ShortSHA(sha))
	if d := strings.TrimSpace(signal.Description); d != "" {
		out += " " + d
	}
	return out
}

const (
	LogSourceActionsJob = "actions_job"
	LogSourceEndpoint   = "logs_url"
)

type LogResult struct {
	Source    string `json:"source"`
	Reference string `json:"reference,omitempty"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
	Note      string `json:"note,omitempty"`
}

func (s *Service) JobLogs(ctx context.Context, repositoryID uuid.UUID, jobID int64, maxChars int) (LogResult, error) {
	if s.actions == nil || s.coords == nil {
		return LogResult{}, ErrNotConfigured
	}
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return LogResult{}, err
	}
	owner, name, err := s.coords(ctx, repo)
	if err != nil {
		return LogResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()
	raw, err := s.actions.JobLogs(ctx, owner, name, jobID)
	if err != nil {
		return LogResult{}, fmt.Errorf("deploy watch: fetching job logs: %w", err)
	}
	content, truncated := SummarizeLog(raw, maxChars)
	return LogResult{
		Source:    LogSourceActionsJob,
		Reference: fmt.Sprintf("job %d", jobID),
		Content:   content,
		Truncated: truncated,
	}, nil
}

func (s *Service) EndpointLogs(ctx context.Context, repositoryID uuid.UUID, env string, maxChars int) (LogResult, error) {
	if s.targets == nil {
		return LogResult{}, ErrNotConfigured
	}
	target, err := s.targets.Get(ctx, repositoryID, "", env)
	if err != nil {
		return LogResult{}, err
	}
	raw := strings.TrimSpace(target.LogsURL)
	if raw == "" {
		return LogResult{}, fmt.Errorf("no logs_url is recorded for %s — record one with update_deploy_target if this application exposes a log endpoint", env)
	}

	ctx, cancel := context.WithTimeout(ctx, logFetchTimeout)
	defer cancel()
	dest, err := s.policy.Validate(ctx, raw)
	if err != nil {
		log.Warn().Err(err).Str("logs_url", urlguard.LogRaw(raw)).Msg("deploy watch: logs URL destination refused")
		return LogResult{}, errors.New("logs_url is not an allowed destination")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dest.URL.String(), nil)
	if err != nil {
		return LogResult{}, errors.New("logs_url is not an allowed destination")
	}
	resp, err := s.policy.ClientFor(dest, logFetchTimeout).Do(req)
	if err != nil {
		if errors.Is(err, urlguard.ErrBlocked) {
			log.Warn().Err(err).Str("logs_url", urlguard.LogValue(dest.URL)).Msg("deploy watch: logs URL destination refused at dial")
			return LogResult{}, errors.New("logs_url is not an allowed destination")
		}
		return LogResult{}, fmt.Errorf("fetching %s: %w", urlguard.LogRaw(raw), err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxLogBytes))
	content, truncated := SummarizeLog(string(body), maxChars)
	out := LogResult{
		Source:    LogSourceEndpoint,
		Reference: urlguard.LogRaw(raw),
		Content:   content,
		Truncated: truncated,
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		out.Note = fmt.Sprintf("the log endpoint answered HTTP %d — the body below is whatever it returned", resp.StatusCode)
	}
	return out, nil
}

const defaultLogChars = 6000

func SummarizeLog(raw string, maxChars int) (string, bool) {
	if maxChars <= 0 {
		maxChars = defaultLogChars
	}
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	if len(raw) <= maxChars {
		return strings.TrimSpace(raw), false
	}
	lines := strings.Split(raw, "\n")

	seen := map[string]bool{}
	var errorLines []string
	for _, line := range lines {
		if !looksLikeError(line) {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		errorLines = append(errorLines, trimmed)
		if len(errorLines) >= 40 {
			break
		}
	}

	var head string
	if len(errorLines) > 0 {
		head = "Error lines found in this log:\n" + strings.Join(errorLines, "\n") + "\n\n"
		if len(head) > maxChars/2 {
			head = head[:maxChars/2] + "\n…\n\n"
		}
	}
	budget := maxChars - len(head)
	if budget < 0 {
		budget = 0
	}
	tail := raw
	if len(tail) > budget {
		tail = tail[len(tail)-budget:]

		if idx := strings.IndexByte(tail, '\n'); idx >= 0 && idx < len(tail)-1 {
			tail = tail[idx+1:]
		}
	}
	return strings.TrimSpace(head + "…(earlier output truncated)\n" + tail), true
}

var errorMarkers = []string{
	"error", "failed", "failure", "fatal", "panic", "exception",
	"##[error]", "exit code 1", "exit status 1", "cannot ", "not found",
	"denied", "timed out", "refused",
}

func looksLikeError(line string) bool {
	lower := strings.ToLower(line)
	for _, marker := range errorMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
