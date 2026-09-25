package board

// merge_task_pull_request: the tool that lands a task's change.
//
// It is a tool rather than a hook on the done column on purpose. A server-side
// automatic merge would fire on a state change nobody was looking at, at a
// moment nothing had checked whether the checks were still green. So the
// release engineer is woken when a task reaches done, reads the PR, and
// decides.
//
// Everything this file does is argument handling and reporting. Every refusal
// lives in application/board.TaskPRService.MergeTaskPullRequest, because the
// gates have to hold for any caller, not only for one whose model was in the
// mood to check.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// The name is domain's, not this file's: the policy layer withholds this tool
// by name in two places (verdict columns, the workspace uplift), and a literal
// that only matched in one of them would silently hand the merge to a run that
// must not have it.
const mergeTaskPullRequestToolName = domain.MergePullRequestToolName

type mergeTaskPullRequestTool struct {
	kit *ToolKit
}

func newMergeTaskPullRequestTool(kit *ToolKit) port.ToolExecutor {
	return &mergeTaskPullRequestTool{kit: kit}
}

func (t *mergeTaskPullRequestTool) Name() string { return mergeTaskPullRequestToolName }

func (t *mergeTaskPullRequestTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: mergeTaskPullRequestToolName,
			Description: "Merge the task's pull request into its base branch with a SQUASH commit and delete the task branch. " +
				"This is how a finished task's code actually lands, and it is IRREVERSIBLE — call it only for a task in the `done` column whose checks you have read and found green (get_task_pull_request, get_pipeline_status). " +
				"It refuses, without merging anything, when: the task is not in `done`; the PR is already merged or was closed unmerged; the checks are not green (GitHub reports anything but a clean mergeable state, or the task's last pipeline failed); the repository requires the full review chain and a stage is missing; the PR's head commit is no longer the commit the task was verified at — which means someone pushed after sign-off and the change must go back through review; or, on an `on_merge` component, the task carries before-deploy steps a human has not confirmed yet (the merge IS the deploy for this mode) — the system comments the steps on the task and you are woken once a human presses Confirm before-deploy steps, so do not chase it further. " +
				"Retrying a refusal changes nothing: act on what it said instead — a refusal naming a CONFLICT with the base branch (`dirty`) or an out-of-date branch (`behind`) is the developer's to resolve, so move the task to need_revision with that reason. " +
				"On success it records the merge commit on the task and the card shows it: do NOT write a comment saying the merge happened. " +
				"The result's `release` field says what the merge set in motion for the component, and what to do next — read it, do not guess: (the merge itself is refused — nothing merged — while the component's delivery profile is unconfirmed, while a deploy dependency is not released, and, for an on_merge component, while the task's before-deploy steps are unconfirmed; each refusal is already commented on the card and you are woken when it clears, so stop and do not retry) `unconfirmed: true` means the profile was unconfirmed for a task merged some other way — stop; otherwise mode `none` means the merge already released the task (nothing left to do); `on_merge` means the deploy is already running, call watch_release; `dispatch` means call deploy_release, then watch_release; `batch` means the merge joined the component's draft release (created if there was none) — the task waits in done and nothing to do now, a human cuts that release later and you are woken with a `pending` release to deploy_release/watch_release when they do.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"task_id": taskRefProperty,
				},
			},
		},
	}
}

func (t *mergeTaskPullRequestTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(mergeTaskPullRequestToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	if t.kit.PullRequests == nil {
		return toolError(mergeTaskPullRequestToolName, "merging pull requests is not configured on this deployment")
	}
	taskID, err := t.kit.resolveTaskArg(ctx, args.TaskID)
	if err != nil {
		return toolError(mergeTaskPullRequestToolName, err.Error())
	}
	repositoryID, err := t.kit.resolveTaskRepositoryID(ctx, taskID)
	if err != nil {
		return toolError(mergeTaskPullRequestToolName, err.Error())
	}

	result, err := t.kit.PullRequests.MergeTaskPullRequest(ctx, repositoryID, taskID)
	if err != nil {
		// A refusal is a tool ERROR and says so, but it also says not to retry:
		// the model's default response to an error is another attempt, and every
		// one of these blocks is a state that only a board action can change.
		// Same shape as deploy_release/rollback_release's identity blocks.
		if isMergeRefusal(err) {
			return toolError(mergeTaskPullRequestToolName, err.Error()+
				"\n\nNothing was merged. Do not retry merge_task_pull_request — it will refuse again until the state above changes. Report this on the task instead.")
		}
		return toolError(mergeTaskPullRequestToolName, err.Error())
	}

	// A clean merge writes NOTHING on the card. The merge commit is recorded on
	// the task itself and the board renders it, so a comment repeating the PR
	// number and the SHA is the third place the same fact appears — and a
	// comment thread that is mostly "everything went fine" is one nobody reads
	// when something does not.
	//
	// A merge that half-worked is the opposite case and still comments: a branch
	// that could not be deleted or a merge commit that could not be recorded are
	// both things somebody has to finish by hand, and the card is where they
	// find out. Best-effort — the merge already happened, and failing the tool
	// call over the note would tell the model the opposite.
	if t.kit.Tasks != nil && mergeNeedsAttention(result) {
		if _, cErr := t.kit.Tasks.AddComment(ctx, repositoryID, taskID, domain.CreateTaskCommentRequest{
			AuthorType: "system",
			Content:    result.Message,
		}); cErr != nil {
			log.Warn().Err(cErr).Str("task_id", taskID.String()).Msg("merge result comment failed")
		}
	}
	return toolJSON(mergeTaskPullRequestToolName, result)
}

// mergeNeedsAttention reports whether a completed merge left something for a
// person to do. Everything the merge did correctly is already on the task
// (merge_commit_sha) and on the board; only the leftovers are worth a comment.
func mergeNeedsAttention(result domain.TaskPRMergeResult) bool {
	return !result.BranchDeleted || strings.TrimSpace(result.MergeCommitSHA) == ""
}

// isMergeRefusal reports whether the error is a gate saying no, as opposed to
// GitHub or the network failing. Only the first kind is pointless to retry.
func isMergeRefusal(err error) bool {
	for _, sentinel := range []error{
		domain.ErrMergeTaskNotDone,
		domain.ErrMergeNoPullRequest,
		domain.ErrMergeAlreadyMerged,
		domain.ErrMergeClosed,
		domain.ErrMergeChecksNotGreen,
		domain.ErrMergeNotConfigured,
		domain.ErrReleaseTargetMoved,
		domain.ErrReleaseTargetUnverified,
		domain.ErrReviewChainIncomplete,
		domain.ErrReviewStageRejected,
	} {
		if errors.Is(err, sentinel) {
			return true
		}
	}
	return false
}
