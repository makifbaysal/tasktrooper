package board

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/application/runtrace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type RunUndoDeps struct {
	Traces   port.RunTraceStore
	Runs     port.TaskAgentRunStore
	Tasks    port.BoardTaskStore
	Events   port.BoardEventStore
	Git      port.RunUndoGit
	Criteria port.AcceptanceCriterionStore
	Repos    RepositoryResolver
	Updater  TaskUpdater
}

// RunUndo takes a run's work back: it previews what a reset of the task branch
// would do, does it behind a backup, and for a merged pull request opens a
// revert instead of rewriting history.
type RunUndo struct{ d RunUndoDeps }

func NewRunUndo(d RunUndoDeps) *RunUndo { return &RunUndo{d: d} }

type undoSubject struct {
	run    domain.TaskAgentRun
	header domain.RunTrace
	task   domain.BoardTask
}

func (u *RunUndo) load(ctx context.Context, repositoryID, taskID, runID uuid.UUID) (undoSubject, error) {
	var s undoSubject
	run, err := u.d.Runs.GetByID(ctx, runID)
	if err != nil {
		return s, err
	}
	if run.TaskID != taskID {
		return s, domain.ErrTaskAgentRunNotFound
	}
	s.run = run
	if s.task, err = u.d.Tasks.Get(ctx, repositoryID, taskID); err != nil {
		return s, err
	}
	s.header, err = u.d.Traces.Header(ctx, runID)
	return s, err
}

func blocked(p domain.RunUndoPreview, reason string) domain.RunUndoPreview {
	p.Supported = false
	p.Reason = reason
	return p
}

func (u *RunUndo) Preview(ctx context.Context, repositoryID, taskID, runID uuid.UUID) (domain.RunUndoPreview, error) {
	p := domain.RunUndoPreview{
		RunID: runID, Mode: domain.UndoModeReset, BackupRef: domain.UndoBackupRef(runID),
		Commits: []domain.RunUndoCommit{}, Files: []domain.TraceFileChange{}, Uncommitted: []domain.TraceFileChange{},
		NonUndoable: []domain.RunTraceEffect{},
	}
	s, err := u.load(ctx, repositoryID, taskID, runID)
	if err == domain.ErrRunTraceNotFound {
		return blocked(p, domain.UndoBlockedNoTrace), nil
	}
	if err != nil {
		return p, err
	}
	if effects, err := u.d.Traces.Effects(ctx, runID); err == nil {
		p.NonUndoable = runtrace.SummaryEffects(effects)
	}
	p.Branch, p.BaseSHA = s.header.Branch, s.header.BaseSHA
	if s.task.PRURL != "" {
		p.PullRequest = &domain.RunUndoPullRequest{URL: s.task.PRURL, Number: s.task.PRNumber, Merged: s.task.MergeCommitSHA != "", MergeSHA: s.task.MergeCommitSHA}
	}

	if s.task.MergeCommitSHA != "" {
		p.Mode = domain.UndoModeRevert
		p.RevertAvailable = !s.header.Remote
		return blocked(p, domain.UndoBlockedMerged), nil
	}
	if s.header.Remote {
		return blocked(p, domain.UndoBlockedRemote), nil
	}
	if !domain.TaskAgentRunIsTerminal(s.run.Status) {
		return blocked(p, domain.UndoBlockedLiveRun), nil
	}
	if live, err := u.d.Runs.HasLiveRunForTask(ctx, taskID, runLiveWithin); err == nil && live {
		return blocked(p, domain.UndoBlockedLiveRun), nil
	}
	if s.header.BaseSHA == "" {
		return blocked(p, domain.UndoBlockedNoBase), nil
	}
	ws := s.header.WorkspacePath
	if info, err := os.Stat(ws); ws == "" || err != nil || !info.IsDir() {
		return blocked(p, domain.UndoBlockedNoWorkspace), nil
	}
	state, err := u.d.Git.UndoState(ctx, ws, s.header.BaseSHA, s.header.Branch)
	if err != nil {
		log.Warn().Err(err).Str("run_id", runID.String()).Msg("run undo: the checkout could not be read")
		return blocked(p, domain.UndoBlockedGit), nil
	}
	p.HeadSHA = state.Head
	switch {
	case s.header.Branch == "" || state.Branch != s.header.Branch:
		return blocked(p, domain.UndoBlockedOtherBranch), nil
	case state.Branch == state.DefaultBranch:
		return blocked(p, domain.UndoBlockedDefaultBranch), nil
	case !state.BaseIsAncestor:
		return blocked(p, domain.UndoBlockedBaseDiverged), nil
	}
	for _, c := range state.Commits {
		p.Commits = append(p.Commits, domain.RunUndoCommit{SHA: c.SHA, Subject: c.Subject})
	}
	for _, f := range state.Changes {
		p.Files = append(p.Files, domain.TraceFileChange{Path: f.Path, Status: f.Status, From: f.From})
	}
	for _, f := range state.Uncommitted {
		p.Uncommitted = append(p.Uncommitted, domain.TraceFileChange{Path: f.Path, Status: f.Status, From: f.From})
	}
	p.Pushed, p.RemoteBranchSHA, p.RemoteAhead = state.Pushed, state.RemoteBranchSHA, state.RemoteAhead
	if len(p.Commits) == 0 && len(p.Files) == 0 {
		p.Mode = domain.UndoModeNothing
		if u.undoneBefore(ctx, runID) {
			return blocked(p, domain.UndoBlockedAlreadyUndone), nil
		}
		return blocked(p, domain.UndoBlockedNothing), nil
	}
	p.Supported = true
	p.MoveTo = u.returnColumn(ctx, s)
	return p, nil
}

func (u *RunUndo) undoneBefore(ctx context.Context, runID uuid.UUID) bool {
	events, err := u.d.Traces.List(ctx, runID, port.RunTraceFilter{Category: domain.TraceCategoryUndo, Limit: 50})
	if err != nil {
		return false
	}
	for _, ev := range events {
		if ev.Status == domain.TraceStatusOK {
			return true
		}
	}
	return false
}

// returnColumn is the column the card goes back to, or "" when it stays: only
// when the run was dispatched from another column and no person has moved the
// card since the run began.
func (u *RunUndo) returnColumn(ctx context.Context, s undoSubject) string {
	before := s.header.ColumnBefore
	if before == "" || before == string(s.task.Column) || u.d.Events == nil {
		return ""
	}
	events, err := u.d.Events.ListByTask(ctx, s.task.ID, 200)
	if err != nil {
		return ""
	}
	for _, ev := range events {
		if ev.EventType != domain.BoardEventTaskMoved || ev.CreatedAt.Before(s.header.StartedAt) {
			continue
		}
		var payload map[string]any
		_ = json.Unmarshal(ev.Payload, &payload)
		if payload[domain.EventPayloadActor] == domain.EventActorHuman {
			return ""
		}
	}
	return before
}

func tracedChangePaths(events []domain.RunTraceEvent) []string {
	seen := map[string]bool{}
	var out []string
	for _, ev := range events {
		if ev.Status == domain.TraceStatusError {
			continue
		}
		var d struct {
			Path string `json:"path"`
			To   string `json:"to"`
		}
		_ = json.Unmarshal(ev.Detail, &d)
		for _, p := range []string{d.Path, d.To} {
			if p != "" && !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

func (u *RunUndo) Undo(ctx context.Context, repositoryID, taskID, runID uuid.UUID, req domain.RunUndoRequest) (domain.RunUndoResult, error) {
	var res domain.RunUndoResult
	if !req.Confirm {
		return res, domain.ErrUndoConfirmRequired
	}
	p, err := u.Preview(ctx, repositoryID, taskID, runID)
	if err != nil {
		return res, err
	}
	if p.Mode == domain.UndoModeRevert {
		return res, domain.ErrUndoUseRevert
	}
	if !p.Supported {
		return res, &domain.UndoBlockedError{Reason: p.Reason}
	}
	if req.ForcePush && p.Pushed && p.RemoteAhead {
		return res, domain.ErrUndoRemoteAhead
	}
	s, err := u.load(ctx, repositoryID, taskID, runID)
	if err != nil {
		return res, err
	}
	changes, _ := u.d.Traces.Changes(ctx, runID)
	outcome, err := u.d.Git.UndoReset(ctx, s.header.WorkspacePath, port.UndoPlan{
		Branch: s.header.Branch, Base: s.header.BaseSHA, BackupRef: p.BackupRef, CleanPaths: tracedChangePaths(changes),
	})
	if err != nil {
		return res, err
	}
	res = domain.RunUndoResult{
		RunID: runID, Mode: domain.UndoModeReset, BackupRef: outcome.BackupRef, ResetTo: s.header.BaseSHA,
		CleanedPaths: nonNil(outcome.CleanedPaths), LeftUntracked: nonNil(outcome.LeftUntracked), NonUndoable: p.NonUndoable,
	}
	if p.Pushed {
		if req.ForcePush {
			if err := u.d.Git.ForcePushBranch(ctx, s.header.WorkspacePath, s.header.Branch, p.RemoteBranchSHA); err != nil {
				log.Warn().Err(err).Str("run_id", runID.String()).Msg("run undo: the reset branch could not be force-pushed")
				res.PushPending = true
			} else {
				res.ForcePushed = true
			}
		} else {
			res.PushPending = true
		}
	}
	res.StaleCriteria = u.staleCriteria(ctx, taskID, s.header.StartedAt)
	if p.MoveTo != "" && !req.KeepColumn && u.d.Updater != nil {
		col := domain.TaskColumn(p.MoveTo)
		if _, err := u.d.Updater.UpdateTask(ctx, repositoryID, taskID, domain.UpdateBoardTaskRequest{Column: &col, Actor: domain.TaskActorHuman}); err != nil {
			log.Warn().Err(err).Str("task_id", taskID.String()).Msg("run undo: the card could not be moved back")
		} else {
			res.MovedTo = p.MoveTo
		}
	}
	u.recordEvent(ctx, repositoryID, taskID, runID, s.run.AgentID, map[string]any{
		"mode": res.Mode, "backup_ref": res.BackupRef, "reset_to": res.ResetTo, "commits": len(p.Commits),
		"files": len(p.Files), "force_pushed": res.ForcePushed, "push_pending": res.PushPending,
		"moved_to": res.MovedTo, "stale_criteria": res.StaleCriteria,
	}, outcome.BackupRef, map[string]any{
		"action": "reset", "base_sha": s.header.BaseSHA, "head_before": p.HeadSHA, "backup_ref": outcome.BackupRef,
		"commits": p.Commits, "files": len(p.Files), "cleaned_paths": res.CleanedPaths, "left_untracked": res.LeftUntracked,
		"force_pushed": res.ForcePushed, "push_pending": res.PushPending, "moved_to": res.MovedTo,
	}, s.header)
	return res, nil
}

func nonNil(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// staleCriteria reopens every criterion a verdict was given on during the run:
// the verdicts judged work that no longer exists. The checks stay as history.
func (u *RunUndo) staleCriteria(ctx context.Context, taskID uuid.UUID, since time.Time) int {
	if u.d.Criteria == nil {
		return 0
	}
	items, err := u.d.Criteria.ListByTask(ctx, taskID)
	if err != nil {
		return 0
	}
	n := 0
	for _, c := range items {
		judged := false
		for _, check := range c.Checks {
			if !check.CheckedAt.Before(since) {
				judged = true
				break
			}
		}
		if !judged || !c.Completed {
			continue
		}
		if _, err := u.d.Criteria.UpdateCompleted(ctx, c.ID, false); err == nil {
			n++
		}
	}
	return n
}

func (u *RunUndo) recordEvent(ctx context.Context, repositoryID, taskID, runID, agentID uuid.UUID, payload map[string]any, target string, detail map[string]any, header domain.RunTrace) {
	payload["run_id"] = runID.String()
	payload[domain.EventPayloadActor] = domain.EventActorHuman
	payload[domain.EventPayloadReason] = domain.MoveReasonRunUndone
	if raw, err := json.Marshal(payload); err == nil && u.d.Events != nil {
		var actor *string
		if uid := registry.ActorUserIDFromContext(ctx); uid != "" {
			actor = &uid
		}
		if _, err := u.d.Events.Create(ctx, domain.BoardEvent{
			RepositoryID: repositoryID, TaskID: taskID, EventType: domain.BoardEventTaskRunUndone, Payload: raw, ActorUserID: actor,
		}); err != nil {
			log.Warn().Err(err).Str("run_id", runID.String()).Msg("run undo: the task event could not be written")
		}
	}
	rec := runtrace.Start(ctx, u.d.Traces, runtrace.Meta{RunID: runID, TaskID: taskID, AgentID: agentID, BaseSHA: header.BaseSHA})
	raw, _ := json.Marshal(detail)
	rec.Note(ctx, domain.RunTraceEvent{
		Tool: "undo_run", Source: domain.TraceSourceUndo, Category: domain.TraceCategoryUndo,
		Target: target, Status: domain.TraceStatusOK, Detail: raw,
	})
}

func (u *RunUndo) Revert(ctx context.Context, repositoryID, taskID, runID uuid.UUID, req domain.RunUndoRequest) (domain.RunRevertResult, error) {
	var res domain.RunRevertResult
	if !req.Confirm {
		return res, domain.ErrUndoConfirmRequired
	}
	s, err := u.load(ctx, repositoryID, taskID, runID)
	if err == domain.ErrRunTraceNotFound {
		return res, &domain.UndoBlockedError{Reason: domain.UndoBlockedNoTrace}
	}
	if err != nil {
		return res, err
	}
	if s.task.MergeCommitSHA == "" {
		return res, domain.ErrUndoNotMerged
	}
	if s.header.Remote {
		return res, &domain.UndoBlockedError{Reason: domain.UndoBlockedRemote}
	}
	repo, err := u.d.Repos.ResolveRepository(ctx, repositoryID)
	if err != nil {
		return res, err
	}
	branch := "tasktrooper/revert-" + strings.ToLower(strings.TrimSpace(s.task.Key)) + "-" + runID.String()[:8]
	out, err := u.d.Git.RevertMerge(ctx, repo.RootPath, port.RevertRequest{MergeSHA: s.task.MergeCommitSHA, Branch: branch})
	if err != nil {
		return res, err
	}
	res = domain.RunRevertResult{RunID: runID, Branch: out.Branch, PRURL: out.PRURL, PRError: out.PRError, MergeSHA: s.task.MergeCommitSHA}
	u.recordEvent(ctx, repositoryID, taskID, runID, s.run.AgentID, map[string]any{
		"mode": domain.UndoModeRevert, "revert_branch": out.Branch, "pr_url": out.PRURL, "merge_sha": s.task.MergeCommitSHA,
	}, out.Branch, map[string]any{
		"action": "revert", "merge_sha": s.task.MergeCommitSHA, "branch": out.Branch, "pr_url": out.PRURL, "pr_error": out.PRError,
	}, s.header)
	return res, nil
}
