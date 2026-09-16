package board

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func hostRouter(ex *fakeExecutor) (*agent.Router, *recordingLLM) {
	llm := &recordingLLM{}
	router := agent.NewRouter(agent.NewLoop(llm, toollessRegistry{}, 3, 3, 16000))
	router.SetTaskExecutor(ex)
	return router, llm
}

func redWorkspace(t *testing.T) (string, domain.Repository) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "verify.sh"), []byte("echo boom >&2\nexit 1\n"), 0o700))
	return dir, domain.Repository{VerifyCommand: "sh verify.sh"}
}

func TestVerifyFixRoundRunsOnTheHostExecutor(t *testing.T) {
	dir, repo := redWorkspace(t)
	ex := &fakeExecutor{
		supports: domain.LLMProviderClaudeCode,
		resp:     domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: "fixed the build"}},
	}
	router, llm := hostRouter(ex)
	r := NewRunner(RunnerDeps{
		AgentLoop:         router,
		Repositories:      failingRepo{repo: repo},
		VerifyFixAttempts: 1,
	})

	job := RunJob{Task: domain.BoardTask{ID: uuid.New(), Key: "tt-7", Title: "wire the gate"}, RepositoryID: uuid.New()}

	ctx := registry.ContextWithWorkspaceDir(context.Background(), dir)

	r.verifyAndFix(ctx, job, claudeCodeAgent(),
		[]domain.Message{{Role: domain.RoleUser, Content: "implement the gate"}},
		domain.AgentResponse{Message: domain.Message{Content: "done"}},
		"opus", domain.ToolPolicy{}, dir)

	require.Equal(t, 1, ex.callCount(), "one red gate must buy exactly one fix round on the CLI")
	req := ex.request()
	require.Equal(t, dir, req.WorkDir, "the fix round must run in the task's own checkout")
	require.Equal(t, domain.LLMProviderClaudeCode, req.Provider)
	require.Equal(t, "opus", req.Model)
	require.Contains(t, req.TaskKey, "tt-7", "the CLI session must be attributable to the task")
	require.Contains(t, messageContents(req.History), "Automated verification failed")
	require.Empty(t, llm.requests, "nothing about this run may reach an HTTP provider")
}

type criteriaUpdater struct {
	fakeTaskUpdater
	criteria []domain.AcceptanceCriterion
}

func (c *criteriaUpdater) ListTaskCriteria(context.Context, uuid.UUID) ([]domain.AcceptanceCriterion, error) {
	return c.criteria, nil
}

func TestCriteriaSweepRunsOnTheHostExecutor(t *testing.T) {
	ex := &fakeExecutor{
		supports: domain.LLMProviderClaudeCode,
		resp:     domain.AgentResponse{Message: domain.Message{Content: "ticked 2 of 2"}},
	}
	router, llm := hostRouter(ex)
	r := NewRunner(RunnerDeps{AgentLoop: router})
	r.SetTaskUpdater(&criteriaUpdater{criteria: []domain.AcceptanceCriterion{
		{ID: uuid.New(), Text: "the gate refuses a red build", Completed: false},
	}})

	dir := t.TempDir()
	ctx := registry.ContextWithWorkspaceDir(context.Background(), dir)
	job := RunJob{
		Task:         domain.BoardTask{ID: uuid.New(), Key: "tt-8", Column: domain.TaskColumnInProgress},
		RepositoryID: uuid.New(),
	}
	resp := domain.AgentResponse{Message: domain.Message{Content: "implemented"}}

	out, _ := r.sweepOpenCriteria(ctx, job, claudeCodeAgent(),
		[]domain.Message{{Role: domain.RoleUser, Content: "do the work"}}, resp, "opus", domain.ToolPolicy{})

	// criteriaUpdater never settles the criterion, so the completion loop asks
	// its full round budget — every round on the SAME engine, which is what
	// this test is about (see criteria_sweep_loop_test.go for the loop itself).
	require.Equal(t, criteriaSweepRounds, ex.callCount(), "the open criterion must be put to the agent on its own engine")
	require.Equal(t, dir, ex.request().WorkDir)
	require.Contains(t, messageContents(ex.request().History), "settle its acceptance criteria")
	require.Empty(t, llm.requests)
	// The sweep's own bookkeeping talk never replaces the run's summary.
	require.Equal(t, "implemented", out.Message.Content)
}

func TestReviewVerdictFinalizeRunsOnTheHostExecutor(t *testing.T) {
	ex := &fakeExecutor{
		supports: domain.LLMProviderClaudeCode,
		resp:     domain.AgentResponse{Message: domain.Message{Content: "APPROVE"}},
	}
	router, llm := hostRouter(ex)
	updater := &fakeTaskUpdater{}
	r := NewRunner(RunnerDeps{AgentLoop: router})
	r.SetTaskUpdater(updater)

	dir := t.TempDir()
	ctx := registry.ContextWithWorkspaceDir(context.Background(), dir)
	agentID := uuid.New()
	job := RunJob{
		Task:         domain.BoardTask{ID: uuid.New(), Key: "tt-9", Column: domain.TaskColumnCodeReview},
		Run:          domain.TaskAgentRun{ID: uuid.New(), AgentID: agentID},
		RepositoryID: uuid.New(),
	}

	moved := r.finalizeReviewVerdict(ctx, job, claudeCodeAgent(),
		[]domain.Message{{Role: domain.RoleUser, Content: "review the diff"}},
		"opus", domain.ToolPolicy{}, domain.TaskColumnReadyForQA)

	require.True(t, moved, "the verdict the reviewer stated must become the move it never made")
	require.Equal(t, 1, ex.callCount())
	require.Equal(t, dir, ex.request().WorkDir)
	require.Empty(t, llm.requests)
	require.Len(t, updater.calls, 1)
	require.NotNil(t, updater.calls[0].Column)
	require.Equal(t, domain.TaskColumnReadyForQA, *updater.calls[0].Column)
	// Attributed to the reviewing agent, like every other hand-off move.
	require.Equal(t, domain.TaskActorAgent, updater.calls[0].Actor)
}

func TestHostExecutedSweepFailsHonestlyWithNoRunner(t *testing.T) {
	llm := &recordingLLM{}
	router := agent.NewRouter(agent.NewLoop(llm, toollessRegistry{}, 3, 3, 16000))
	r := NewRunner(RunnerDeps{AgentLoop: router})
	r.SetTaskUpdater(&criteriaUpdater{criteria: []domain.AcceptanceCriterion{
		{ID: uuid.New(), Text: "still open"},
	}})

	job := RunJob{Task: domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInProgress}, RepositoryID: uuid.New()}
	resp := domain.AgentResponse{Message: domain.Message{Content: "implemented"}}

	out, _ := r.sweepOpenCriteria(registry.ContextWithWorkspaceDir(context.Background(), t.TempDir()),
		job, claudeCodeAgent(), nil, resp, "opus", domain.ToolPolicy{})

	require.Empty(t, llm.requests, "a claude_code run must never become an HTTP request")
	require.Equal(t, "implemented", out.Message.Content, "the sweep failing leaves the run's own answer alone")
}

func TestHTTPProviderStillRunsOnTheLoop(t *testing.T) {
	ex := &fakeExecutor{supports: domain.LLMProviderClaudeCode}
	router, llm := hostRouter(ex)
	r := NewRunner(RunnerDeps{AgentLoop: router})
	r.SetTaskUpdater(&criteriaUpdater{criteria: []domain.AcceptanceCriterion{
		{ID: uuid.New(), Text: "still open"},
	}})

	job := RunJob{Task: domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInProgress}, RepositoryID: uuid.New()}
	r.sweepOpenCriteria(context.Background(), job,
		domain.Agent{Name: "gpt", ProviderType: domain.LLMProviderOpenAI, Model: "gpt-4o"},
		nil, domain.AgentResponse{Message: domain.Message{Content: "implemented"}}, "gpt-4o", domain.ToolPolicy{})

	require.Zero(t, ex.callCount(), "an HTTP agent's run must not be handed to the CLI")
	// One request per completion-check round: criteriaUpdater never settles the
	// criterion, so the loop spends its whole budget — on the HTTP loop, which
	// is what this test is about.
	require.Len(t, llm.requests, criteriaSweepRounds)
}
