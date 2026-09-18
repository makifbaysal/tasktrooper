package board

// The merge gate's refusal matrix, and the one path that lands code.
//
// These tests are written as a matrix rather than as a happy path with a few
// error cases because the refusals ARE the feature: merging is irreversible, so
// every one of them is the difference between an unreviewed commit on the
// default branch and a card that stays where it is.

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

// mergePRs is the GitHub half: one PR, whatever shape the test needs.
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

// mergeGates is the repository half: the review chain verdict and the task's
// last pipeline, both of which the merge re-asks rather than assuming.
type mergeGates struct {
	chainErr    error
	pipeline    domain.TaskPipeline
	pipelineErr error
	// autoReleased scripts what AutoReleaseIfUndeployable reports, defaulting
	// to false so every existing fixture keeps merging into `done` unchanged.
	autoReleased bool
	// autoReleaseCalls counts every AutoReleaseIfUndeployable call, so a test
	// can assert it was (or was not) asked at all.
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

// mergeTask is a task in the state the merge is meant to succeed for: done,
// signed off at the commit its PR is at, with the PR recorded and unmerged.
func mergeTask() domain.BoardTask {
	return domain.BoardTask{
		ID:          uuid.New(),
		Key:         "T-7",
		Title:       "Add the store link",
		TaskType:    domain.TaskTypeTask,
		Column:      domain.TaskColumnDone,
		VerifiedSHA: mergeHeadSHA,
		PRURL:       "https://github.com/acme/widget/pull/42",
		PRNumber:    42,
	}
}

// openCleanPR is the PR GitHub reports for that task: open, ready, clean.
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

// The whole point: a signed-off task's change reaches the default branch, as ONE
// squash commit, on the exact commit the board verified, with the branch cleaned
// up and the commit recorded so nothing merges it twice.
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
	// The precondition is the safety property: GitHub must merge this commit or
	// nothing.
	assert.Equal(t, mergeHeadSHA, req.ExpectedHeadSHA)
	assert.True(t, req.DeleteBranch)
	assert.Equal(t, "feature/t-7", req.Branch)
	// A ready PR is not un-drafted: there is nothing to repair.
	assert.False(t, req.Undraft)
	// openCleanPR's fixture carries no PR title, so this falls back to the
	// task's own title — with no task key glued onto it; see mergeCommitTitle.
	assert.Equal(t, "Add the store link (#42)", req.CommitTitle)

	assert.True(t, result.Merged)
	assert.True(t, result.BranchDeleted)
	assert.Equal(t, "mergecommitsha0000000000000000000000000", result.MergeCommitSHA)
	assert.Contains(t, result.Message, "squash")
	// Recorded on the task: this is what stops the done column asking for the
	// same merge again.
	assert.Equal(t, "mergecommitsha0000000000000000000000000", tasks.merges[task.ID])
}

// When the repository has no deploy_target configured anywhere, the merge
// also auto-releases the task, and the QA agent reading the result must be
// told not to call trigger_release.
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

// The ordinary case — at least one deploy target configured — must not claim
// an auto-release that never happened.
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

// A configured-out gates dependency (s.gates == nil) is an existing, already
// refused path for every other call the merge makes to it — the auto-release
// check must be guarded the same way and never panic.
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

// The squash title is the PR's own subject, not the task key glued onto it —
// gluing a task key onto an otherwise-conventional subject is exactly the
// commitlint-breaking shape this used to produce on main.
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

// A PR opened before task PRs became ready-for-review PRs is still a draft on
// GitHub, and GitHub refuses to merge one. The repair path has to run — and it
// has to run only for those.
func TestMergeTaskPullRequestUndraftsALegacyDraft(t *testing.T) {
	task := mergeTask()
	pr := openCleanPR()
	pr.Draft = true
	// A draft reports mergeable_state "draft"; the gate must not read that as a
	// red check.
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

// The refusal matrix. Every row is a state in which nothing may be merged, and
// the assertion is on the sentinel rather than on the wording, so the message
// can be improved without the guard quietly disappearing.
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
			// The only thing that actually matters: no refusal reaches GitHub.
			assert.Empty(t, git.mergeReqs, "a refused merge must not call GitHub")
		})
	}
}

// A PR merged outside the board still gets its commit recorded, because the
// dispatcher decides whether done needs waking from that column alone — leaving
// it empty would wake QA on this task for good.
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

// A missing pipeline is a repository with no CI wired up, not a red build:
// refusing there would make the merge unreachable for every such install.
// GitHub's own mergeable state is the check that remains.
func TestMergeTaskPullRequestProceedsWithoutAPipeline(t *testing.T) {
	task := mergeTask()
	svc, _, git, repositoryID := newMergeFixture(task, openCleanPR(), &mergeGates{
		pipelineErr: domain.ErrPipelineNotFound,
	})

	_, err := svc.MergeTaskPullRequest(context.Background(), repositoryID, task.ID)

	require.NoError(t, err)
	assert.Len(t, git.mergeReqs, 1)
}

// The merge happened; only the bookkeeping failed. Reporting that as an error
// would tell the agent nothing landed, and the next run would try to merge a
// merged PR — so it is a warning inside a successful result instead.
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

// A branch that survives the merge is reported, never mistaken for a failed
// merge: the change is on the default branch either way.
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
