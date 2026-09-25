package board

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type mergePRs struct {
	pr  port.PullRequest
	err error
}

func (f *mergePRs) GetPullRequest(context.Context, string, string, string, int) (port.PullRequest, error) {
	return f.pr, f.err
}

func (f *mergePRs) ListPullRequestFiles(context.Context, string, string, string, int) ([]port.PullRequestFile, error) {
	return nil, nil
}

func (f *mergePRs) PullRequestDiff(context.Context, string, string, string, int, int) (string, bool, error) {
	return "", false, nil
}

func (f *mergePRs) ListPullRequestReviewComments(context.Context, string, string, string, int) ([]port.PullRequestComment, error) {
	return nil, nil
}

func (f *mergePRs) ListIssueComments(context.Context, string, string, string, int) ([]port.PullRequestComment, error) {
	return nil, nil
}

func (f *mergePRs) CreateIssueComment(context.Context, string, string, string, int, string) (port.PullRequestComment, error) {
	return port.PullRequestComment{}, nil
}

func (f *mergePRs) ReplyToReviewComment(context.Context, string, string, string, int, int64, string) (port.PullRequestComment, error) {
	return port.PullRequestComment{}, nil
}

type mergeGates struct {
	chainErr         error
	pipeline         domain.TaskPipeline
	pipelineErr      error
	autoReleased     bool
	autoReleaseCalls int
}

func (f *mergeGates) CheckReviewChain(context.Context, uuid.UUID, uuid.UUID) error { return f.chainErr }

func (f *mergeGates) LatestTaskPipeline(context.Context, uuid.UUID, uuid.UUID) (domain.TaskPipeline, error) {
	if f.pipelineErr != nil {
		return domain.TaskPipeline{}, f.pipelineErr
	}
	return f.pipeline, nil
}

func (f *mergeGates) AutoReleaseIfUndeployable(context.Context, uuid.UUID, uuid.UUID) bool {
	f.autoReleaseCalls++
	return f.autoReleased
}

const (
	mergeHeadSHA  = "1111111111111111111111111111111111111111"
	mergeOtherSHA = "2222222222222222222222222222222222222222"
)

func mergeTask() domain.BoardTask {
	return domain.BoardTask{
		ID:          uuid.New(),
		Key:         "T-7",
		Title:       "Add the store link",
		TaskType:    "task",
		Column:      domain.TaskColumnDone,
		VerifiedSHA: mergeHeadSHA,
		PRURL:       "https://github.com/acme/widget/pull/42",
		PRNumber:    42,
	}
}

func openCleanPR() port.PullRequest {
	return port.PullRequest{
		Number:         42,
		State:          "open",
		MergeableState: "clean",
		HeadRef:        "feature/t-7",
		BaseRef:        "main",
		HeadSHA:        mergeHeadSHA,
	}
}

func newMergeFixture(task domain.BoardTask, pr port.PullRequest, gates *mergeGates) (*TaskPRService, *taskChatTaskStore, *taskPRGit, uuid.UUID) {
	repositoryID := uuid.New()
	tasks := &taskChatTaskStore{tasks: map[[2]uuid.UUID]domain.BoardTask{{repositoryID, task.ID}: task}}
	git := &taskPRGit{hasGit: true, branch: pr.HeadRef}
	svc := NewTaskPRService(TaskPRServiceDeps{
		Tasks:         tasks,
		Repos:         taskChatRepos{root: "/repos/widget"},
		Git:           git,
		PRs:           &mergePRs{pr: pr},
		Tokens:        func(context.Context) (string, error) { return "tok", nil },
		Gates:         gates,
		WorkspaceRoot: "/data/workspaces",
	})
	return svc, tasks, git, repositoryID
}

func TestMergeTaskPullRequestSquashesDeletesTheBranchAndRecordsTheCommit(t *testing.T) {
	task := mergeTask()
	svc, tasks, git, repositoryID := newMergeFixture(task, openCleanPR(), &mergeGates{
		pipeline: domain.TaskPipeline{Status: domain.PipelineStatusSuccess},
	})

	result, err := svc.MergeTaskPullRequest(context.Background(), repositoryID, task.ID)
	require.NoError(t, err)

	require.Len(t, git.mergeReqs, 1)
	req := git.mergeReqs[0]
	assert.Equal(t, "acme", req.Owner)
	assert.Equal(t, "widget", req.Repo)
	assert.Equal(t, 42, req.Number)
	assert.Equal(t, mergeHeadSHA, req.ExpectedHeadSHA)
	assert.True(t, req.DeleteBranch)
	assert.Equal(t, "feature/t-7", req.Branch)
	assert.False(t, req.Undraft)
	assert.Equal(t, "Add the store link (#42)", req.CommitTitle)

	assert.True(t, result.Merged)
	assert.True(t, result.BranchDeleted)
	assert.Equal(t, "mergecommitsha0000000000000000000000000", result.MergeCommitSHA)
	assert.Contains(t, result.Message, "squash")
	assert.Equal(t, "mergecommitsha0000000000000000000000000", tasks.merges[task.ID])
}

func TestMergeTaskPullRequestReportsAutoRelease(t *testing.T) {
	task := mergeTask()
	gates := &mergeGates{
		pipeline:     domain.TaskPipeline{Status: domain.PipelineStatusSuccess},
		autoReleased: true,
	}
	svc, _, _, repositoryID := newMergeFixture(task, openCleanPR(), gates)

	result, err := svc.MergeTaskPullRequest(context.Background(), repositoryID, task.ID)
	require.NoError(t, err)

	assert.Equal(t, 1, gates.autoReleaseCalls)
	assert.True(t, result.AutoReleased)
	assert.Contains(t, result.Message, "do not call trigger_release")
}

func TestMergeTaskPullRequestWithoutAutoReleaseReportsNone(t *testing.T) {
	task := mergeTask()
	gates := &mergeGates{
		pipeline:     domain.TaskPipeline{Status: domain.PipelineStatusSuccess},
		autoReleased: false,
	}
	svc, _, _, repositoryID := newMergeFixture(task, openCleanPR(), gates)

	result, err := svc.MergeTaskPullRequest(context.Background(), repositoryID, task.ID)
	require.NoError(t, err)

	assert.Equal(t, 1, gates.autoReleaseCalls)
	assert.False(t, result.AutoReleased)
	assert.NotContains(t, result.Message, "trigger_release")
}

type fakeReleaseOpener struct {
	calls   int
	task    domain.BoardTask
	sha     string
	opening domain.ReleaseOpening
}

func (f *fakeReleaseOpener) OpenForMerge(_ context.Context, _ uuid.UUID, task domain.BoardTask, sha string) domain.ReleaseOpening {
	f.calls++
	f.task = task
	f.sha = sha
	return f.opening
}

func TestMergeTaskPullRequestOpensAReleaseAndSkipsTheLegacyAutoRelease(t *testing.T) {
	task := mergeTask()
	repositoryID := uuid.New()
	tasks := &taskChatTaskStore{tasks: map[[2]uuid.UUID]domain.BoardTask{{repositoryID, task.ID}: task}}
	git := &taskPRGit{hasGit: true, branch: "feature/t-7"}
	gates := &mergeGates{pipeline: domain.TaskPipeline{Status: domain.PipelineStatusSuccess}}
	opener := &fakeReleaseOpener{opening: domain.ReleaseOpening{Mode: domain.DeliveryOnMerge, Next: "call watch_release"}}
	svc := NewTaskPRService(TaskPRServiceDeps{
		Tasks:         tasks,
		Repos:         taskChatRepos{root: "/repos/widget"},
		Git:           git,
		PRs:           &mergePRs{pr: openCleanPR()},
		Tokens:        func(context.Context) (string, error) { return "tok", nil },
		Gates:         gates,
		Releases:      opener,
		WorkspaceRoot: "/data/workspaces",
	})

	result, err := svc.MergeTaskPullRequest(context.Background(), repositoryID, task.ID)
	require.NoError(t, err)

	assert.Equal(t, 1, opener.calls, "the release opener must be called exactly once")
	assert.Equal(t, 0, gates.autoReleaseCalls, "the legacy auto-release gate must not run once an opener is wired")
	assert.Equal(t, "mergecommitsha0000000000000000000000000", opener.sha)
	assert.Equal(t, "mergecommitsha0000000000000000000000000", opener.task.MergeCommitSHA,
		"the task handed to OpenForMerge must carry the merge commit that was just recorded")
	require.NotNil(t, result.Release)
	assert.Equal(t, domain.DeliveryOnMerge, result.Release.Mode)
	assert.Contains(t, result.Message, "call watch_release")
	assert.False(t, result.AutoReleased, "AutoReleased stays false once an opener replaces the legacy path")
}

func TestMergeTaskPullRequestWithoutGatesNeverCallsAutoRelease(t *testing.T) {
	task := mergeTask()
	repositoryID := uuid.New()
	tasks := &taskChatTaskStore{tasks: map[[2]uuid.UUID]domain.BoardTask{{repositoryID, task.ID}: task}}
	git := &taskPRGit{hasGit: true, branch: "feature/t-7"}
	svc := NewTaskPRService(TaskPRServiceDeps{
		Tasks:         tasks,
		Repos:         taskChatRepos{root: "/repos/widget"},
		Git:           git,
		PRs:           &mergePRs{pr: openCleanPR()},
		Tokens:        func(context.Context) (string, error) { return "tok", nil },
		WorkspaceRoot: "/data/workspaces",
	})

	_, err := svc.MergeTaskPullRequest(context.Background(), repositoryID, task.ID)
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrMergeNotConfigured)
}

func TestMergeCommitTitlePrefersThePRTitleOverTheTaskTitle(t *testing.T) {
	task := domain.BoardTask{Key: "T-7", Title: "Mağaza linkini ekle"}
	pr := port.PullRequest{Number: 42, Title: "feat(store): add the store link"}

	assert.Equal(t, "feat(store): add the store link (#42)", mergeCommitTitle(task, pr))
}

func TestMergeCommitTitleFallsBackToTheTaskTitleWithoutAPRTitle(t *testing.T) {
	task := domain.BoardTask{Key: "T-7", Title: "Add the store link"}
	pr := port.PullRequest{Number: 42}

	assert.Equal(t, "Add the store link (#42)", mergeCommitTitle(task, pr))
}

func TestMergeCommitTitleFallsBackToAGenericTitleWithNeither(t *testing.T) {
	task := domain.BoardTask{}
	pr := port.PullRequest{Number: 42}

	assert.Equal(t, "Merge pull request #42 (#42)", mergeCommitTitle(task, pr))
}

func TestMergeTaskPullRequestUndraftsALegacyDraft(t *testing.T) {
	task := mergeTask()
	pr := openCleanPR()
	pr.Draft = true
	pr.MergeableState = "draft"
	svc, _, git, repositoryID := newMergeFixture(task, pr, &mergeGates{
		pipeline: domain.TaskPipeline{Status: domain.PipelineStatusSuccess},
	})

	result, err := svc.MergeTaskPullRequest(context.Background(), repositoryID, task.ID)
	require.NoError(t, err)

	require.Len(t, git.mergeReqs, 1)
	assert.True(t, git.mergeReqs[0].Undraft)
	assert.True(t, result.Undrafted)
	assert.Contains(t, result.Message, "draft")
}

func TestMergeTaskPullRequestRefusalMatrix(t *testing.T) {
	cases := []struct {
		name  string
		task  func(domain.BoardTask) domain.BoardTask
		pr    func(port.PullRequest) port.PullRequest
		gates *mergeGates
		want  error
	}{
		{
			name: "task is not in done",
			task: func(task domain.BoardTask) domain.BoardTask {
				task.Column = domain.TaskColumnInQA
				return task
			},
			want: domain.ErrMergeTaskNotDone,
		},
		{
			name: "task has no pull request",
			task: func(task domain.BoardTask) domain.BoardTask {
				task.PRURL, task.PRNumber = "", 0
				return task
			},
			want: domain.ErrMergeNoPullRequest,
		},
		{
			name: "the board already recorded a merge",
			task: func(task domain.BoardTask) domain.BoardTask {
				task.MergeCommitSHA = "abc1234567890000000000000000000000000000"
				return task
			},
			want: domain.ErrMergeAlreadyMerged,
		},
		{
			name: "github reports it already merged",
			pr: func(pr port.PullRequest) port.PullRequest {
				pr.Merged = true
				pr.State = "closed"
				return pr
			},
			want: domain.ErrMergeAlreadyMerged,
		},
		{
			name: "the pull request was closed unmerged",
			pr: func(pr port.PullRequest) port.PullRequest {
				pr.State = "closed"
				return pr
			},
			want: domain.ErrMergeClosed,
		},
		{
			name: "a required check is red",
			pr: func(pr port.PullRequest) port.PullRequest {
				pr.MergeableState = "blocked"
				return pr
			},
			want: domain.ErrMergeChecksNotGreen,
		},
		{
			name: "a non-required check is red",
			pr: func(pr port.PullRequest) port.PullRequest {
				pr.MergeableState = "unstable"
				return pr
			},
			want: domain.ErrMergeChecksNotGreen,
		},
		{
			name: "the branch conflicts with its base",
			pr: func(pr port.PullRequest) port.PullRequest {
				pr.MergeableState = "dirty"
				return pr
			},
			want: domain.ErrMergeChecksNotGreen,
		},
		{
			name: "github has not computed mergeability yet",
			pr: func(pr port.PullRequest) port.PullRequest {
				pr.MergeableState = "unknown"
				return pr
			},
			want: domain.ErrMergeChecksNotGreen,
		},
		{
			name:  "the board's own pipeline failed",
			gates: &mergeGates{pipeline: domain.TaskPipeline{Status: domain.PipelineStatusFailed, Trigger: domain.PipelineTriggerReadyForQA}},
			want:  domain.ErrMergeChecksNotGreen,
		},
		{
			name:  "the review chain is incomplete",
			gates: &mergeGates{chainErr: domain.ErrReviewChainIncomplete},
			want:  domain.ErrReviewChainIncomplete,
		},
		{
			name: "the head moved since the task was verified",
			pr: func(pr port.PullRequest) port.PullRequest {
				pr.HeadSHA = mergeOtherSHA
				return pr
			},
			want: domain.ErrReleaseTargetMoved,
		},
		{
			name: "nothing was ever stamped on the task",
			task: func(task domain.BoardTask) domain.BoardTask {
				task.VerifiedSHA = ""
				return task
			},
			want: domain.ErrReleaseTargetUnverified,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			task := mergeTask()
			if tc.task != nil {
				task = tc.task(task)
			}
			pr := openCleanPR()
			if tc.pr != nil {
				pr = tc.pr(pr)
			}
			gates := tc.gates
			if gates == nil {
				gates = &mergeGates{pipeline: domain.TaskPipeline{Status: domain.PipelineStatusSuccess}}
			}
			svc, _, git, repositoryID := newMergeFixture(task, pr, gates)

			_, err := svc.MergeTaskPullRequest(context.Background(), repositoryID, task.ID)

			require.Error(t, err)
			assert.ErrorIs(t, err, tc.want)
			assert.Empty(t, git.mergeReqs, "a refused merge must not call GitHub")
		})
	}
}

func TestMergeTaskPullRequestRecordsAMergeItDidNotMake(t *testing.T) {
	task := mergeTask()
	pr := openCleanPR()
	pr.Merged = true
	pr.State = "closed"
	svc, tasks, _, repositoryID := newMergeFixture(task, pr, &mergeGates{
		pipeline: domain.TaskPipeline{Status: domain.PipelineStatusSuccess},
	})

	_, err := svc.MergeTaskPullRequest(context.Background(), repositoryID, task.ID)

	assert.ErrorIs(t, err, domain.ErrMergeAlreadyMerged)
	assert.Equal(t, mergeHeadSHA, tasks.merges[task.ID])
}

func TestMergeTaskPullRequestProceedsWithoutAPipeline(t *testing.T) {
	task := mergeTask()
	svc, _, git, repositoryID := newMergeFixture(task, openCleanPR(), &mergeGates{
		pipelineErr: domain.ErrPipelineNotFound,
	})

	_, err := svc.MergeTaskPullRequest(context.Background(), repositoryID, task.ID)

	require.NoError(t, err)
	assert.Len(t, git.mergeReqs, 1)
}

func TestMergeTaskPullRequestReportsAnUnrecordedMerge(t *testing.T) {
	task := mergeTask()
	svc, tasks, _, repositoryID := newMergeFixture(task, openCleanPR(), &mergeGates{
		pipeline: domain.TaskPipeline{Status: domain.PipelineStatusSuccess},
	})
	tasks.mergeErr = errors.New("database is on fire")

	result, err := svc.MergeTaskPullRequest(context.Background(), repositoryID, task.ID)

	require.NoError(t, err)
	assert.True(t, result.Merged)
	assert.Contains(t, result.Message, "WARNING")
}

func TestMergeTaskPullRequestReportsAnUndeletedBranch(t *testing.T) {
	task := mergeTask()
	svc, _, git, repositoryID := newMergeFixture(task, openCleanPR(), &mergeGates{
		pipeline: domain.TaskPipeline{Status: domain.PipelineStatusSuccess},
	})
	git.mergeResult = domain.PullRequestMergeResult{BranchDeleteError: "403 Forbidden"}

	result, err := svc.MergeTaskPullRequest(context.Background(), repositoryID, task.ID)

	require.NoError(t, err)
	assert.True(t, result.Merged)
	assert.False(t, result.BranchDeleted)
	assert.Contains(t, result.Message, "could NOT be deleted")
}
