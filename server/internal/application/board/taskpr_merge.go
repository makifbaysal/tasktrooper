package board

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
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
			"%w — %s", domain.ErrMergeTaskNotDone,
			mergeTaskNotDoneKey.Render(taskLabelColumnInput{Label: taskLabel(task), Column: string(task.Column)})))
	}

	if sha := strings.TrimSpace(task.MergeCommitSHA); sha != "" {
		return domain.TaskPRMergeResult{}, s.refuse(task, fmt.Errorf(
			"%w — %s", domain.ErrMergeAlreadyMerged,
			mergeAlreadyMergedRecordedKey.Render(taskLabelSHAInput{Label: taskLabel(task), SHA: domain.ShortSHA(sha)})))
	}

	number, prURL := taskPRRef(task)
	if prURL == "" {
		return domain.TaskPRMergeResult{}, s.refuse(task, fmt.Errorf(
			"%w — %s", domain.ErrMergeNoPullRequest,
			mergeNoPRRecordedKey.Render(taskLabelInput{Label: taskLabel(task)})))
	}
	if number <= 0 {
		return domain.TaskPRMergeResult{}, s.refuse(task, fmt.Errorf(
			"%w — %s", domain.ErrMergeNoPullRequest,
			mergePRNumberUnreadableKey.Render(mergeURLInput{URL: prURL})))
	}

	if s.gates == nil || s.prs == nil || s.git == nil {
		return domain.TaskPRMergeResult{}, s.refuse(task, fmt.Errorf(
			"%w — %s", domain.ErrMergeNotConfigured, prompt.Text(mergeNotConfiguredKey)))
	}
	token := s.token(ctx)
	if token == "" {
		return domain.TaskPRMergeResult{}, s.refuse(task, fmt.Errorf(
			"%w — %s", domain.ErrMergeNotConfigured, prompt.Text(mergeGitHubNotConnectedKey)))
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
			"merge refused: %s: %w", prompt.Text(mergeOwnerRepoUnresolvedKey), err))
	}
	pr, err := s.prs.GetPullRequest(ctx, token, owner, repo, number)
	if err != nil {
		return domain.TaskPRMergeResult{}, fmt.Errorf("read pull request #%d before merging it: %w", number, err)
	}

	if pr.Merged {
		s.recordMergeCommit(ctx, taskID, pr.HeadSHA, "")
		return domain.TaskPRMergeResult{}, s.refuse(task, fmt.Errorf(
			"%w — %s", domain.ErrMergeAlreadyMerged,
			mergeAlreadyMergedOnGitHubKey.Render(mergeNumberInput{Number: number})))
	}
	if strings.EqualFold(pr.State, "closed") {
		return domain.TaskPRMergeResult{}, s.refuse(task, fmt.Errorf(
			"%w — %s", domain.ErrMergeClosed,
			mergeClosedKey.Render(mergeNumberInput{Number: number})))
	}
	if pr.Draft {
		log.Warn().Str("task_id", taskID.String()).Int("pull_request", number).
			Msg("merge: pull request is a legacy draft, so GitHub's mergeable state cannot be judged before un-drafting it")
	}
	var preexistingNote string
	state := strings.ToLower(strings.TrimSpace(pr.MergeableState))
	if !pr.Draft && !mergeableStates[state] {
		proceed := false
		if state == "unstable" || state == "blocked" {
			if headFailing, baseFailing, baseRef, baseSHA, ok := s.preexistingBaseFailure(ctx, owner, repo, pr, token); ok && preexistingSubset(headFailing, baseFailing) {
				switch state {
				case "blocked":
					return domain.TaskPRMergeResult{}, s.refuse(task, fmt.Errorf(
						"%w — %s", domain.ErrMergeBaseRed,
						mergeBaseRedKey.Render(mergeBaseRedInput{
							BaseRef: baseRef, BaseSHA: domain.ShortSHA(baseSHA), FailingChecks: strings.Join(headFailing, ", "),
						})))
				case "unstable":
					preexistingNote = mergePreexistingNoteKey.Render(mergePreexistingNoteInput{
						FailingChecks: strings.Join(headFailing, ", "), BaseRef: baseRef, BaseSHA: domain.ShortSHA(baseSHA),
					})
					log.Info().Str("task_id", taskID.String()).Int("pull_request", number).
						Str("base_ref", baseRef).Str("base_sha", baseSHA).Strs("failing_checks", headFailing).
						Msg(preexistingNote)
					proceed = true
				}
			}
		}
		if !proceed {
			return domain.TaskPRMergeResult{}, s.refuse(task, fmt.Errorf(
				"%w — %s", domain.ErrMergeChecksNotGreen,
				mergeChecksNotGreenKey.Render(mergeChecksNotGreenInput{
					Number: number, State: pr.MergeableState, Remedy: mergeableStateRemedy(pr.MergeableState),
				})))
		}
	}

	switch err := domain.VerifiedCommitMatches(task.VerifiedSHA, pr.HeadSHA); {
	case errors.Is(err, domain.ErrReleaseTargetUnverified):
		return domain.TaskPRMergeResult{}, s.refuse(task, fmt.Errorf(
			"%w: %s", domain.ErrReleaseTargetUnverified,
			mergeTargetUnverifiedKey.Render(mergeTargetUnverifiedInput{SHA: domain.ShortSHA(pr.HeadSHA)})))
	case errors.Is(err, domain.ErrReleaseTargetMoved):
		return domain.TaskPRMergeResult{}, s.refuse(task, fmt.Errorf(
			"%w: %s", domain.ErrReleaseTargetMoved,
			mergeTargetMovedKey.Render(mergeTargetMovedInput{
				VerifiedSHA: domain.ShortSHA(task.VerifiedSHA), HeadSHA: domain.ShortSHA(pr.HeadSHA),
			})))
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
		Merged:                true,
		PRNumber:              number,
		PRURL:                 prURL,
		MergeCommitSHA:        merge.MergeCommitSHA,
		Branch:                pr.HeadRef,
		BaseBranch:            pr.BaseRef,
		BranchDeleted:         merge.BranchDeleted,
		Undrafted:             merge.Undrafted,
		PreexistingChecksNote: preexistingNote,
	}
	if s.rootSyncer != nil {
		s.rootSyncer.SyncAfterMerge(ctx, repositoryID)
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
		"%w — %s", domain.ErrMergeChecksNotGreen,
		mergePipelineFailedKey.Render(mergePipelineFailedInput{Trigger: string(pipeline.Trigger), Detail: detail}))
}

// preexistingBaseFailure reads which checks are red on the pull request's
// head and on its base branch's current head, so an `unstable`/`blocked`
// mergeable state can be told apart from a check this pull request actually
// broke. ok is false whenever the comparison cannot be made — no
// PreMergeChecksReader wired up, no base ref, or either read failing — and
// the caller then falls back to refusing outright.
func (s *TaskPRService) preexistingBaseFailure(ctx context.Context, owner, repo string, pr port.PullRequest, token string) (headFailing, baseFailing []string, baseRef, baseSHA string, ok bool) {
	checker, supported := s.prs.(port.PreMergeChecksReader)
	if !supported {
		return nil, nil, "", "", false
	}
	baseRef = strings.TrimSpace(pr.BaseRef)
	if baseRef == "" {
		return nil, nil, "", "", false
	}
	sha, err := checker.BranchHeadSHA(ctx, token, owner, repo, baseRef)
	if err != nil || strings.TrimSpace(sha) == "" {
		return nil, nil, "", "", false
	}
	baseSHA = sha
	baseFailing, err = checker.FailingChecks(ctx, token, owner, repo, baseSHA)
	if err != nil {
		return nil, nil, "", "", false
	}
	headFailing, err = checker.FailingChecks(ctx, token, owner, repo, pr.HeadSHA)
	if err != nil {
		return nil, nil, "", "", false
	}
	return headFailing, baseFailing, baseRef, baseSHA, true
}

// preexistingSubset reports whether every one of head's failing checks also
// fails on base — the criterion for "pre-existing", not "introduced by this
// pull request". A PR with no failing checks of its own is never pre-existing
// (there is nothing to explain away).
func preexistingSubset(head, base []string) bool {
	if len(head) == 0 {
		return false
	}
	inBase := make(map[string]bool, len(base))
	for _, b := range base {
		inBase[b] = true
	}
	for _, h := range head {
		if !inBase[h] {
			return false
		}
	}
	return true
}

func mergeableStateRemedy(state string) string {
	return mergeStateRemedyKey.Render(mergeStateRemedyInput{State: strings.ToLower(strings.TrimSpace(state))})
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
	releaseNext := ""
	if out.Release != nil {
		releaseNext = out.Release.Next
	}
	recordErrText := ""
	if recordErr != nil {
		recordErrText = recordErr.Error()
	}
	return mergeMessageKey.Render(mergeMessageInput{
		PRNumber:        out.PRNumber,
		BaseBranch:      fallback(out.BaseBranch, "the base branch"),
		MergeCommitSHA:  domain.ShortSHA(out.MergeCommitSHA),
		Undrafted:       out.Undrafted,
		PreexistingNote: out.PreexistingChecksNote,
		ReleaseNext:     releaseNext,
		AutoReleased:    out.AutoReleased,
		BranchDeleted:   out.BranchDeleted,
		Branch:          out.Branch,
		BranchDeleteErr: branchDeleteErr,
		RecordErr:       recordErrText,
	})
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
