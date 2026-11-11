package board

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"strings"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type reviewGit struct {
	port.GitClient
	origin    string
	prErrs    []error
	prURL     string
	pushErr   error
	pushCalls int
	prCalls   int
}

func (g *reviewGit) OriginURL(context.Context, string) string { return g.origin }

func (g *reviewGit) PushBranch(context.Context, string) error {
	g.pushCalls++
	return g.pushErr
}

func (g *reviewGit) EnsurePullRequest(context.Context, string) (string, error) {
	g.prCalls++
	if len(g.prErrs) > 0 {
		err := g.prErrs[0]
		g.prErrs = g.prErrs[1:]
		if err != nil {
			return "", err
		}
	}
	return g.prURL, nil
}

func TestReviewPRContext_UsesTheExistingPR(t *testing.T) {
	g := &reviewGit{origin: "https://github.com/acme/acme-web.git", prURL: "https://github.com/acme/acme-web/pull/7"}

	msg, err := (&Runner{git: g}).reviewPRContext(context.Background(), "/ws/task-1", uuid.New())

	require.NoError(t, err)
	assert.Contains(t, msg, "https://github.com/acme/acme-web/pull/7")
	assert.Zero(t, g.pushCalls, "a branch that already has a PR must not be pushed again")
}

func TestReviewPRContext_PushesTheBranchAndOpensThePR(t *testing.T) {
	g := &reviewGit{
		origin:  "https://github.com/acme/acme-web.git",
		prErrs:  []error{errors.New("no commits between main and feature/task-abc")},
		prURL:   "https://github.com/acme/acme-web/pull/9",
		pushErr: nil,
	}

	msg, err := (&Runner{git: g}).reviewPRContext(context.Background(), "/ws/task-1", uuid.New())

	require.NoError(t, err)
	assert.Equal(t, 1, g.pushCalls)
	assert.Equal(t, 2, g.prCalls)
	assert.Contains(t, msg, "pull/9")
}

func TestReviewPRContext_NoPRIsAnErrorWhenTheRepoHasAnOrigin(t *testing.T) {
	g := &reviewGit{
		origin: "https://github.com/acme/acme-web.git",
		prErrs: []error{errors.New("gh pr create failed"), errors.New("gh pr create failed")},
	}

	_, err := (&Runner{git: g}).reviewPRContext(context.Background(), "/ws/task-1", uuid.New())

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrReviewPRMissing)
}

func TestReviewPRContext_NoOriginReviewsTheDiffAlone(t *testing.T) {
	g := &reviewGit{prErrs: []error{errors.New("no origin"), errors.New("no origin")}}

	msg, err := (&Runner{git: g}).reviewPRContext(context.Background(), "/ws/task-1", uuid.New())

	require.NoError(t, err)
	assert.Empty(t, msg)
}

// Reviewing is now read off the stage's kind rather than a flag, so every
// review/approval stage counts — in_qa and human_uat included, which is what
// they are: someone judging work they did not do.
func TestIsReviewColumn(t *testing.T) {
	for _, col := range []domain.TaskColumn{
		domain.TaskColumnCodeReview, domain.TaskColumnAnalizReview,
		domain.TaskColumnInQA, domain.TaskColumnHumanUAT,
	} {
		assert.True(t, isReviewColumn(taskWF, col), "%s judges someone else's change", col)
	}
	for _, col := range []domain.TaskColumn{
		domain.TaskColumnTodo, domain.TaskColumnInProgress, domain.TaskColumnNeedRevision,
		domain.TaskColumnReadyForQA, domain.TaskColumnBacklog, domain.TaskColumnDone,
	} {
		assert.False(t, isReviewColumn(taskWF, col), "%s is not a review column", col)
	}
}

func TestReviewDiffMessage_ReviewerGetsTheWholeDiff(t *testing.T) {
	diff := strings.Repeat("+ line of code\n", 1200)

	reviewer := reviewDiffMessage(taskWF, domain.TaskColumnCodeReview, diff)
	implementer := reviewDiffMessage(taskWF, domain.TaskColumnInProgress, diff)

	assert.NotContains(t, reviewer, "(truncated)", "an 18 KB diff must reach the reviewer whole")
	assert.Contains(t, reviewer, "reviewing")
	assert.Contains(t, implementer, "(truncated)")
	assert.Less(t, len(implementer), len(reviewer))
}

func TestReviewDiffMessage_TruncatesBeyondTheReviewLimit(t *testing.T) {
	diff := strings.Repeat("+ line of code\n", 4000)

	got := reviewDiffMessage(taskWF, domain.TaskColumnCodeReview, diff)

	assert.Contains(t, got, "(truncated)")
	assert.Less(t, len(got), reviewDiffLimit+500)
}

func TestColumnInstructionCodeReviewReadsTheDiffInsteadOfRunningIt(t *testing.T) {
	got := columnInstruction(taskWF, domain.BoardTask{Column: domain.TaskColumnCodeReview})

	for _, want := range []string{"pull request", "get_pipeline_status", "ready_for_qa", "need_revision"} {
		assert.Contains(t, got, want)
	}
}

func TestReviewPRContext_NonGitHubRemoteReviewsTheDiffWithoutRetrying(t *testing.T) {
	g := &reviewGit{
		origin: "https://gitlab.com/acme/acme-web.git",
		prErrs: []error{domain.ErrNotGitHubRepo, domain.ErrNotGitHubRepo},
	}

	msg, err := (&Runner{git: g}).reviewPRContext(context.Background(), "/ws/task-1", uuid.New())

	require.NoError(t, err)
	assert.Empty(t, msg)
	assert.Equal(t, 1, g.prCalls, "a non-GitHub origin is not retried")
	assert.Zero(t, g.pushCalls)
}
