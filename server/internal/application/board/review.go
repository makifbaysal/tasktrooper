package board

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/rs/zerolog/log"
)

const reviewDiffLimit = 24000

var ErrReviewPRMissing = errors.New("code review has no pull request for the task branch")

// A reviewing stage is one whose whole job is to judge work someone else did
// — its kind, not a flag of its own: there is no such thing as a review or
// approval stage that is not one.
func isReviewColumn(wf domain.Workflow, column domain.TaskColumn) bool {
	switch wf.KindOf(column) {
	case domain.StageKindReview, domain.StageKindApproval:
		return true
	default:
		return false
	}
}

func (r *Runner) ensureReviewPR(ctx context.Context, workspace string) (string, error) {
	if r.git == nil || workspace == "" {
		return "", nil
	}
	url, err := r.git.EnsurePullRequest(ctx, workspace)
	if err == nil {
		return url, nil
	}
	if errors.Is(err, domain.ErrNotGitHubRepo) {
		return "", err
	}
	firstErr := err
	if pushErr := r.git.PushBranch(ctx, workspace); pushErr != nil {
		return "", fmt.Errorf("%w: %v (publishing the branch also failed: %v)", ErrReviewPRMissing, firstErr, pushErr)
	}
	url, err = r.git.EnsurePullRequest(ctx, workspace)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrReviewPRMissing, err)
	}
	return url, nil
}

func (r *Runner) reviewPRContext(ctx context.Context, workspace string, taskID uuid.UUID) (string, error) {
	url, err := r.ensureReviewPR(ctx, workspace)
	if errors.Is(err, domain.ErrNotGitHubRepo) {
		log.Info().Str("workspace", workspace).
			Msg("code review: origin is not a GitHub repo, reviewing the branch diff without a pull request")
		return "", nil
	}
	if err != nil {
		if r.git != nil && r.git.OriginURL(ctx, workspace) == "" {
			log.Warn().Err(err).Str("workspace", workspace).
				Msg("code review: repository has no origin, reviewing the branch diff without a pull request")
			return "", nil
		}
		return "", err
	}
	if url == "" {
		return "", nil
	}
	recordTaskPR(ctx, r.prRecorder, taskID, url)
	return reviewPRContextMessage(url), nil
}

// reviewPRContextMessage points the reviewer at the PR the diff below came
// from and scopes what "review" means: this PR, not the whole repository.
func reviewPRContextMessage(url string) string {
	return reviewPRContextKey.Render(reviewPRContextData{URL: url})
}

func (r *Runner) revisionPRComments(ctx context.Context, job RunJob) string {
	if r.prReader == nil {
		return ""
	}
	pr, err := r.prReader.PullRequest(ctx, job.RepositoryID, job.Task.ID, false)
	if err != nil {
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("revision context: pull request read failed")
		return ""
	}
	if !pr.Known || (len(pr.ReviewComments) == 0 && len(pr.Comments) == 0) {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(revisionPRCommentsHeader(pr.URL))
	write := func(c domain.PullRequestComment) {
		where := ""
		if c.Path != "" {
			where = " " + c.Path
			if c.Line > 0 {
				where += fmt.Sprintf(":%d", c.Line)
			}
		}
		body := c.Body
		if len(body) > 1500 {
			body = domain.TruncateHead(body, 1500) + "…"
		}
		sb.WriteString(fmt.Sprintf("- [%s%s] %s\n", c.Author, where, strings.TrimSpace(body)))
	}
	for _, c := range pr.ReviewComments {
		write(c)
	}
	for _, c := range pr.Comments {
		write(c)
	}
	return sb.String()
}

// revisionPRCommentsHeader frames the PR's own review thread as revision
// feedback, not a separate conversation to be read and set aside.
func revisionPRCommentsHeader(url string) string {
	return reviewPRCommentsHeaderKey.Render(reviewPRCommentsHeaderData{URL: url}) + "\n"
}

// reviewNoPRReason is why a code_review run refuses to start: the branch
// has no pull request, and review happens on the PR.
func reviewNoPRReason(cause error) string {
	return reviewNoPRReasonKey.Render(reviewNoPRReasonInput{Cause: cause.Error()})
}

func (r *Runner) failRunNoPR(ctx context.Context, job RunJob, run domain.TaskAgentRun, cause error) error {
	reason := reviewNoPRReason(cause)

	if r.taskUpdater != nil {
		if _, err := r.taskUpdater.AddComment(ctx, job.RepositoryID, job.Task.ID, domain.CreateTaskCommentRequest{
			AuthorType: "system",
			Content:    truncateHead(reason, 3000),
		}); err != nil {
			log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("missing-PR review comment failed")
		}
	}

	log.Warn().Err(cause).Str("task_id", job.Task.ID.String()).Str("run_id", run.ID.String()).
		Msg("code review run rejected: no pull request for the task branch")

	run.Status = domain.TaskAgentRunStatusFailed
	run.Summary = truncateHead(reason, 500)
	pctx, cancel := persistCtx(ctx)
	defer cancel()
	if _, updateErr := r.runs.Update(pctx, run); updateErr != nil {
		if errors.Is(updateErr, domain.ErrTaskAgentRunNotFound) {
			log.Info().Str("run_id", run.ID.String()).Msg("run row gone, task deleted mid-run")
			return nil
		}
		return updateErr
	}
	return fmt.Errorf("%w (task %s)", ErrReviewPRMissing, job.Task.ID)
}

func reviewDiffMessage(wf domain.Workflow, column domain.TaskColumn, diff string) string {
	limit := 8000
	heading := prompt.Text(reviewDiffHeadingBranchKey)
	if isReviewColumn(wf, column) {
		limit = reviewDiffLimit
		heading = prompt.Text(reviewDiffHeadingPRKey)
	}
	if len(diff) > limit {
		diff = domain.TruncateHead(diff, limit) + "\n…(truncated)"
	}
	return heading + "\n```diff\n" + strings.TrimSpace(diff) + "\n```"
}
