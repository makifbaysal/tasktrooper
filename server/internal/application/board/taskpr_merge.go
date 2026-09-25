package board

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// unstable still refuses: required checks green but an optional job red is exactly what a person would stop a merge for.
var mergeableStates = map[string]bool{
	"clean":     true,
	"has_hooks": true,
}

// A refusal is a state no retry can change; re-trying would spend the run.
func (s *TaskPRService) MergeTaskPullRequest(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.TaskPRMergeResult, error) {
	task, err := s.tasks.Get(ctx, repositoryID, taskID)
	if err != nil {
		return domain.TaskPRMergeResult{}, err
	}

	if task.Column != domain.TaskColumnDone {
		return domain.TaskPRMergeResult{}, s.refuse(task, fmt.Errorf(
			"%w — %s is in `%s`. A pull request is merged when the board has signed the task off, not while it is still being reviewed or tested",
			domain.ErrMergeTaskNotDone, taskLabel(task), task.Column))
	}

	if sha := strings.TrimSpace(task.MergeCommitSHA); sha != "" {
		return domain.TaskPRMergeResult{}, s.refuse(task, fmt.Errorf(
			"%w — %s was merged as %s. Nothing further is needed here",
			domain.ErrMergeAlreadyMerged, taskLabel(task), domain.ShortSHA(sha)))
	}

	number, prURL := taskPRRef(task)
	if prURL == "" {
		return domain.TaskPRMergeResult{}, s.refuse(task, fmt.Errorf(
			"%w — %s has no pull request recorded, so there is nothing to merge. Its branch was never pushed, or the PR was opened outside the board",
			domain.ErrMergeNoPullRequest, taskLabel(task)))
	}
	if number <= 0 {
		return domain.TaskPRMergeResult{}, s.refuse(task, fmt.Errorf(
			"%w — the recorded pull request URL (%s) carries no readable number, so it cannot be merged through the API. Merge it by hand",
			domain.ErrMergeNoPullRequest, prURL))
	}

	if s.gates == nil || s.prs == nil || s.git == nil {
		return domain.TaskPRMergeResult{}, s.refuse(task, fmt.Errorf(
			"%w — this deployment has no GitHub pull-request access wired up", domain.ErrMergeNotConfigured))
	}
	token := s.token(ctx)
	if token == "" {
		return domain.TaskPRMergeResult{}, s.refuse(task, fmt.Errorf(
			"%w — GitHub is not connected", domain.ErrMergeNotConfigured))
	}

	if err := s.gates.CheckReviewChain(ctx, repositoryID, taskID); err != nil {
		return domain.TaskPRMergeResult{}, s.refuse(task, fmt.Errorf(
			"merge refused: %w", err))
	}

	if err := s.pipelineIsGreen(ctx, repositoryID, taskID); err != nil {
		return domain.TaskPRMergeResult{}, s.refuse(task, err)
	}

	// The last gate before anything touches GitHub: an on_merge component's
	// deploy IS the merge, so a task with unconfirmed before-deploy steps must
	// not land — a human has to perform them and press "Confirm before-deploy
	// steps" first. release.Service.MergeGate posts that explanation as a
	// comment itself; the wrapped error already says not to retry.
	if s.releases != nil {
		if err := s.releases.MergeGate(ctx, repositoryID, task); err != nil {
			return domain.TaskPRMergeResult{}, s.refuse(task, err)
		}
	}

	owner, repo, err := s.ownerRepo(ctx, repositoryID, taskID)
	if err != nil {
		return domain.TaskPRMergeResult{}, s.refuse(task, fmt.Errorf(
			"merge refused: the GitHub owner/repo for this task could not be resolved: %w", err))
	}
	pr, err := s.prs.GetPullRequest(ctx, token, owner, repo, number)
	if err != nil {
		return domain.TaskPRMergeResult{}, fmt.Errorf("read pull request #%d before merging it: %w", number, err)
	}

	if pr.Merged {
		s.recordMergeCommit(ctx, taskID, pr.HeadSHA, "")
		return domain.TaskPRMergeResult{}, s.refuse(task, fmt.Errorf(
			"%w — pull request #%d is already merged on GitHub (it was merged outside this board)",
			domain.ErrMergeAlreadyMerged, number))
	}
	if strings.EqualFold(pr.State, "closed") {
		return domain.TaskPRMergeResult{}, s.refuse(task, fmt.Errorf(
			"%w — pull request #%d was closed without merging. Someone decided against this change; reopening it is a human's call",
			domain.ErrMergeClosed, number))
	}
	if pr.Draft {
		log.Warn().Str("task_id", taskID.String()).Int("pull_request", number).
			Msg("merge: pull request is a legacy draft, so GitHub's mergeable state cannot be judged before un-drafting it")
	}
	if !pr.Draft && !mergeableStates[strings.ToLower(strings.TrimSpace(pr.MergeableState))] {
		return domain.TaskPRMergeResult{}, s.refuse(task, fmt.Errorf(
			"%w — GitHub reports pull request #%d as `%s` (expected `clean`). %s",
			domain.ErrMergeChecksNotGreen, number, pr.MergeableState, mergeableStateRemedy(pr.MergeableState)))
	}

	switch err := domain.VerifiedCommitMatches(task.VerifiedSHA, pr.HeadSHA); {
	case errors.Is(err, domain.ErrReleaseTargetUnverified):
		return domain.TaskPRMergeResult{}, s.refuse(task, fmt.Errorf(
			"%w: no verified commit is stamped on this task, while its pull request is at %s. "+
				"Move it back through review (need_revision → code_review → … → done): reaching done stamps the commit that was signed off, which is what this gate compares against",
			domain.ErrReleaseTargetUnverified, domain.ShortSHA(pr.HeadSHA)))
	case errors.Is(err, domain.ErrReleaseTargetMoved):
		return domain.TaskPRMergeResult{}, s.refuse(task, fmt.Errorf(
			"%w: verified at %s, but the pull request head is now at %s. "+
				"Something was pushed after this task was signed off — send it back through review so the new commits are reviewed and QA'd; returning it to done re-stamps the verified commit",
			domain.ErrReleaseTargetMoved, domain.ShortSHA(task.VerifiedSHA), domain.ShortSHA(pr.HeadSHA)))
	case err != nil:
		return domain.TaskPRMergeResult{}, s.refuse(task, fmt.Errorf("merge refused: %w", err))
	}

	log.Info().Str("task_id", taskID.String()).Str("owner", owner).Str("repo", repo).
		Int("pull_request", number).Str("head_sha", pr.HeadSHA).Bool("draft", pr.Draft).
		Msg("merging task pull request (squash)")

	merge, err := s.git.MergePullRequest(ctx, domain.PullRequestMergeRequest{
		Owner:           owner,
		Repo:            repo,
		Number:          number,
		Branch:          pr.HeadRef,
		ExpectedHeadSHA: pr.HeadSHA,
		Undraft:         pr.Draft,
		DeleteBranch:    true,
		CommitTitle:     mergeCommitTitle(task, pr),
		CommitBody:      mergeCommitBody(task, prURL),
	})
	if err != nil {
		log.Warn().Err(err).Str("task_id", taskID.String()).Int("pull_request", number).
			Msg("task pull request merge failed")
		return domain.TaskPRMergeResult{}, err
	}

	out := domain.TaskPRMergeResult{
		Merged:         true,
		PRNumber:       number,
		PRURL:          prURL,
		MergeCommitSHA: merge.MergeCommitSHA,
		Branch:         pr.HeadRef,
		BaseBranch:     pr.BaseRef,
		BranchDeleted:  merge.BranchDeleted,
		Undrafted:      merge.Undrafted,
	}
	recordErr := s.recordMergeCommit(ctx, taskID, merge.MergeCommitSHA, prURL)
	if s.releases != nil {
		mergedTask := task
		mergedTask.MergeCommitSHA = merge.MergeCommitSHA
		opening := s.releases.OpenForMerge(ctx, repositoryID, mergedTask, merge.MergeCommitSHA)
		out.Release = &opening
	} else if s.gates != nil {
		out.AutoReleased = s.gates.AutoReleaseIfUndeployable(ctx, repositoryID, taskID)
	}
	out.Message = mergeMessage(out, merge.BranchDeleteError, recordErr)
	log.Info().Str("task_id", taskID.String()).Int("pull_request", number).
		Str("merge_commit", merge.MergeCommitSHA).Bool("branch_deleted", merge.BranchDeleted).
		Msg("task pull request merged")
	return out, nil
}

func (s *TaskPRService) pipelineIsGreen(ctx context.Context, repositoryID, taskID uuid.UUID) error {
	pipeline, err := s.gates.LatestTaskPipeline(ctx, repositoryID, taskID)
	if err != nil {
		if !errors.Is(err, domain.ErrPipelineNotFound) {
			log.Warn().Err(err).Str("task_id", taskID.String()).
				Msg("merge gate: task pipeline could not be read, relying on GitHub's mergeable state")
		}
		return nil
	}
	if pipeline.Status != domain.PipelineStatusFailed {
		return nil
	}
	failed := make([]string, 0, len(pipeline.Jobs))
	for _, job := range pipeline.Jobs {
		if job.Status == domain.PipelineJobStatusFailed {
			failed = append(failed, job.Name)
		}
	}
	detail := ""
	if len(failed) > 0 {
		detail = " Failing jobs: " + strings.Join(failed, ", ") + "."
	}
	return fmt.Errorf(
		"%w — the last %s pipeline for this task FAILED.%s Send the task back to need_revision so the developer fixes it; a red build is not merged and then fixed on the default branch",
		domain.ErrMergeChecksNotGreen, pipeline.Trigger, detail)
}

func mergeableStateRemedy(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "blocked":
		return "A required check is red or still running, or a required review is missing. Read the PR checks (get_task_pull_request) and send the task back to need_revision if the build is broken."
	case "unstable":
		return "A check on this PR is failing. It is not a required one, so GitHub would merge it — this board does not: report the failing check and send the task back to need_revision if it is real."
	case "dirty":
		return "The branch conflicts with its base. It has to be rebased or merged by whoever owns the code — send the task back to need_revision."
	case "behind":
		return "The base branch has moved and this repository requires branches to be up to date. The branch has to be brought up to date by whoever owns the code — send the task back to need_revision."
	case "unknown", "":
		return "GitHub has not finished computing this PR's mergeability. Wait a moment and read the PR again before trying once more."
	default:
		return "Read the PR's checks and conversation before doing anything else."
	}
}

// Recorded merge is a warning, not a failure: an error sends the agent to merge a merged PR.
func (s *TaskPRService) recordMergeCommit(ctx context.Context, taskID uuid.UUID, sha, prURL string) error {
	if strings.TrimSpace(sha) == "" {
		return nil
	}
	if err := s.tasks.SetTaskMergeCommit(ctx, taskID, sha); err != nil {
		log.Error().Err(err).Str("task_id", taskID.String()).Str("merge_commit", sha).Str("pr_url", prURL).
			Msg("the pull request was merged but the merge commit could not be recorded on the task")
		return err
	}
	return nil
}

func (s *TaskPRService) refuse(task domain.BoardTask, err error) error {
	log.Warn().Err(err).Str("task_id", task.ID.String()).Str("task_key", task.Key).
		Str("column", string(task.Column)).Msg("task pull request merge refused")
	return err
}

func mergeCommitTitle(task domain.BoardTask, pr port.PullRequest) string {
	title := strings.TrimSpace(pr.Title)
	if title == "" {
		title = strings.TrimSpace(task.Title)
	}
	if title == "" {
		title = fmt.Sprintf("Merge pull request #%d", pr.Number)
	}
	return fmt.Sprintf("%s (#%d)", title, pr.Number)
}

func mergeCommitBody(task domain.BoardTask, prURL string) string {
	lines := []string{}
	if key := strings.TrimSpace(task.Key); key != "" {
		lines = append(lines, "Task: "+key+" "+strings.TrimSpace(task.Title))
	}
	if prURL != "" {
		lines = append(lines, "Pull request: "+prURL)
	}
	if sha := strings.TrimSpace(task.VerifiedSHA); sha != "" {
		lines = append(lines, "Verified at: "+sha)
	}
	return strings.Join(lines, "\n")
}

func mergeMessage(out domain.TaskPRMergeResult, branchDeleteErr string, recordErr error) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Merged pull request #%d into %s as %s (squash).",
		out.PRNumber, fallback(out.BaseBranch, "the base branch"), domain.ShortSHA(out.MergeCommitSHA))
	if out.Undrafted {
		sb.WriteString(" The PR was still a draft and was marked ready for review first.")
	}
	switch {
	case out.Release != nil:
		sb.WriteString(" " + out.Release.Next)
	case out.AutoReleased:
		sb.WriteString(" This repository has no deploy target configured, so the merge released the task directly — do not call trigger_release.")
	}
	switch {
	case out.BranchDeleted:
		sb.WriteString(" Branch " + out.Branch + " deleted.")
	case branchDeleteErr != "":
		sb.WriteString(" The branch " + out.Branch + " could NOT be deleted (" + branchDeleteErr + "); delete it by hand.")
	}
	if recordErr != nil {
		sb.WriteString(" WARNING: the merge commit could not be recorded on the task (" + recordErr.Error() +
			"), so the board may ask for this merge again — say so on the card.")
	}
	return sb.String()
}

func fallback(value, alt string) string {
	if strings.TrimSpace(value) == "" {
		return alt
	}
	return value
}

func taskLabel(task domain.BoardTask) string {
	if strings.TrimSpace(task.Key) != "" {
		return task.Key
	}
	return task.ID.String()
}
