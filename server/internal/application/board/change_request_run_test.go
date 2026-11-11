package board

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type midRunUpdater struct {
	fakeTaskUpdater
	mu       sync.Mutex
	stored   []domain.TaskComment
	criteria []domain.AcceptanceCriterion
}

func (u *midRunUpdater) ListComments(context.Context, uuid.UUID, uuid.UUID) ([]domain.TaskComment, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]domain.TaskComment(nil), u.stored...), nil
}

func (u *midRunUpdater) ListTaskCriteria(context.Context, uuid.UUID) ([]domain.AcceptanceCriterion, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]domain.AcceptanceCriterion(nil), u.criteria...), nil
}

func (u *midRunUpdater) requestChange(text string, criterion string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.stored = append(u.stored, domain.TaskComment{
		ID: uuid.New(), AuthorType: "agent", Content: domain.ChangeRequestComment(text), CreatedAt: time.Now(),
	})
	if criterion != "" {
		u.criteria = append(u.criteria, domain.AcceptanceCriterion{ID: uuid.New(), Text: criterion})
	}
}

func (u *midRunUpdater) settleAll() {
	u.mu.Lock()
	defer u.mu.Unlock()
	for i := range u.criteria {
		u.criteria[i].Completed = true
	}
}

// changeAfterFirstTurn plays the stakeholder: the change lands while the agent's
// first turn is running, and the agent settles every criterion it is later asked about.
type changeAfterFirstTurn struct {
	fakeExecutor
	updater *midRunUpdater
}

func (e *changeAfterFirstTurn) Execute(ctx context.Context, req domain.TaskExecution) (domain.AgentResponse, error) {
	resp, err := e.fakeExecutor.Execute(ctx, req)
	if e.callCount() == 1 {
		e.updater.requestChange("export must include archived rows", "the export includes archived rows")
	} else if e.callCount() >= 3 {
		e.updater.settleAll()
	}
	return resp, err
}

func routedRunner(t *testing.T, ex port.TaskExecutor, updater *midRunUpdater) (*Runner, RunJob) {
	t.Helper()
	agentRec := claudeCodeAgent()
	router, _ := hostRouter(ex)
	r := NewRunner(RunnerDeps{
		Runs:         &recordingRunStore{},
		Catalog:      &agentCatalog{agent: agentRec},
		Repositories: oneRepoResolver{root: t.TempDir()},
		AgentLoop:    router,
	})
	r.SetWorkflows(workflowtest.Default().Reader())
	r.SetTaskExecutor(ex)
	r.SetTaskUpdater(updater)
	taskID := uuid.New()
	return r, RunJob{
		Run:  domain.TaskAgentRun{ID: uuid.New(), TaskID: taskID, AgentID: agentRec.ID},
		Task: domain.BoardTask{ID: taskID, RepositoryID: uuid.New(), Key: "tt-42", Title: "Export", Column: domain.TaskColumnInProgress},
	}
}

func TestARunPicksUpAChangeRequestedWhileItWasWorking(t *testing.T) {
	updater := &midRunUpdater{}
	ex := &changeAfterFirstTurn{
		fakeExecutor: fakeExecutor{supports: domain.LLMProviderClaudeCode, resp: domain.AgentResponse{Message: domain.Message{Content: "implemented"}}},
		updater:      updater,
	}
	r, job := routedRunner(t, ex, updater)

	require.NoError(t, r.execute(context.Background(), context.Background(), func() {}, job))

	reqs := ex.requests()
	require.GreaterOrEqual(t, len(reqs), 3, "main turn, change-request follow-up, criteria sweep")
	followUp := reqs[1].History[len(reqs[1].History)-1].Content
	require.Contains(t, followUp, "export must include archived rows")
	require.NotContains(t, followUp, domain.ChangeRequestCommentPrefix, "the prefix is a marker, not part of the request")
	sweep := reqs[2].History[len(reqs[2].History)-1].Content
	require.Contains(t, sweep, "the export includes archived rows", "a criterion added mid-run is in the run's criteria sweep")
}

func TestARunWithNoNewChangeRequestRunsNoFollowUp(t *testing.T) {
	updater := &midRunUpdater{}
	ex := &fakeExecutor{supports: domain.LLMProviderClaudeCode, resp: domain.AgentResponse{Message: domain.Message{Content: "implemented"}}}
	r, job := routedRunner(t, ex, updater)

	require.NoError(t, r.execute(context.Background(), context.Background(), func() {}, job))

	require.Equal(t, 1, ex.callCount())
}

func TestARequestAlreadyInTheOpeningContextIsNotSentTwice(t *testing.T) {
	updater := &midRunUpdater{}
	updater.requestChange("the button says Export all", "")
	ex := &fakeExecutor{supports: domain.LLMProviderClaudeCode, resp: domain.AgentResponse{Message: domain.Message{Content: "implemented"}}}
	r, job := routedRunner(t, ex, updater)

	require.NoError(t, r.execute(context.Background(), context.Background(), func() {}, job))

	require.Equal(t, 1, ex.callCount(), "the request was read at start, so no follow-up")
	var opening strings.Builder
	for _, m := range ex.request().History {
		opening.WriteString(m.Content)
	}
	require.Contains(t, opening.String(), "the button says Export all")
	require.Contains(t, opening.String(), "Changes the stakeholder asked for")
}

func TestChangeRequestsAreNotHumanRequirementsOrRevisionFeedback(t *testing.T) {
	c := domain.TaskComment{AuthorType: "user", Content: domain.ChangeRequestComment("x")}
	require.Empty(t, humanRequirementsMessage([]domain.TaskComment{c}))
	c.AuthorType = "agent"
	require.Empty(t, revisionCommentsMessage([]domain.TaskComment{c}))
	require.Contains(t, changeRequestsMessage([]domain.TaskComment{c}), "x")
}

func TestOnlyTheNewestChangeRequestsRideInTheOpeningContext(t *testing.T) {
	var comments []domain.TaskComment
	for i := 0; i < changeRequestsLimit+2; i++ {
		comments = append(comments, domain.TaskComment{ID: uuid.New(), Content: domain.ChangeRequestComment(strings.Repeat("n", i+1))})
	}
	msg := changeRequestsMessage(comments)
	require.Equal(t, changeRequestsLimit, strings.Count(msg, "\n- "))
}
