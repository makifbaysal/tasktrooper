package board

// Merging a task's pull request: the last thing that happens to a task's code
// before a deploy, and the only board action that cannot be taken back.
//
// It lives beside the other task↔PR use cases (taskpr.go) because it needs the
// same four things resolved first — the task, its PR number, the repository's
// GitHub coordinates, a token — and resolving them a second way is how two
// paths end up disagreeing about which PR a task is in.
//
// The shape is deliberately gate-heavy. Every refusal below is a case that was
// either observed or is one push away from being observed, and each one is
// named, logged and returned as its own sentinel, so an agent reading the tool
// result is told which rule stopped it rather than "merge failed".

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

// mergeableStates are the GitHub mergeable_state values a merge may proceed on.
//
// GitHub's own summary of "can this be merged right now" is the only signal
// that sees the whole picture — required status checks, branch protection,
// conflicts with the base — and it sees them at the repository's settings, not
// at ours. The two accepted values:
//
//	clean     — mergeable, every required check green, nothing blocking.
//	has_hooks — same as clean on a repository with pre-receive hooks.
//
// Everything else refuses, including the ones that are arguably survivable:
//
//	blocked   — a required check is red or still running, or a review is
//	            missing. This is THE red-checks case.
//	unstable  — a NON-required check is failing. Strictly the required checks
//	            are green, and GitHub would merge it. We do not: a red check on
//	            the PR at the moment it lands is exactly what a human would stop
//	            for, and "it was only the optional job" is a judgement for a
//	            person, not for an agent with a merge button.
//	dirty     — conflicts with the base.
//	behind    — the base moved and the repository requires up-to-date branches.
//	draft     — GitHub refuses drafts outright (we un-draft before this check).
//	unknown   — GitHub has not computed the mergeability yet. Fails closed:
//	            "not computed" is not "fine", and the retry is free.
var mergeableStates = map[string]bool{
	"clean":     true,
	"has_hooks": true,
}

// MergeTaskPullRequest merges the task's pull request with a squash commit and
// deletes its branch.
//
// The order of the refusals is the order of how expensive they are to be wrong
// about, cheapest first: board state, then the recorded PR, then GitHub's view
// of the PR, then the identity check, and only then the merge. Nothing here
// retries: every refusal is a state that a retry cannot change, and an agent
// that reads a refusal as "try again" would spend its whole run on it.
func (s *TaskPRService) MergeTaskPullRequest(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.TaskPRMergeResult, error) {
	task, err := s.tasks.Get(ctx, repositoryID, taskID)
	if err != nil {
		return domain.TaskPRMergeResult{}, err
	}

	// 1. The column. Merging IS the done column's action, and a task anywhere
	//    else has a PR precisely because its change is still being judged.
	if task.Column != domain.TaskColumnDone {
		return domain.TaskPRMergeResult{}, s.refuse(task, fmt.Errorf(
			"%w — %s is in `%s`. A pull request is merged when the board has signed the task off, not while it is still being reviewed or tested",
			domain.ErrMergeTaskNotDone, taskLabel(task), task.Column))
	}

	// 2. Our own record of an earlier merge. Cheaper than asking GitHub, and it
	//    is also what the dispatcher reads — if these two ever disagreed, the
	//    done column would either loop or go quiet.
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

	// 3. The review chain, re-asked. `done` asserts it, but a task can be
	//    dragged into done by hand, and require_review_chain is the repository
	//    owner's statement that nothing lands without every stage. Asking the
	//    repository service means there is ONE definition of the chain, the
	//    same one that guards the column.
	if err := s.gates.CheckReviewChain(ctx, repositoryID, taskID); err != nil {
		return domain.TaskPRMergeResult{}, s.refuse(task, fmt.Errorf(
			"merge refused: %w", err))
	}

	// 4. The board's own build/test result for the task. GitHub's mergeable
	//    state below covers the checks the REPOSITORY marks required; this
	//    covers the pipeline the BOARD ran for this task, which on a repo with
	//    no required checks configured is the only evidence that exists.
	if err := s.pipelineIsGreen(ctx, repositoryID, taskID); err != nil {
		return domain.TaskPRMergeResult{}, s.refuse(task, err)
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

	// 5. What GitHub says the PR is.
	if pr.Merged {
		// Merged on GitHub but not recorded here: someone merged it by hand, or
		// a previous run merged and died before recording. Record it now — the
		// dispatcher's "does this still need merging" question is answered by
		// our column, and leaving it empty would wake QA on this task forever.
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
	// A draft PR reports mergeable_state "draft" whatever its checks say, so
	// there is nothing to judge here for one — reading that as "checks not
	// green" would send the agent chasing a red build that does not exist, and
	// re-reading the state right after un-drafting would only get "unknown",
	// because GitHub computes it asynchronously. A legacy draft therefore rests
	// on the two checks that do apply: the board's own pipeline verdict above,
	// and GitHub's own refusal at merge time (405) when a required check is red
	// or the branch is protected. New PRs are never drafts (CreatePullRequest),
	// so this is the pre-existing backlog, not the normal path.
	if pr.Draft {
		log.Warn().Str("task_id", taskID.String()).Int("pull_request", number).
			Msg("merge: pull request is a legacy draft, so GitHub's mergeable state cannot be judged before un-drafting it")
	}
	if !pr.Draft && !mergeableStates[strings.ToLower(strings.TrimSpace(pr.MergeableState))] {
		return domain.TaskPRMergeResult{}, s.refuse(task, fmt.Errorf(
			"%w — GitHub reports pull request #%d as `%s` (expected `clean`). %s",
			domain.ErrMergeChecksNotGreen, number, pr.MergeableState, mergeableStateRemedy(pr.MergeableState)))
	}

	// 6. Identity. The commit GitHub would merge against the commit the board
	//    signed off at — the same comparison the release gate makes before a
	//    prod deploy (domain.VerifiedCommitMatches), asked here of the PR head
	//    rather than of the workspace, because the PR head is what actually
	//    lands.
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

	// Everything above passed. From here the change lands.
	log.Info().Str("task_id", taskID.String()).Str("owner", owner).Str("repo", repo).
		Int("pull_request", number).Str("head_sha", pr.HeadSHA).Bool("draft", pr.Draft).
		Msg("merging task pull request (squash)")

	merge, err := s.git.MergePullRequest(ctx, domain.PullRequestMergeRequest{
		Owner:  owner,
		Repo:   repo,
		Number: number,
		Branch: pr.HeadRef,
		// The SHA the gate above verified, not a fresh read: a precondition
		// re-read immediately before the call would only prove that nothing was
		// pushed in the last millisecond.
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
	if s.gates != nil {
		out.AutoReleased = s.gates.AutoReleaseIfUndeployable(ctx, repositoryID, taskID)
	}
	out.Message = mergeMessage(out, merge.BranchDeleteError, recordErr)
	log.Info().Str("task_id", taskID.String()).Int("pull_request", number).
		Str("merge_commit", merge.MergeCommitSHA).Bool("branch_deleted", merge.BranchDeleted).
		Msg("task pull request merged")
	return out, nil
}

// pipelineIsGreen refuses on the board's own last build/test verdict for the
// task.
//
// Only a FAILED pipeline blocks. "No pipeline" and "skipped" are the states of a
// repository with no CI wired up at all, where refusing would make the merge
// unreachable for every such install; GitHub's mergeable state is then the only
// check, which is the same trade the QA gate already makes. A pipeline the
// ledger cannot be read from does NOT block either — it is a control-plane
// failure, and GitHub's required checks (the ones a repository owner actually
// enforces) still stand between us and a bad merge.
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

// mergeableStateRemedy turns GitHub's one-word state into the next action, so
// the agent reports what has to happen instead of re-reading the PR in a loop.
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

// recordMergeCommit writes the merge onto the task. Its failure is reported to
// the caller but never turns a completed merge into an error: the commit is on
// the default branch either way, and reporting failure would have the agent
// merge again (finding the PR merged, refusing) or, worse, believe nothing
// happened. The board consequence of losing it is that the done column keeps
// waking QA for this task, which the message says out loud.
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

// refuse logs a refusal and hands it back unchanged.
//
// Every path that declines to merge goes through here, so "why did this not
// merge?" is one grep rather than a reading of the control flow. It does not
// comment on the task: the refusal is returned to the agent that asked, which
// reports it in its own words — unlike the release gate, whose refusals happen
// during a board move nobody is watching.
func (s *TaskPRService) refuse(task domain.BoardTask, err error) error {
	log.Warn().Err(err).Str("task_id", task.ID.String()).Str("task_key", task.Key).
		Str("column", string(task.Column)).Msg("task pull request merge refused")
	return err
}

// mergeCommitTitle is the squash commit's subject: the PR's own title, plus
// the PR number GitHub would otherwise append itself.
//
// The PR title is preferred over the task's raw title because it is the
// Conventional Commits subject writeCommitMessage already produced in
// English — task.Title can be in whatever language the board's tasks are
// titled in, and prefixing the task key onto it (as this used to do) breaks
// that same convention on the branch commit one step earlier. The task key
// still reaches main, in mergeCommitBody.
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

// mergeCommitBody keeps the trail from the default branch back to the card. A
// squash throws the branch's own history away, so this is the only place the
// task and its PR are named in the merged history.
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

// mergeMessage is the sentence the agent repeats to the board. It never hides a
// partial failure behind the success, and it never reports the merge itself as
// anything but done.
func mergeMessage(out domain.TaskPRMergeResult, branchDeleteErr string, recordErr error) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Merged pull request #%d into %s as %s (squash).",
		out.PRNumber, fallback(out.BaseBranch, "the base branch"), domain.ShortSHA(out.MergeCommitSHA))
	if out.Undrafted {
		sb.WriteString(" The PR was still a draft and was marked ready for review first.")
	}
	if out.AutoReleased {
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

// taskLabel names a task the way a human reads it on the board, falling back to
// the id for a row with no key.
func taskLabel(task domain.BoardTask) string {
	if strings.TrimSpace(task.Key) != "" {
		return task.Key
	}
	return task.ID.String()
}
