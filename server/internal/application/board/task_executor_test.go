package board

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// agentCatalog hands the run one agent and no skills or rules. Only the three
// calls execute() makes before it picks an executor are meaningful; the rest of
// port.CatalogStore is embedded so this stays a fake of the seam, not of the
// interface.
type agentCatalog struct {
	port.CatalogStore
	agent domain.Agent
}

func (c *agentCatalog) GetAgent(context.Context, uuid.UUID) (domain.Agent, error) {
	return c.agent, nil
}
func (c *agentCatalog) ListSkillsByAgent(context.Context, uuid.UUID) ([]domain.Skill, error) {
	return nil, nil
}
func (c *agentCatalog) ListEnabledRulesByAgent(context.Context, uuid.UUID) ([]domain.OrchestratorRule, error) {
	return nil, nil
}

// oneRepoResolver points every run at one directory. No git client is wired in
// the tests below, so this IS the run's working directory — which is what the
// executor is asserted to be started in.
type oneRepoResolver struct{ root string }

func (r oneRepoResolver) ResolveRootPath(context.Context, uuid.UUID) (string, error) {
	return r.root, nil
}
func (r oneRepoResolver) ResolveDescription(context.Context, uuid.UUID) (string, error) {
	return "", nil
}
func (r oneRepoResolver) ResolveRepository(context.Context, uuid.UUID) (domain.Repository, error) {
	return domain.Repository{Name: "demo", RootPath: r.root}, nil
}
func (r oneRepoResolver) ProfileForRun(context.Context, uuid.UUID, string) string { return "" }

// recordingRunStore keeps the last row written, which is where a parked or
// failed run leaves its verdict.
type recordingRunStore struct {
	countingRunStore
	mu   sync.Mutex
	last domain.TaskAgentRun
	prev []domain.TaskAgentRun
}

func (s *recordingRunStore) Update(_ context.Context, run domain.TaskAgentRun) (domain.TaskAgentRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.last = run
	return run, nil
}

func (s *recordingRunStore) ListByTask(context.Context, uuid.UUID, int) ([]domain.TaskAgentRun, error) {
	return s.prev, nil
}

func (s *recordingRunStore) row() domain.TaskAgentRun {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last
}

// fakeExecutor stands in for the Claude Code CLI: it records what it was asked
// to run and answers with whatever the test put there.
type fakeExecutor struct {
	supports domain.LLMProviderType
	resp     domain.AgentResponse
	err      error

	mu      sync.Mutex
	calls   int
	lastReq domain.TaskExecution
	// reqs keeps every request in order, for a test that needs to compare a
	// follow-up call against the main one (lastReq only keeps the latest).
	reqs []domain.TaskExecution
}

func (f *fakeExecutor) Supports(provider domain.LLMProviderType) bool {
	return provider == f.supports
}

func (f *fakeExecutor) Execute(_ context.Context, req domain.TaskExecution) (domain.AgentResponse, error) {
	f.mu.Lock()
	f.calls++
	f.lastReq = req
	f.reqs = append(f.reqs, req)
	f.mu.Unlock()
	return f.resp, f.err
}

func (f *fakeExecutor) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeExecutor) request() domain.TaskExecution {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastReq
}

func (f *fakeExecutor) requests() []domain.TaskExecution {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.TaskExecution, len(f.reqs))
	copy(out, f.reqs)
	return out
}

// blockRecorder captures the park. previous is what the store hands back as the
// column the task was parked out of, which the runner turns into from_column.
type blockRecorder struct {
	mu       sync.Mutex
	resource string
	detail   string
	previous domain.TaskColumn
	err      error
}

func (b *blockRecorder) BlockOnQuestion(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string) error {
	return nil
}

func (b *blockRecorder) BlockOnResource(_ context.Context, _, _ uuid.UUID, resource, detail string) (domain.TaskColumn, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.resource, b.detail = resource, detail
	if b.err != nil {
		return "", b.err
	}
	return b.previous, nil
}

func (b *blockRecorder) parked() (string, string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.resource, b.detail
}

func executorRunner(t *testing.T, agent domain.Agent, runs *recordingRunStore, ex port.TaskExecutor) (*Runner, RunJob) {
	t.Helper()
	r := NewRunner(RunnerDeps{
		Runs:         runs,
		Catalog:      &agentCatalog{agent: agent},
		Repositories: oneRepoResolver{root: t.TempDir()},
	})
	if ex != nil {
		r.SetTaskExecutor(ex)
	}
	taskID := uuid.New()
	job := RunJob{
		Run:  domain.TaskAgentRun{ID: uuid.New(), TaskID: taskID, AgentID: agent.ID},
		Task: domain.BoardTask{ID: taskID, RepositoryID: uuid.New(), Key: "tt-42", Title: "Executor seam", Column: domain.TaskColumnInProgress},
	}
	return r, job
}

func claudeCodeAgent() domain.Agent {
	return domain.Agent{ID: uuid.New(), Name: "backend-developer", ProviderType: domain.LLMProviderClaudeCode, Model: "opus"}
}

// The whole point of the seam: a claude_code agent's task goes to the executor,
// in the run's own workspace, with the history the runner assembled.
func TestClaudeCodeAgentIsRunByTheExecutor(t *testing.T) {
	runs := &recordingRunStore{}
	ex := &fakeExecutor{
		supports: domain.LLMProviderClaudeCode,
		resp:     domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: "Done: added the seam."}},
	}
	r, job := executorRunner(t, claudeCodeAgent(), runs, ex)

	require.NoError(t, r.execute(context.Background(), job))

	assert.Equal(t, 1, ex.callCount(), "the executor must have been used")
	req := ex.request()
	assert.Equal(t, domain.LLMProviderClaudeCode, req.Provider)
	assert.Equal(t, "opus", req.Model)
	assert.Equal(t, "tt-42", req.TaskKey)
	assert.NotEmpty(t, req.WorkDir, "an executor with no workspace would run in the wrong tree")
	assert.NotEmpty(t, req.History, "the run's assembled context has to travel with it")
	assert.Equal(t, domain.RoleSystem, req.History[0].Role)
	assert.Empty(t, req.ResumeSessionID, "a task with no parked run behind it starts a fresh session")

	row := runs.row()
	assert.Equal(t, domain.TaskAgentRunStatusCompleted, row.Status)
	assert.Equal(t, "Done: added the seam.", row.Summary)
}

// criteriaSweepRunner wires a Runner whose initial execution AND its
// criteria-sweep rounds both go through the same fake executor — the sweep
// dials r.agentLoop.RunTask, a different seam from the initial r.taskExecutor
// call, and both have to be wired or the sweep panics on a nil agentLoop.
func criteriaSweepRunner(t *testing.T, runs *recordingRunStore, ex port.TaskExecutor, updater TaskUpdater) (*Runner, RunJob) {
	t.Helper()
	agentRec := claudeCodeAgent()
	router, _ := hostRouter(ex)
	r := NewRunner(RunnerDeps{
		AgentLoop:    router,
		Runs:         runs,
		Catalog:      &agentCatalog{agent: agentRec},
		Repositories: oneRepoResolver{root: t.TempDir()},
	})
	r.SetTaskExecutor(ex)
	r.SetTaskUpdater(updater)
	taskID := uuid.New()
	job := RunJob{
		Run:  domain.TaskAgentRun{ID: uuid.New(), TaskID: taskID, AgentID: agentRec.ID},
		Task: domain.BoardTask{ID: taskID, RepositoryID: uuid.New(), Key: "tt-42", Title: "Executor seam", Column: domain.TaskColumnInProgress},
	}
	return r, job
}

// A run that exhausted the criteria sweep with a criterion still open must not
// be recorded as a clean finish: T-6 sat quietly in in_progress because the old
// behaviour marked it Completed, and nothing ever revisited it again. Failed is
// what puts the task back on the reconciler's retry_failed_run path.
func TestRunExhaustingTheCriteriaSweepIsRecordedAsFailed(t *testing.T) {
	runs := &recordingRunStore{}
	ex := &fakeExecutor{
		supports: domain.LLMProviderClaudeCode,
		resp:     domain.AgentResponse{Message: domain.Message{Content: "done, I think"}},
	}
	r, job := criteriaSweepRunner(t, runs, ex, &criteriaUpdater{criteria: []domain.AcceptanceCriterion{
		{ID: uuid.New(), Text: "the export includes archived rows"},
	}})

	require.NoError(t, r.execute(context.Background(), job))

	row := runs.row()
	assert.Equal(t, domain.TaskAgentRunStatusFailed, row.Status,
		"a sweep that exhausted its rounds with an open criterion is not a clean finish")
	assert.Contains(t, row.Summary, "1", "the summary must say how many criteria stayed open")
	assert.Contains(t, row.Summary, "still open")
}

// The counterpart: a run that settles every criterion, by ticking or
// cancelling, keeps the exact behaviour it had before — Completed.
func TestRunSettlingEveryCriterionStaysCompleted(t *testing.T) {
	runs := &recordingRunStore{}
	ex := &fakeExecutor{
		supports: domain.LLMProviderClaudeCode,
		resp:     domain.AgentResponse{Message: domain.Message{Content: "all done"}},
	}
	r, job := criteriaSweepRunner(t, runs, ex, &settlingCriteriaUpdater{
		criteria:    []domain.AcceptanceCriterion{{ID: uuid.New(), Text: "the export includes archived rows"}},
		settleAfter: 1,
	})

	require.NoError(t, r.execute(context.Background(), job))

	row := runs.row()
	assert.Equal(t, domain.TaskAgentRunStatusCompleted, row.Status, "no regression: a settled sweep still completes")
}

// Without the binary there is no executor, and the run must fail with a
// sentence a human can act on — not fall through to an agent loop that would
// try to open an HTTP connection to a provider that has no endpoint.
func TestClaudeCodeAgentWithoutAnExecutorFailsClearly(t *testing.T) {
	runs := &recordingRunStore{}
	r, job := executorRunner(t, claudeCodeAgent(), runs, nil)

	err := r.execute(context.Background(), job)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "binary not available on this host")
	assert.Contains(t, err.Error(), "claude_code provider")

	row := runs.row()
	assert.Equal(t, domain.TaskAgentRunStatusFailed, row.Status)
	assert.Contains(t, row.Summary, "binary not available on this host",
		"the reason belongs on the run row, which is what the task detail shows")
	assert.Contains(t, row.Summary, "CLAUDE_CODE_BIN", "and it has to name the way out")
}

// A registered executor that does not support this provider is the same
// situation as none at all: the run must not be handed to it.
func TestClaudeCodeAgentWithAnUnsupportedExecutorFailsClearly(t *testing.T) {
	runs := &recordingRunStore{}
	ex := &fakeExecutor{supports: "some-other-cli"}
	r, job := executorRunner(t, claudeCodeAgent(), runs, ex)

	err := r.execute(context.Background(), job)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "binary not available on this host")
	assert.Contains(t, err.Error(), "claude_code provider")
	assert.Zero(t, ex.callCount(), "an executor that says it does not support the provider must not be called")
}

// Every other provider keeps the path it had. The loop is a concrete type that
// cannot be faked here, so the assertion is the one that matters for this
// change: the executor is not consulted, and the run does not take the
// host-executor branch.
func TestOtherProvidersNeverReachTheExecutor(t *testing.T) {
	for _, provider := range []domain.LLMProviderType{
		domain.LLMProviderAnthropic,
		domain.LLMProviderOpenAI,
		domain.LLMProviderLocal,
		// A named endpoint's uuid is also a legal provider value on an agent.
		domain.LLMProviderType("6f1a1f9e-0f1e-4a4a-9d9a-000000000001"),
	} {
		t.Run(string(provider), func(t *testing.T) {
			runs := &recordingRunStore{}
			agent := claudeCodeAgent()
			agent.ProviderType = provider
			ex := &fakeExecutor{supports: domain.LLMProviderClaudeCode}
			r, job := executorRunner(t, agent, runs, ex)

			// The loop is nil in this runner, so taking the loop path panics the
			// worker goroutine — which is exactly what the recovering worker is
			// for in production and is not what this test measures. Running
			// execute directly, the assertion is on the executor never being
			// consulted; the panic is recovered here.
			func() {
				defer func() { _ = recover() }()
				_ = r.execute(context.Background(), job)
			}()

			assert.Zero(t, ex.callCount(), "%s must not be routed to the claude code executor", provider)
		})
	}
}

// A usage limit is a park, not a failure. The run row keeps everything the
// resume needs (the reset time and the CLI session), the card moves to blocked
// on the quota, and the task keeps all three of its consecutive-failure lives.
func TestQuotaBlockParksTheTaskInsteadOfFailingIt(t *testing.T) {
	resumeAt := time.Now().Add(2 * time.Hour).Round(time.Second)
	runs := &recordingRunStore{}
	ex := &fakeExecutor{
		supports: domain.LLMProviderClaudeCode,
		err: &domain.QuotaBlock{
			ResumeAt:     resumeAt,
			CLISessionID: "sess-parked-1",
			Detail:       "Claude AI usage limit reached|4102444800",
		},
	}
	r, job := executorRunner(t, claudeCodeAgent(), runs, ex)
	blocker := &blockRecorder{}
	r.SetTaskBlocker(blocker)

	require.NoError(t, r.execute(context.Background(), job), "a park is not an error the worker should log as a failed run")

	row := runs.row()
	assert.NotEqual(t, domain.TaskAgentRunStatusFailed, row.Status, "parking must not spend a consecutive-failure life")
	assert.Equal(t, "sess-parked-1", row.CLISessionID, "the resume continues this session")
	require.NotNil(t, row.QuotaResumeAt)
	assert.True(t, resumeAt.Equal(*row.QuotaResumeAt), "the sweeper reads the reset time off the row")
	assert.Contains(t, row.Summary, "usage limit reached")

	resource, detail := blocker.parked()
	assert.Equal(t, domain.ResourceClaudeCodeQuota, resource)
	assert.Contains(t, detail, "resumes automatically")
	assert.True(t, domain.ValidResource(resource), "a resource no sweeper looks for would park the task forever")
}

// The park is a MOVE, and the board only knows about moves it was told about.
// Without this event the card appeared in `blocked` with its history ending at
// "started work" hours earlier, and the span ledger counted the whole wait as
// time spent in the working column.
func TestQuotaParkIsRecordedOnTheBoard(t *testing.T) {
	events, spans := &parkEventStore{}, &parkSpanStore{}
	runs := &recordingRunStore{}
	ex := &fakeExecutor{
		supports: domain.LLMProviderClaudeCode,
		err:      &domain.QuotaBlock{ResumeAt: time.Now().Add(time.Hour), CLISessionID: "sess-park"},
	}
	r, job := executorRunner(t, claudeCodeAgent(), runs, ex)
	// The store reports where the card was parked OUT of; the runner must use
	// that and not the column it happens to hold in memory.
	r.SetTaskBlocker(&blockRecorder{previous: domain.TaskColumnNeedRevision})
	r.SetParkJournal(NewParkJournal(events, spans))

	require.NoError(t, r.execute(context.Background(), job))

	payloads := events.payloads()
	require.Len(t, payloads, 1, "exactly one move, written directly and never through the dispatcher")
	assert.Equal(t, string(domain.TaskColumnNeedRevision), payloads[0]["from_column"])
	assert.Equal(t, string(domain.TaskColumnBlocked), payloads[0]["to_column"])
	assert.Equal(t, domain.EventActorSystem, payloads[0][domain.EventPayloadActor])
	assert.Equal(t, domain.MoveReasonQuotaExhausted, payloads[0][domain.EventPayloadReason])
	assert.Equal(t, domain.ResourceClaudeCodeQuota, payloads[0]["resource"])

	moves := spans.recorded()
	require.Len(t, moves, 1, "the open span has to close, or the wait is billed as work")
	assert.Equal(t, string(domain.TaskColumnBlocked), moves[0].column)
}

// The device/deploy/work-order park had the same hole and closes the same way,
// with the resource-block reason rather than the quota's.
func TestResourceParkIsRecordedOnTheBoard(t *testing.T) {
	events, spans := &parkEventStore{}, &parkSpanStore{}
	runs := &recordingRunStore{}
	ex := &fakeExecutor{
		supports: domain.LLMProviderClaudeCode,
		resp: domain.AgentResponse{
			Message:       domain.Message{Content: "waiting"},
			ResourceBlock: &domain.ResourceBlock{Resource: domain.ResourceMobileDevice, Detail: "every phone is taken"},
		},
	}
	r, job := executorRunner(t, claudeCodeAgent(), runs, ex)
	r.SetTaskBlocker(&blockRecorder{previous: domain.TaskColumnInQA})
	r.SetParkJournal(NewParkJournal(events, spans))

	require.NoError(t, r.execute(context.Background(), job))

	payloads := events.payloads()
	require.Len(t, payloads, 1)
	assert.Equal(t, string(domain.TaskColumnInQA), payloads[0]["from_column"])
	assert.Equal(t, string(domain.TaskColumnBlocked), payloads[0]["to_column"])
	assert.Equal(t, domain.EventActorSystem, payloads[0][domain.EventPayloadActor])
	assert.Equal(t, domain.MoveReasonResourceBlocked, payloads[0][domain.EventPayloadReason],
		"the park is the blocked half; MoveReasonResourceFree is what the sweeper writes on the way back out")
	assert.Equal(t, domain.ResourceMobileDevice, payloads[0]["resource"])
	require.Len(t, spans.recorded(), 1)
}

// A failed park is not a park, so it must leave no history claiming otherwise —
// and it must still not fail the run (the row keeps the resume state).
func TestAFailedParkRecordsNoMove(t *testing.T) {
	events, spans := &parkEventStore{}, &parkSpanStore{}
	runs := &recordingRunStore{}
	ex := &fakeExecutor{
		supports: domain.LLMProviderClaudeCode,
		err:      &domain.QuotaBlock{ResumeAt: time.Now().Add(time.Hour), CLISessionID: "sess-x"},
	}
	r, job := executorRunner(t, claudeCodeAgent(), runs, ex)
	r.SetTaskBlocker(&blockRecorder{err: errors.New("board unavailable")})
	r.SetParkJournal(NewParkJournal(events, spans))

	require.NoError(t, r.execute(context.Background(), job))
	assert.Empty(t, events.all(), "the card never moved, so the timeline must not say it did")
	assert.Empty(t, spans.recorded())
	assert.Equal(t, "sess-x", runs.row().CLISessionID, "the durable half still lands")
}

// The resume is a NEW run row, so the CLI session has to be found on the task's
// history. The run immediately before this one wins; the current run (which has
// not written its own yet) is skipped.
func TestResumeSessionComesFromTheTasksPreviousRuns(t *testing.T) {
	agent := claudeCodeAgent()
	currentID := uuid.New()
	parkedAt := time.Now().Add(-time.Hour)
	runs := &recordingRunStore{prev: []domain.TaskAgentRun{
		{ID: currentID, AgentID: agent.ID},
		{ID: uuid.New(), AgentID: agent.ID, CLISessionID: "sess-newest", QuotaResumeAt: &parkedAt},
		{ID: uuid.New(), AgentID: agent.ID, CLISessionID: "sess-older", QuotaResumeAt: &parkedAt},
	}}
	ex := &fakeExecutor{
		supports: domain.LLMProviderClaudeCode,
		resp:     domain.AgentResponse{Message: domain.Message{Content: "continued"}},
	}
	r, job := executorRunner(t, agent, runs, ex)
	job.Run.ID = currentID

	require.NoError(t, r.execute(context.Background(), job))
	assert.Equal(t, "sess-newest", ex.request().ResumeSessionID)
}

func TestLatestCLISessionOnlyContinuesAParkedRun(t *testing.T) {
	current := uuid.New()
	agent := uuid.New()
	parked := time.Now().Add(-time.Hour)

	assert.Equal(t, "", latestCLISession(nil, current, agent), "a task with no history starts fresh")
	assert.Equal(t, "", latestCLISession([]domain.TaskAgentRun{
		{ID: current, AgentID: agent, CLISessionID: "own", QuotaResumeAt: &parked},
	}, current, agent), "resuming the session inside the run that owns it is nonsense")
	assert.Equal(t, "sess-parked", latestCLISession([]domain.TaskAgentRun{
		{ID: current, AgentID: agent},
		{ID: uuid.New(), AgentID: agent, CLISessionID: "sess-parked", QuotaResumeAt: &parked},
	}, current, agent))

	// The decisive case: the previous run FINISHED and the card came back (a
	// revision). Continuing that session would tell it to carry on with work it
	// already delivered instead of acting on the reviewer's comments.
	assert.Equal(t, "", latestCLISession([]domain.TaskAgentRun{
		{ID: current, AgentID: agent},
		{ID: uuid.New(), AgentID: agent, Status: domain.TaskAgentRunStatusCompleted},
		{ID: uuid.New(), AgentID: agent, CLISessionID: "sess-older-park", QuotaResumeAt: &parked},
	}, current, agent), "only the run immediately before this one may be continued")
}

// A column can dispatch one task to several agents at once. Handing them all the
// same CLI session id would start two `claude --resume <same id>` processes in
// one workspace, each editing files the other cannot see.
func TestLatestCLISessionIsNotSharedBetweenAgents(t *testing.T) {
	current := uuid.New()
	mine, theirs := uuid.New(), uuid.New()
	parked := time.Now().Add(-time.Hour)

	history := []domain.TaskAgentRun{
		{ID: current, AgentID: mine},
		{ID: uuid.New(), AgentID: theirs, CLISessionID: "sess-theirs", QuotaResumeAt: &parked},
		{ID: uuid.New(), AgentID: mine, CLISessionID: "sess-mine", QuotaResumeAt: &parked},
	}

	assert.Equal(t, "sess-theirs", latestCLISession(history, current, theirs),
		"the agent that parked continues its own session")
	assert.Equal(t, "", latestCLISession(history, current, mine),
		"another agent's park is not this agent's session to resume, even when an older one of its own exists")
}

// Parking costs the task nothing, so a park that keeps repeating has nothing to
// stop it: the sweeper resumes, the same thing parks it again, forever. Past the
// cap the run fails like any other, which counts against the task and puts a
// human in front of it.
func TestRepeatedQuotaParksEventuallyFailTheRun(t *testing.T) {
	agent := claudeCodeAgent()
	currentID := uuid.New()
	parkedAt := time.Now().Add(-time.Hour)

	history := []domain.TaskAgentRun{{ID: currentID, AgentID: agent.ID}}
	for range maxConsecutiveQuotaParks {
		history = append(history, domain.TaskAgentRun{
			ID: uuid.New(), AgentID: agent.ID, CLISessionID: "sess-loop", QuotaResumeAt: &parkedAt,
		})
	}
	runs := &recordingRunStore{prev: history}
	ex := &fakeExecutor{
		supports: domain.LLMProviderClaudeCode,
		err:      &domain.QuotaBlock{ResumeAt: time.Now().Add(time.Hour), CLISessionID: "sess-loop"},
	}
	r, job := executorRunner(t, agent, runs, ex)
	job.Run.ID = currentID
	blocker := &blockRecorder{}
	r.SetTaskBlocker(blocker)

	err := r.execute(context.Background(), job)
	require.Error(t, err, "past the cap a park is an ordinary failure")

	row := runs.row()
	assert.Equal(t, domain.TaskAgentRunStatusFailed, row.Status)
	assert.Contains(t, row.Summary, "times in a row")
	resource, _ := blocker.parked()
	assert.Empty(t, resource, "the card must not be parked again once the cap has tripped")
}

// One park short of the cap still parks: the brake must not fire on a task that
// is genuinely waiting out a long outage.
func TestQuotaParksBelowTheCapStillPark(t *testing.T) {
	agent := claudeCodeAgent()
	currentID := uuid.New()
	parkedAt := time.Now().Add(-time.Hour)

	history := []domain.TaskAgentRun{{ID: currentID, AgentID: agent.ID}}
	for range maxConsecutiveQuotaParks - 1 {
		history = append(history, domain.TaskAgentRun{
			ID: uuid.New(), AgentID: agent.ID, CLISessionID: "sess-loop", QuotaResumeAt: &parkedAt,
		})
	}
	runs := &recordingRunStore{prev: history}
	ex := &fakeExecutor{
		supports: domain.LLMProviderClaudeCode,
		err:      &domain.QuotaBlock{ResumeAt: time.Now().Add(time.Hour), CLISessionID: "sess-loop"},
	}
	r, job := executorRunner(t, agent, runs, ex)
	job.Run.ID = currentID
	blocker := &blockRecorder{}
	r.SetTaskBlocker(blocker)

	require.NoError(t, r.execute(context.Background(), job))
	resource, _ := blocker.parked()
	assert.Equal(t, domain.ResourceClaudeCodeQuota, resource)
	assert.NotEqual(t, domain.TaskAgentRunStatusFailed, runs.row().Status)
}

// A run that COMPLETED between two parks breaks the streak: the task recovered,
// so its earlier parks say nothing about the one happening now.
func TestQuotaParkStreakStopsAtTheFirstNonPark(t *testing.T) {
	current := uuid.New()
	parked := time.Now().Add(-time.Hour)

	assert.Equal(t, 0, quotaParkStreak(nil, current))
	assert.Equal(t, 2, quotaParkStreak([]domain.TaskAgentRun{
		{ID: current},
		{ID: uuid.New(), QuotaResumeAt: &parked},
		{ID: uuid.New(), QuotaResumeAt: &parked},
		{ID: uuid.New(), Status: domain.TaskAgentRunStatusCompleted},
		{ID: uuid.New(), QuotaResumeAt: &parked},
	}, current), "only the unbroken run of parks counts")
}

// A failure to park the card still has to leave the durable half behind: the
// row carries the reset time, so the state is not lost even when the board
// write fails.
func TestQuotaParkKeepsTheRunRowEvenIfTheCardCannotBeParked(t *testing.T) {
	runs := &recordingRunStore{}
	ex := &fakeExecutor{
		supports: domain.LLMProviderClaudeCode,
		err:      &domain.QuotaBlock{ResumeAt: time.Now().Add(time.Hour), CLISessionID: "sess-x"},
	}
	r, job := executorRunner(t, claudeCodeAgent(), runs, ex)
	r.SetTaskBlocker(&blockRecorder{err: errors.New("board unavailable")})

	require.NoError(t, r.execute(context.Background(), job))
	assert.Equal(t, "sess-x", runs.row().CLISessionID)
	assert.True(t, strings.Contains(runs.row().Summary, "usage limit"))
}

func (c *agentCatalog) ListTechStacksByAgent(context.Context, uuid.UUID) ([]domain.TechStack, error) {
	return nil, nil
}
