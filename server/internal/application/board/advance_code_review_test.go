package board

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type handoffGit struct {
	port.GitClient
	diff     string
	err      error
	files    []string
	filesErr error
}

func (g *handoffGit) TaskDiff(context.Context, string) (string, error) {
	return g.diff, g.err
}

func (g *handoffGit) TaskChangedFiles(context.Context, string) ([]string, error) {
	return g.files, g.filesErr
}

type uiKindRepos struct{ kind string }

func (u uiKindRepos) ResolveRootPath(context.Context, uuid.UUID) (string, error)    { return "", nil }
func (u uiKindRepos) ResolveDescription(context.Context, uuid.UUID) (string, error) { return "", nil }
func (u uiKindRepos) ResolveRepository(context.Context, uuid.UUID) (domain.Repository, error) {
	return domain.Repository{Kind: u.kind}, nil
}
func (u uiKindRepos) ProfileForRun(context.Context, uuid.UUID, string) string { return "" }

type readableUpdater struct {
	fakeTaskUpdater
	fresh domain.BoardTask
}

func (r *readableUpdater) GetTask(context.Context, uuid.UUID, uuid.UUID) (domain.BoardTask, error) {
	return r.fresh, nil
}

func handoffRunner(updater TaskUpdater, git port.GitClient) *Runner {
	return &Runner{taskUpdater: updater, git: git}
}

func handoffRunnerWithProjects(updater TaskUpdater, git port.GitClient, projects RepositoryResolver) *Runner {
	r := handoffRunner(updater, git)
	r.projects = projects
	return r
}

func verifiedUsage() *registry.ToolUsage {
	u := registry.NewToolUsage()
	u.Record("run_terminal")
	return u
}

func TestAdvanceToCodeReviewHoldsAnUnverifiedRun(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInProgress, AssigneeAgentID: &agentID}
	updater := &fakeTaskUpdater{task: task}
	r := handoffRunner(updater, &handoffGit{diff: "diff --git a/app.tsx b/app.tsx"})

	usage := registry.NewToolUsage()
	usage.Record("edit_file")
	usage.Record("read_file")

	r.advanceToCodeReview(context.Background(), runJobFor(task, agentID), "/w/task-1", usage)

	assert.Empty(t, updater.calls, "an unexecuted diff must stay in the working column")
	require.Len(t, updater.comments, 1, "the next run has to be told why the card did not move")
	assert.Contains(t, updater.comments[0].Content, "run_terminal")
}

func TestAdvanceToCodeReviewMovesWhenUsageIsUnmeasured(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInProgress, AssigneeAgentID: &agentID}
	updater := &fakeTaskUpdater{task: task}
	r := handoffRunner(updater, &handoffGit{diff: "diff --git a/app.tsx b/app.tsx"})

	r.advanceToCodeReview(context.Background(), runJobFor(task, agentID), "/w/task-1", nil)

	require.Len(t, updater.calls, 1)
	require.NotNil(t, updater.calls[0].Column)
	assert.Equal(t, domain.TaskColumnCodeReview, *updater.calls[0].Column)
}

func TestAdvanceToCodeReviewMovesAFinishedImplementation(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInProgress, AssigneeAgentID: &agentID}
	updater := &fakeTaskUpdater{task: task}
	r := handoffRunner(updater, &handoffGit{diff: "diff --git a/app.tsx b/app.tsx"})

	r.advanceToCodeReview(context.Background(), runJobFor(task, agentID), "/w/task-1", verifiedUsage())

	require.Len(t, updater.calls, 1)
	require.NotNil(t, updater.calls[0].Column)
	assert.Equal(t, domain.TaskColumnCodeReview, *updater.calls[0].Column)
	assert.Equal(t, domain.TaskActorAgent, updater.calls[0].Actor)
	require.NotNil(t, updater.calls[0].ActorAgentID)
	assert.Equal(t, agentID, *updater.calls[0].ActorAgentID, "the dispatcher skips the agent that made the move; the system actor would re-dispatch this run")
}

func TestAdvanceToCodeReviewHandsBackARevision(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnNeedRevision}
	updater := &fakeTaskUpdater{task: task}
	r := handoffRunner(updater, &handoffGit{diff: "diff"})

	r.advanceToCodeReview(context.Background(), runJobFor(task, agentID), "/w/task-1", verifiedUsage())

	require.Len(t, updater.calls, 1)
	assert.Equal(t, domain.TaskColumnCodeReview, *updater.calls[0].Column)
}

func TestAdvanceToCodeReviewNeedsARealDiff(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInProgress}
	updater := &fakeTaskUpdater{task: task}
	r := handoffRunner(updater, &handoffGit{diff: "   \n"})

	r.advanceToCodeReview(context.Background(), runJobFor(task, agentID), "/w/task-1", verifiedUsage())

	assert.Empty(t, updater.calls)
}

func TestAdvanceToCodeReviewStaysPutWhenTheDiffCannotBeRead(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInProgress}
	updater := &fakeTaskUpdater{task: task}
	r := handoffRunner(updater, &handoffGit{err: errors.New("not a git repository")})

	r.advanceToCodeReview(context.Background(), runJobFor(task, agentID), "/w/task-1", verifiedUsage())

	assert.Empty(t, updater.calls)
}

func TestAdvanceToCodeReviewOnlyActsOnImplementationColumns(t *testing.T) {
	agentID := uuid.New()
	for _, column := range []domain.TaskColumn{
		domain.TaskColumnTodo, domain.TaskColumnCodeReview,
		domain.TaskColumnReadyForQA, domain.TaskColumnInQA, domain.TaskColumnPMUAT,
	} {
		task := domain.BoardTask{ID: uuid.New(), Column: column}
		updater := &fakeTaskUpdater{task: task}
		r := handoffRunner(updater, &handoffGit{diff: "diff"})

		r.advanceToCodeReview(context.Background(), runJobFor(task, agentID), "/w/task-1", verifiedUsage())

		assert.Empty(t, updater.calls, "column %s must be left alone", column)
	}
}

func TestAdvanceToCodeReviewSkipsAnalizTasks(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInProgress, TaskType: domain.TaskTypeAnaliz}
	updater := &fakeTaskUpdater{task: task}
	r := handoffRunner(updater, &handoffGit{diff: "diff"})

	r.advanceToCodeReview(context.Background(), runJobFor(task, agentID), "/w/task-1", verifiedUsage())

	assert.Empty(t, updater.calls)
}

func TestAdvanceToCodeReviewRespectsAColumnChangedDuringTheRun(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInProgress}
	updater := &readableUpdater{
		fakeTaskUpdater: fakeTaskUpdater{task: task},
		fresh:           domain.BoardTask{ID: task.ID, Column: domain.TaskColumnCodeReview},
	}
	r := handoffRunner(updater, &handoffGit{diff: "diff"})

	r.advanceToCodeReview(context.Background(), runJobFor(task, agentID), "/w/task-1", verifiedUsage())

	assert.Empty(t, updater.calls)
}

func TestAdvanceToCodeReviewCommentsWhenTheBoardRefuses(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInProgress}
	updater := &commentingUpdater{fakeTaskUpdater: fakeTaskUpdater{task: task, err: errors.New("2 acceptance criteria incomplete")}}
	r := handoffRunner(updater, &handoffGit{diff: "diff"})

	r.advanceToCodeReview(context.Background(), runJobFor(task, agentID), "/w/task-1", verifiedUsage())

	require.Len(t, updater.comments, 1)
	assert.Contains(t, updater.comments[0].Content, "acceptance criteria incomplete")
}

type commentingUpdater struct {
	fakeTaskUpdater
	comments []domain.CreateTaskCommentRequest
}

func (c *commentingUpdater) AddComment(_ context.Context, _, _ uuid.UUID, req domain.CreateTaskCommentRequest) (domain.TaskComment, error) {
	c.comments = append(c.comments, req)
	return domain.TaskComment{}, nil
}

func TestAdvanceToCodeReviewSkipsUIGateForDocsOnlyDiffOnFrontendRepo(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInProgress, AssigneeAgentID: &agentID}
	updater := &fakeTaskUpdater{task: task}
	git := &handoffGit{diff: "diff --git a/.ai/architecture.md b/.ai/architecture.md", files: []string{".ai/architecture.md", "scripts/dev.sh"}}
	r := handoffRunnerWithProjects(updater, git, uiKindRepos{kind: domain.RepoKindFrontend})

	r.advanceToCodeReview(context.Background(), runJobFor(task, agentID), "/w/task-1", verifiedUsage())

	require.Len(t, updater.calls, 1)
	assert.Equal(t, domain.TaskColumnCodeReview, *updater.calls[0].Column)
	assert.Empty(t, updater.comments)
}

func TestAdvanceToCodeReviewStillBlocksUIGateForRealUIDiffOnFrontendRepo(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInProgress, AssigneeAgentID: &agentID}
	updater := &fakeTaskUpdater{task: task}
	git := &handoffGit{diff: "diff --git a/src/components/Button.tsx b/src/components/Button.tsx", files: []string{"src/components/Button.tsx"}}
	r := handoffRunnerWithProjects(updater, git, uiKindRepos{kind: domain.RepoKindFrontend})

	r.advanceToCodeReview(context.Background(), runJobFor(task, agentID), "/w/task-1", verifiedUsage())

	assert.Empty(t, updater.calls)
	require.Len(t, updater.comments, 1)
	assert.Contains(t, updater.comments[0].Content, "ekrana hiç bakmadı")
}

func TestAdvanceToCodeReviewUIGateDefaultsToBlockingWhenChangedFilesUnreadable(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInProgress, AssigneeAgentID: &agentID}
	updater := &fakeTaskUpdater{task: task}
	git := &handoffGit{diff: "diff --git a/.ai/architecture.md b/.ai/architecture.md", filesErr: errors.New("not a git repository")}
	r := handoffRunnerWithProjects(updater, git, uiKindRepos{kind: domain.RepoKindFrontend})

	r.advanceToCodeReview(context.Background(), runJobFor(task, agentID), "/w/task-1", verifiedUsage())

	assert.Empty(t, updater.calls)
	require.Len(t, updater.comments, 1)
	assert.Contains(t, updater.comments[0].Content, "ekrana hiç bakmadı")
}

func TestAdvanceToCodeReviewUIGateNeverFiresOnNonUIRepo(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInProgress, AssigneeAgentID: &agentID}
	updater := &fakeTaskUpdater{task: task}
	git := &handoffGit{diff: "diff --git a/.ai/architecture.md b/.ai/architecture.md", files: []string{".ai/architecture.md"}}
	r := handoffRunnerWithProjects(updater, git, uiKindRepos{})

	r.advanceToCodeReview(context.Background(), runJobFor(task, agentID), "/w/task-1", verifiedUsage())

	require.Len(t, updater.calls, 1)
	assert.Equal(t, domain.TaskColumnCodeReview, *updater.calls[0].Column)
	assert.Empty(t, updater.comments)
}

func TestAdvanceToCodeReviewIsNilSafe(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInProgress}

	assert.NotPanics(t, func() {
		(&Runner{}).advanceToCodeReview(context.Background(), runJobFor(task, agentID), "/w/task-1", verifiedUsage())
	})
}
