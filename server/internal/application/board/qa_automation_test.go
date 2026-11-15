package board

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type automationUpdater struct {
	fakeTaskUpdater
	fresh    domain.BoardTask
	cases    []domain.TaskTestCase
	criteria []domain.AcceptanceCriterion
}

func (u *automationUpdater) GetTask(context.Context, uuid.UUID, uuid.UUID) (domain.BoardTask, error) {
	return u.fresh, nil
}

func (u *automationUpdater) ListTaskCriteria(context.Context, uuid.UUID) ([]domain.AcceptanceCriterion, error) {
	return u.criteria, nil
}

func (u *automationUpdater) ListTestCases(context.Context, uuid.UUID) ([]domain.TaskTestCase, error) {
	return u.cases, nil
}

type automationGit struct {
	port.GitClient
	kept []string
}

func (g *automationGit) ScopeWorkingTree(context.Context, string, string) ([]string, []string, error) {
	return g.kept, nil, nil
}

func (g *automationGit) TaskChangedFiles(context.Context, string) ([]string, error) { return nil, nil }

// fixLoop stands in for QA's fix round: it rewrites the entrypoint so the
// next suite run passes, or does nothing.
type fixLoop struct {
	agent.Runner
	calls  int
	asked  []string
	repair func()
}

func (l *fixLoop) RunTask(_ context.Context, msgs []domain.Message, _ string, _ domain.LLMProviderType, _ domain.ToolPolicy, _ ...agent.RunOption) (domain.AgentResponse, error) {
	l.calls++
	l.asked = append(l.asked, msgs[len(msgs)-1].Content)
	if l.repair != nil {
		l.repair()
	}
	return domain.AgentResponse{Message: domain.Message{Content: "fixed"}}, nil
}

func passedQARound() *automationUpdater {
	return &automationUpdater{
		fresh: domain.BoardTask{Column: domain.TaskColumnInQA},
		cases: []domain.TaskTestCase{
			{ID: uuid.New(), Title: "export returns rows", Status: domain.TestCaseStatusPassed},
			{ID: uuid.New(), Title: "upload of 0 rows", Status: domain.TestCaseStatusInvalid, Notes: "unreachable"},
		},
		criteria: []domain.AcceptanceCriterion{{ID: uuid.New(), Text: "export works", Checks: []domain.CriterionCheck{{Role: domain.CriterionReviewRoleQA, Approved: true}}}},
	}
}

func writeEntrypoint(t *testing.T, dir, body string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(automationEntrypoint))
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(body), 0o755))
}

func automationJob() RunJob {
	return RunJob{
		Task:         domain.BoardTask{ID: uuid.New(), Key: "T-7", Title: "export", TaskType: "task", Column: domain.TaskColumnInQA},
		RepositoryID: uuid.New(),
		Run:          domain.TaskAgentRun{AgentID: uuid.New()},
	}
}

func TestQARoundPassedNeedsEveryCaseRunAndEveryCriterionApproved(t *testing.T) {
	wf, err := workflowtest.Default().Reader().Workflow(context.Background(), "task")
	require.NoError(t, err)
	job := automationJob()

	u := passedQARound()
	r := &Runner{taskUpdater: u}
	assert.True(t, r.qaRoundPassed(context.Background(), job, wf))

	u.cases = append(u.cases, domain.TaskTestCase{Title: "expired token", Status: domain.TestCaseStatusPlanned})
	assert.False(t, r.qaRoundPassed(context.Background(), job, wf), "a planned case is not a passed round")

	u = passedQARound()
	u.cases[0].Status = domain.TestCaseStatusFailed
	assert.False(t, (&Runner{taskUpdater: u}).qaRoundPassed(context.Background(), job, wf), "a failed case is not a passed round")

	u = passedQARound()
	u.criteria[0].Checks = nil
	assert.False(t, (&Runner{taskUpdater: u}).qaRoundPassed(context.Background(), job, wf), "an unruled criterion is not a passed round")
}

func TestAutomationRoundMovesOnOnlyWhenTheSuiteIsGreen(t *testing.T) {
	wf, err := workflowtest.Default().Reader().Workflow(context.Background(), "task")
	require.NoError(t, err)

	t.Run("green suite with new tests passes the task on", func(t *testing.T) {
		ws := t.TempDir()
		writeEntrypoint(t, ws, "#!/usr/bin/env bash\nexit 0\n")
		loop := &fixLoop{}
		r := &Runner{verifyEnabled: true, taskUpdater: passedQARound(), git: &automationGit{kept: []string{"qa-automation/api/export_test.go"}}, agentLoop: loop}

		out, quota := r.runAutomationRound(context.Background(), automationJob(), domain.Agent{}, nil, domain.AgentResponse{}, "m", domain.ToolPolicy{}, wf, ws)

		require.Nil(t, quota)
		assert.Equal(t, domain.TaskColumnHumanUAT, out.Target)
		assert.Zero(t, loop.calls)
	})

	t.Run("no test added is handed back to QA", func(t *testing.T) {
		ws := t.TempDir()
		writeEntrypoint(t, ws, "#!/usr/bin/env bash\nexit 0\n")
		g := &automationGit{}
		loop := &fixLoop{repair: func() { g.kept = []string{"qa-automation/api/export_test.go"} }}
		r := &Runner{verifyEnabled: true, taskUpdater: passedQARound(), git: g, agentLoop: loop}

		out, _ := r.runAutomationRound(context.Background(), automationJob(), domain.Agent{}, nil, domain.AgentResponse{}, "m", domain.ToolPolicy{}, wf, ws)

		assert.Equal(t, domain.TaskColumnHumanUAT, out.Target)
		require.Equal(t, 1, loop.calls)
		assert.Contains(t, loop.asked[0], "No automation test was added")
	})

	t.Run("a suite still red after the fix rounds sends the task back", func(t *testing.T) {
		ws := t.TempDir()
		writeEntrypoint(t, ws, "#!/usr/bin/env bash\necho 'FAIL api/export_returns_rows'\nexit 1\n")
		loop := &fixLoop{}
		r := &Runner{verifyEnabled: true, taskUpdater: passedQARound(), git: &automationGit{kept: []string{"qa-automation/api/export_test.go"}}, agentLoop: loop}

		out, _ := r.runAutomationRound(context.Background(), automationJob(), domain.Agent{}, nil, domain.AgentResponse{}, "m", domain.ToolPolicy{}, wf, ws)

		assert.Equal(t, domain.TaskColumnNeedRevision, out.Target)
		assert.Equal(t, automationFixRounds, loop.calls)
		assert.True(t, strings.Contains(out.Comment, "FAIL api/export_returns_rows"), out.Comment)
	})

	t.Run("a round that did not pass is left to the sweeps", func(t *testing.T) {
		u := passedQARound()
		u.cases[0].Status = domain.TestCaseStatusFailed
		r := &Runner{taskUpdater: u, git: &automationGit{}, agentLoop: &fixLoop{}}

		out, _ := r.runAutomationRound(context.Background(), automationJob(), domain.Agent{}, nil, domain.AgentResponse{}, "m", domain.ToolPolicy{}, wf, t.TempDir())

		assert.Empty(t, out.Target)
	})
}

func TestKeepWorkspaceWritersRestoresWhatTheQAStageStripped(t *testing.T) {
	full := domain.ToolPolicy{AllowTools: []string{"run_terminal", "write_file", "edit_file", "browser_navigate"}}
	stripped := domain.ToolPolicy{AllowTools: []string{"run_terminal", "browser_navigate"}}

	got := domain.KeepWorkspaceWriters(stripped, full)

	assert.ElementsMatch(t, []string{"run_terminal", "browser_navigate", "write_file", "edit_file"}, got.AllowTools)
	assert.Len(t, stripped.AllowTools, 2, "the restricted policy itself is not mutated")
}
