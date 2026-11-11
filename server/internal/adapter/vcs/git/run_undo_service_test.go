package git

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/board"
	"github.com/makifbaysal/tasktrooper/server/internal/application/runtrace"
	"github.com/makifbaysal/tasktrooper/server/internal/application/runtrace/runtracetest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type undoRuns struct {
	port.TaskAgentRunStore
	run  domain.TaskAgentRun
	live bool
}

func (r undoRuns) GetByID(context.Context, uuid.UUID) (domain.TaskAgentRun, error) { return r.run, nil }
func (r undoRuns) HasLiveRunForTask(context.Context, uuid.UUID, time.Duration) (bool, error) {
	return r.live, nil
}

type undoTasks struct {
	port.BoardTaskStore
	task domain.BoardTask
}

func (t undoTasks) Get(context.Context, uuid.UUID, uuid.UUID) (domain.BoardTask, error) {
	return t.task, nil
}

type undoEvents struct {
	port.BoardEventStore
	created []domain.BoardEvent
	listed  []domain.BoardEvent
}

func (e *undoEvents) Create(_ context.Context, ev domain.BoardEvent) (domain.BoardEvent, error) {
	e.created = append(e.created, ev)
	return ev, nil
}
func (e *undoEvents) ListByTask(context.Context, uuid.UUID, int) ([]domain.BoardEvent, error) {
	return e.listed, nil
}

type undoCriteria struct {
	port.AcceptanceCriterionStore
	items   []domain.AcceptanceCriterion
	reopned []uuid.UUID
}

func (c *undoCriteria) ListByTask(context.Context, uuid.UUID) ([]domain.AcceptanceCriterion, error) {
	return c.items, nil
}
func (c *undoCriteria) UpdateCompleted(_ context.Context, id uuid.UUID, done bool) (domain.AcceptanceCriterion, error) {
	if !done {
		c.reopned = append(c.reopned, id)
	}
	return domain.AcceptanceCriterion{}, nil
}

type undoRepos struct {
	board.RepositoryResolver
	root string
}

func (r undoRepos) ResolveRepository(context.Context, uuid.UUID) (domain.Repository, error) {
	return domain.Repository{RootPath: r.root}, nil
}

type undoUpdater struct {
	board.TaskUpdater
	moved []domain.TaskColumn
}

func (u *undoUpdater) UpdateTask(_ context.Context, _, _ uuid.UUID, req domain.UpdateBoardTaskRequest) (domain.BoardTask, error) {
	if req.Column != nil {
		u.moved = append(u.moved, *req.Column)
	}
	return domain.BoardTask{}, nil
}

type undoHarness struct {
	f        undoFixture
	svc      *board.RunUndo
	traces   *runtracetest.MemStore
	events   *undoEvents
	criteria *undoCriteria
	updater  *undoUpdater
	runs     *undoRuns
	tasks    *undoTasks
	runID    uuid.UUID
	taskID   uuid.UUID
	repoID   uuid.UUID
}

func newUndoHarness(t *testing.T) *undoHarness {
	t.Helper()
	f := newUndoFixture(t)
	h := &undoHarness{f: f, traces: runtracetest.NewMemStore(), events: &undoEvents{}, updater: &undoUpdater{},
		runID: uuid.New(), taskID: uuid.New(), repoID: uuid.New()}
	started := time.Now().Add(-time.Hour)
	h.runs = &undoRuns{run: domain.TaskAgentRun{ID: h.runID, TaskID: h.taskID, AgentID: uuid.New(), Status: domain.TaskAgentRunStatusCompleted}}
	h.tasks = &undoTasks{task: domain.BoardTask{ID: h.taskID, Key: "TT-7", Column: domain.TaskColumn("code_review")}}
	h.criteria = &undoCriteria{items: []domain.AcceptanceCriterion{
		{ID: uuid.New(), Completed: true, Checks: []domain.CriterionCheck{{CheckedAt: time.Now()}}},
		{ID: uuid.New(), Completed: true, Checks: []domain.CriterionCheck{{CheckedAt: started.Add(-time.Hour)}}},
	}}
	require.NoError(t, h.traces.Begin(context.Background(), domain.RunTrace{
		RunID: h.runID, TaskID: h.taskID, AgentID: h.runs.run.AgentID, BaseSHA: f.base, Branch: f.branch,
		WorkspacePath: f.ws, ColumnBefore: "todo", StartedAt: started,
	}))
	rec := runtrace.Start(context.Background(), h.traces, runtrace.Meta{RunID: h.runID, TaskID: h.taskID, AgentID: h.runs.run.AgentID, WorkDir: f.ws})
	rec.Begin(runtrace.CallStart{Tool: "write_file", Arguments: `{"path":"created-by-run.txt"}`}).End(runtrace.CallResult{})
	rec.Begin(runtrace.CallStart{Tool: "http_request", Arguments: `{"method":"POST","url":"https://api.example.com/x"}`}).End(runtrace.CallResult{})
	h.rebuild(t)
	return h
}

func (h *undoHarness) rebuild(t *testing.T) {
	t.Helper()
	h.svc = board.NewRunUndo(board.RunUndoDeps{
		Traces: h.traces, Runs: h.runs, Tasks: h.tasks, Events: h.events, Git: NewClient(),
		Criteria: h.criteria, Repos: undoRepos{root: filepath.Dir(h.f.ws)}, Updater: h.updater,
	})
}

func TestPreviewListsWhatAnUndoWouldDoAndWhatItCannot(t *testing.T) {
	h := newUndoHarness(t)
	p, err := h.svc.Preview(context.Background(), h.repoID, h.taskID, h.runID)
	require.NoError(t, err)
	require.True(t, p.Supported, p.Reason)
	require.Equal(t, domain.UndoModeReset, p.Mode)
	require.Equal(t, domain.UndoBackupRef(h.runID), p.BackupRef)
	require.Len(t, p.Commits, 1)
	require.Len(t, p.Files, 4)
	require.Len(t, p.Uncommitted, 3)
	require.Len(t, p.NonUndoable, 1)
	require.Equal(t, "http_request", p.NonUndoable[0].Kind)
	require.Equal(t, "todo", p.MoveTo)
	require.False(t, p.Pushed)
}

func TestUndoNeedsTheConfirmation(t *testing.T) {
	h := newUndoHarness(t)
	_, err := h.svc.Undo(context.Background(), h.repoID, h.taskID, h.runID, domain.RunUndoRequest{})
	require.ErrorIs(t, err, domain.ErrUndoConfirmRequired)
	require.NotEqual(t, h.f.base, gitRun(t, h.f.ws, "rev-parse", "HEAD"))
}

func TestUndoResetsTheTaskBranchRecordsItAndMovesTheCardBack(t *testing.T) {
	h := newUndoHarness(t)
	mainBefore := gitRun(t, h.f.ws, "rev-parse", "origin/main")

	res, err := h.svc.Undo(context.Background(), h.repoID, h.taskID, h.runID, domain.RunUndoRequest{Confirm: true})
	require.NoError(t, err)
	require.Equal(t, h.f.base, gitRun(t, h.f.ws, "rev-parse", "HEAD"))
	require.Equal(t, domain.UndoBackupRef(h.runID), res.BackupRef)
	require.Equal(t, "created-by-run.txt", res.CleanedPaths[0])
	require.Equal(t, "todo", res.MovedTo)
	require.Equal(t, []domain.TaskColumn{"todo"}, h.updater.moved)
	require.Equal(t, 1, res.StaleCriteria, "only the verdict given during the run is reopened")
	require.Len(t, h.criteria.reopned, 1)
	require.Equal(t, mainBefore, gitRun(t, h.f.ws, "rev-parse", "origin/main"))
	require.False(t, res.PushPending)

	require.Len(t, h.events.created, 1)
	require.Equal(t, domain.BoardEventTaskRunUndone, h.events.created[0].EventType)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(h.events.created[0].Payload, &payload))
	require.Equal(t, res.BackupRef, payload["backup_ref"])

	undo, err := h.traces.List(context.Background(), h.runID, port.RunTraceFilter{Category: domain.TraceCategoryUndo})
	require.NoError(t, err)
	require.Len(t, undo, 1, "the undo is appended to the trace")
	all, _ := h.traces.List(context.Background(), h.runID, port.RunTraceFilter{})
	require.Len(t, all, 3, "the original trace is kept")

	again, err := h.svc.Preview(context.Background(), h.repoID, h.taskID, h.runID)
	require.NoError(t, err)
	require.Empty(t, again.Commits)
	require.Equal(t, []domain.TraceFileChange{{Path: "made-by-shell.log", Status: "?"}}, again.Files, "only the untracked file the trace did not list is left")
}

func TestUndoStaysPutWhenAPersonMovedTheCard(t *testing.T) {
	h := newUndoHarness(t)
	moved, _ := json.Marshal(map[string]any{domain.EventPayloadActor: domain.EventActorHuman})
	h.events.listed = []domain.BoardEvent{{EventType: domain.BoardEventTaskMoved, Payload: moved, CreatedAt: time.Now()}}
	p, err := h.svc.Preview(context.Background(), h.repoID, h.taskID, h.runID)
	require.NoError(t, err)
	require.Empty(t, p.MoveTo)
}

func TestUndoOfAPushedBranchResetsLocallyUntilForcePushIsConfirmed(t *testing.T) {
	h := newUndoHarness(t)
	gitRun(t, h.f.ws, "add", "-A")
	gitRun(t, h.f.ws, "commit", "-m", "TT-7: more")
	gitRun(t, h.f.ws, "push", "-u", "origin", h.f.branch)
	pushed := gitRun(t, h.f.ws, "rev-parse", "HEAD")

	p, err := h.svc.Preview(context.Background(), h.repoID, h.taskID, h.runID)
	require.NoError(t, err)
	require.True(t, p.Pushed)

	res, err := h.svc.Undo(context.Background(), h.repoID, h.taskID, h.runID, domain.RunUndoRequest{Confirm: true})
	require.NoError(t, err)
	require.True(t, res.PushPending)
	require.False(t, res.ForcePushed)
	require.Equal(t, pushed, gitRun(t, h.f.origin, "rev-parse", h.f.branch), "nothing is pushed without the go-ahead")

	h2 := newUndoHarness(t)
	gitRun(t, h2.f.ws, "add", "-A")
	gitRun(t, h2.f.ws, "commit", "-m", "TT-7: more")
	gitRun(t, h2.f.ws, "push", "-u", "origin", h2.f.branch)
	res, err = h2.svc.Undo(context.Background(), h2.repoID, h2.taskID, h2.runID, domain.RunUndoRequest{Confirm: true, ForcePush: true})
	require.NoError(t, err)
	require.True(t, res.ForcePushed)
	require.Equal(t, h2.f.base, gitRun(t, h2.f.origin, "rev-parse", h2.f.branch))
}

func TestUndoRefusesWhenTheRemoteBranchHoldsOtherCommits(t *testing.T) {
	h := newUndoHarness(t)
	gitRun(t, h.f.ws, "add", "-A")
	gitRun(t, h.f.ws, "commit", "-m", "TT-7: more")
	gitRun(t, h.f.ws, "push", "-u", "origin", h.f.branch)
	other := filepath.Join(t.TempDir(), "other")
	gitRun(t, filepath.Dir(other), "clone", "-b", h.f.branch, h.f.origin, other)
	write(t, other, "colleague.txt", "x\n")
	gitRun(t, other, "add", ".")
	gitRun(t, other, "commit", "-m", "colleague")
	gitRun(t, other, "push", "origin", h.f.branch)
	gitRun(t, h.f.ws, "fetch", "origin")

	_, err := h.svc.Undo(context.Background(), h.repoID, h.taskID, h.runID, domain.RunUndoRequest{Confirm: true, ForcePush: true})
	require.ErrorIs(t, err, domain.ErrUndoRemoteAhead)
	require.NotEqual(t, h.f.base, gitRun(t, h.f.ws, "rev-parse", "HEAD"), "nothing was reset")
}

func TestUndoIsBlockedWhileARunIsLiveOrOnTheWrongBranch(t *testing.T) {
	h := newUndoHarness(t)
	h.runs.live = true
	h.rebuild(t)
	p, err := h.svc.Preview(context.Background(), h.repoID, h.taskID, h.runID)
	require.NoError(t, err)
	require.Equal(t, domain.UndoBlockedLiveRun, p.Reason)
	_, err = h.svc.Undo(context.Background(), h.repoID, h.taskID, h.runID, domain.RunUndoRequest{Confirm: true})
	var blocked *domain.UndoBlockedError
	require.ErrorAs(t, err, &blocked)

	h.runs.live = false
	gitRun(t, h.f.ws, "stash", "-u")
	gitRun(t, h.f.ws, "checkout", "main")
	p, err = h.svc.Preview(context.Background(), h.repoID, h.taskID, h.runID)
	require.NoError(t, err)
	require.Equal(t, domain.UndoBlockedOtherBranch, p.Reason)
}

func TestARunWithoutATraceOrOnAnotherComputerIsNotUndone(t *testing.T) {
	h := newUndoHarness(t)
	other := uuid.New()
	h.runs.run.ID = other
	p, err := h.svc.Preview(context.Background(), h.repoID, h.taskID, other)
	require.NoError(t, err)
	require.Equal(t, domain.UndoBlockedNoTrace, p.Reason)

	remote := newUndoHarness(t)
	hdr, _ := remote.traces.Header(context.Background(), remote.runID)
	hdr.Remote = true
	remote.traces.Headers[remote.runID] = hdr
	p, err = remote.svc.Preview(context.Background(), remote.repoID, remote.taskID, remote.runID)
	require.NoError(t, err)
	require.Equal(t, domain.UndoBlockedRemote, p.Reason, "reported, never a silent failure")
}

func TestAMergedPullRequestIsRevertedNotRewritten(t *testing.T) {
	h := newUndoHarness(t)
	h.tasks.task.MergeCommitSHA = gitRun(t, h.f.ws, "rev-parse", "origin/main")
	h.tasks.task.PRURL = "https://github.com/o/r/pull/3"

	p, err := h.svc.Preview(context.Background(), h.repoID, h.taskID, h.runID)
	require.NoError(t, err)
	require.Equal(t, domain.UndoModeRevert, p.Mode)
	require.True(t, p.RevertAvailable)
	require.True(t, p.PullRequest.Merged)

	headBefore := gitRun(t, h.f.ws, "rev-parse", "HEAD")
	_, err = h.svc.Undo(context.Background(), h.repoID, h.taskID, h.runID, domain.RunUndoRequest{Confirm: true})
	require.ErrorIs(t, err, domain.ErrUndoUseRevert)
	require.Equal(t, headBefore, gitRun(t, h.f.ws, "rev-parse", "HEAD"))

	mirror := filepath.Join(t.TempDir(), "mirror")
	gitRun(t, filepath.Dir(mirror), "clone", h.f.origin, mirror)
	h.svc = board.NewRunUndo(board.RunUndoDeps{Traces: h.traces, Runs: h.runs, Tasks: h.tasks, Events: h.events, Git: NewClient(), Repos: undoRepos{root: mirror}})
	h.tasks.task.MergeCommitSHA = gitRun(t, mirror, "rev-parse", "HEAD")
	mainBefore := gitRun(t, h.f.origin, "rev-parse", "main")
	res, err := h.svc.Revert(context.Background(), h.repoID, h.taskID, h.runID, domain.RunUndoRequest{Confirm: true})
	require.NoError(t, err)
	require.Contains(t, res.Branch, "tasktrooper/revert-tt-7-")
	require.Equal(t, mainBefore, gitRun(t, h.f.origin, "rev-parse", "main"))
	require.NotEmpty(t, gitRun(t, h.f.origin, "rev-parse", res.Branch))
	require.Len(t, h.events.created, 1)
}
