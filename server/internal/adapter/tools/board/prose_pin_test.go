package board

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// TestBoardProsePinnedByteIdentical pins every string this package's
// Execute() calls used to return as a Go literal, now rendered from
// catalog/system/guards/board_*.md and catalog/system/prompts/tool_results/
// board_*.md — see WP14a. Each case renders the catalog key with the same
// data the call site passes it and compares against the exact original
// wording, so a catalog edit that silently changes the bytes an agent reads
// fails here first.
func TestBoardProsePinnedByteIdentical(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{
			"claim_already_assigned",
			claimAlreadyAssignedKey.Render(refInput{Ref: "T-9"}),
			"task T-9 is already assigned to another agent; only unassigned tasks or tasks assigned to you can be claimed",
		},
		{
			"claim_task_not_found",
			claimTaskNotFoundKey.Render(refInput{Ref: "T-9"}),
			"task T-9 was not found in this repository; list_board_tasks shows the tasks that exist here",
		},
		{
			"unknown_task_ref",
			unknownTaskRefKey.Render(refInput{Ref: "T-9"}),
			`unknown task "T-9"; pass the task UUID or its board key (e.g. T-1, B-1, A-1) — list_board_tasks shows both`,
		},
		{
			"unknown_project_ref_empty",
			unknownProjectRefEmptyKey.Render(refInput{Ref: "acme"}),
			`unknown project "acme"; no projects exist yet — create one with create_project`,
		},
		{
			"invalid_criterion_id",
			invalidCriterionIDKey.Render(rawIDInput{RawID: "nope"}),
			`invalid criterion_id "nope": expected a UUID from list_acceptance_criteria`,
		},
		{
			"criterion_not_found_stale",
			criterionNotFoundStaleKey.Render(rawIDInput{RawID: "c-1"}),
			"criterion c-1 was not found — acceptance criteria were replaced since you last listed them (their ids changed); call list_acceptance_criteria to get the current ids and retry",
		},
		{
			"cross_repository_task",
			crossRepositoryTaskKey.Render(crossRepositoryTaskInput{TaskID: "T-1", RepositoryID: "R-1"}),
			"board task T-1 is not in repository R-1, which this run is bound to; cross-repository actions are refused — leave a comment on your own task naming the other task instead",
		},
		{
			"document_edit_not_found",
			documentEditNotFoundKey.Render(indexInput{Index: 2}),
			"edits[2]: old_text not found in the document as edited so far; nothing was saved — re-read the source with list_task_documents raw: true and copy the passage exactly",
		},
		{
			"document_edit_ambiguous",
			documentEditAmbiguousKey.Render(indexCountInput{Index: 0, Count: 3}),
			"edits[0]: old_text occurs 3 times — include more surrounding text so it matches exactly once; nothing was saved",
		},
		{
			"document_none_yet",
			documentNoneYetKey.Render(struct{}{}),
			"this task has no documents yet — use add_task_document to write the first one",
		},
		{
			"test_case_invalid_criterion_id",
			testCaseInvalidCriterionIDKey.Render(testCaseInvalidCriterionIDInput{Title: "Login works", RawID: "nope"}),
			`case "Login works" has an invalid criterion_id "nope"; use an id from list_acceptance_criteria, or leave it empty`,
		},
		{
			"release_no_release_hint",
			releaseNoReleaseHintKey.Render(struct{}{}),
			" — nothing has opened a release for this task: it may not have merged yet, its component's delivery mode may be none/batch (nothing to watch), or its delivery profile is not confirmed. get_task_pull_request or the card's release field says which.",
		},
		{
			"deploy_release_refused",
			deployReleaseRefusedKey.Render(struct{}{}),
			"\n\nNothing was deployed. Do not retry deploy_release — it will refuse again until the release's status or delivery mode changes.",
		},
		{
			"finish_release_refused",
			finishReleaseRefusedKey.Render(struct{}{}),
			"\n\nNothing was finished. Do not retry finish_release as an agent — it refuses again until the release reaches awaiting_verdict, or a human overrides a failed one.",
		},
		{
			"rollback_release_refused",
			rollbackReleaseRefusedKey.Render(struct{}{}),
			"\n\nNothing was rolled back. Do not retry rollback_release — it will refuse again until the release's status changes.",
		},
		{
			"merge_task_refused",
			mergeTaskRefusedKey.Render(struct{}{}),
			"\n\nNothing was merged. Do not retry merge_task_pull_request — it will refuse again until the state above changes. Report this on the task instead.",
		},
		{
			"ungrounded_analysis_grounding",
			ungroundedAnalysisGroundingKey.Render(ungroundedAnalysisGroundingInput{Tools: []string{"a", "b"}}),
			"An analiz result must be based on the repository, and this run has not read it yet: no a, b call has succeeded. Explore the code first (get_repo_tree for structure, codebase_search for concepts, grep_code for exact symbols, expand_symbol_context to read the parts that matter), then write the analysis naming the real files and interfaces you found.",
		},
		{
			"document_rewritten_in_place",
			documentRewrittenInPlaceKey.Render(struct{}{}),
			"A document with this title was already on the task, so it was rewritten in place instead of duplicated. Use update_task_document for revisions.",
		},
		{
			"duplicate_task_hint",
			duplicateTaskHintKey.Render(struct{}{}),
			"Use this task instead of creating another: move_board_task / update_board_task / add_task_comment. Pass allow_duplicate=true only if it is genuinely different work.",
		},
		{
			"move_verdict_held",
			moveVerdictHeldKey.Render(moveVerdictInput{Column: "code_review", Requested: "ready_for_qa"}),
			"Your verdict is recorded, but the card stays in `code_review`: it moves to `ready_for_qa` only when every required reviewer has decided, or when a person signs it off. Your review is complete — do not move the card again and do not re-review it; end the run.",
		},
		{
			"move_verdict_redirected",
			moveVerdictRedirectedKey.Render(moveVerdictInput{Column: "need_revision", Requested: "ready_for_qa"}),
			"Your verdict is recorded, but another required reviewer asked for changes, so the card went to `need_revision` instead of `ready_for_qa` with every reviewer's comments on it. Your review is complete — do not move the card again; end the run.",
		},
		{
			"criteria_dropped_hint",
			criteriaDroppedHintKey.Render(countInput{Count: 2}),
			"2 acceptance criterion/criteria were board actions (moving the card, opening the next tasks, attaching things, hand-offs) and were not saved. Acceptance criteria describe what the finished work IS — the content of the spec, the behaviour of the endpoint, the state of the screen — never the board steps around it. Re-send them as observable statements about the deliverable, or leave them out.",
		},
		{
			"delete_task_blocked_hint",
			deleteTaskBlockedHintKey.Render(struct{}{}),
			"Work has already started on this task, and deleting it would erase its branch history, comments and criteria. Move it to a terminal column with move_board_task instead, or call this again with force=true if the user specifically asked for THIS task to be deleted.",
		},
		{
			"deploy_logs_no_failed_job",
			deployLogsNoFailedJobKey.Render(stateInput{State: "connected"}),
			"this task has no failing GitHub Actions job to read: its release (if it has one) names none, and the legacy deploy watch reports connected. If the repository deploys on push there is no CI log at all — try source=logs_url, or read the provider's own link in get_release.",
		},
		{
			"local_preview_failed",
			localPreviewFailedKey.Render(struct{}{}),
			"The preview failed to start — report the detail and log tail on the task; do not approve without executing.",
		},
		{
			"local_preview_ready",
			localPreviewReadyKey.Render(struct{}{}),
			"Open url with browser_navigate; this is the task branch running locally, not production.",
		},
		{
			"local_preview_pending",
			localPreviewPendingKey.Render(struct{}{}),
			"No URL yet — call start_task_preview again; it will not restart a preview that is already starting.",
		},
		{
			"pipeline_skipped_hint",
			pipelineSkippedHintKey.Render(struct{}{}),
			"No CI checks are configured for this repository, so nothing was built or tested. The task was allowed through the gate, but this run is NOT evidence that the code compiles or passes tests — verify the work yourself.",
		},
		{
			"task_preview_none",
			taskPreviewNoneKey.Render(struct{}{}),
			"No component of this repository has a per-branch preview environment (a `preview` environment bound to a Vercel project). Test locally or on stage.",
		},
		{
			"task_preview_not_built",
			taskPreviewNotBuiltKey.Render(branchInput{Branch: "feature/x"}),
			`Vercel has no deployment of branch "feature/x" yet; it builds one when the branch is pushed. Call again later, or test locally.`,
		},
		{
			"task_preview_building",
			taskPreviewBuildingKey.Render(struct{}{}),
			"Still building: call get_task_preview again before testing on it.",
		},
		{
			"task_preview_failed",
			taskPreviewFailedKey.Render(struct{}{}),
			"This build did not finish; inspect_url shows why. Do not test on it.",
		},
		{
			"task_preview_stale_build",
			taskPreviewStaleBuildKey.Render(staleBuildInput{Built: "abc123", Head: "def456"}),
			"Built from abc123, not the pull request head def456: the head's build has not appeared yet.",
		},
		{
			"task_preview_protected",
			taskPreviewProtectedKey.Render(struct{}{}),
			"This preview is behind Vercel Deployment Protection and the project has no Protection Bypass for Automation, so every automated request gets a login page. Ask the human to create one in the Vercel project (Settings → Deployment Protection → Protection Bypass for Automation), then call get_task_preview again.",
		},
		{
			"watch_release_parked",
			watchReleaseParkedKey.Render(watchReleaseParkedInput{Version: "v3", Mode: "continuous", Executor: "github_actions", Status: "deploying"}),
			"Release v3 (continuous/github_actions) is still deploying. This task is parked until it settles and will be picked up again then — nothing further to do in this run.",
		},
		{"release_next_draft", releaseNextDraftKey.Render(struct{}{}), "Nothing to do — a human cuts this release when they are ready."},
		{
			"release_next_awaiting_verdict_batch",
			releaseNextAwaitingVerdictBatchKey.Render(struct{}{}),
			"Read get_release for the build/publish evidence (workflow run, local_run, or store_builds) and any smoke checks, then call finish_release or rollback_release. Read query_runtime_logs and list_runtime_errors too when the component has a bound runtime environment (pass its component); when it does not, say so explicitly in the finish note instead of treating the gap as a pass.",
		},
		{
			"release_next_awaiting_verdict",
			releaseNextAwaitingVerdictKey.Render(struct{}{}),
			"Read query_runtime_logs and list_runtime_errors — pass the release's component, and read errors from well before deployed_at, judging a group by its own first_seen rather than by `new` alone — then call finish_release or rollback_release.",
		},
		{
			"release_next_failed_batch",
			releaseNextFailedBatchKey.Render(struct{}{}),
			"Read get_release: for a local run, its local_run.tail (and local_run.log_path); for github_actions, call get_deploy_logs. Then call rollback_release if the bad code is live or sitting on the default branch — or, if nothing can be done from here, report that on the task.",
		},
		{
			"release_next_failed",
			releaseNextFailedKey.Render(struct{}{}),
			"Read get_deploy_logs if there is a failed job, then call rollback_release — or, if nothing can be done from here, report that on the task.",
		},
		{"release_next_pending_batch", releaseNextPendingBatchKey.Render(struct{}{}), "A human has cut this release — call deploy_release."},
		{"release_next_pending", releaseNextPendingKey.Render(struct{}{}), "Call deploy_release."},
		{"release_next_watch", releaseNextWatchKey.Render(struct{}{}), "Call watch_release."},
		{"release_next_done", releaseNextDoneKey.Render(struct{}{}), "Nothing to do."},
		{"release_next_unknown", releaseNextUnknownKey.Render(releaseNextUnknownInput{Status: "weird"}), "No instruction for status weird — report it as-is."},
		{"deploy_release_next", deployReleaseNextKey.Render(struct{}{}), "call watch_release"},
		{"finish_release_done", finishReleaseDoneKey.Render(struct{}{}), "Nothing to do — the task(s) are released."},
		{"rollback_release_next", rollbackReleaseNextKey.Render(struct{}{}), "Perform or report EVERY manual step, then call watch_release."},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: rendered =\n%q\nwant =\n%q", c.name, c.got, c.want)
		}
	}
}

// TestReleaseNextStepDispatch pins releaseNextStep's per-status/mode
// dispatch — the Go switch itself, not just the catalog text each branch
// renders (covered above).
func TestReleaseNextStepDispatch(t *testing.T) {
	cases := []struct {
		status domain.ReleaseStatus
		mode   domain.DeliveryMode
		want   string
	}{
		{domain.ReleaseDraft, "", "Nothing to do — a human cuts this release when they are ready."},
		{domain.ReleaseAwaitingVerdict, domain.DeliveryBatch, releaseNextAwaitingVerdictBatchKey.Render(struct{}{})},
		{domain.ReleaseAwaitingVerdict, "", releaseNextAwaitingVerdictKey.Render(struct{}{})},
		{domain.ReleaseFailed, domain.DeliveryBatch, releaseNextFailedBatchKey.Render(struct{}{})},
		{domain.ReleaseFailed, "", releaseNextFailedKey.Render(struct{}{})},
		{domain.ReleasePending, domain.DeliveryBatch, releaseNextPendingBatchKey.Render(struct{}{})},
		{domain.ReleasePending, "", releaseNextPendingKey.Render(struct{}{})},
		{domain.ReleaseDeploying, "", "Call watch_release."},
		{domain.ReleaseVerifying, "", "Call watch_release."},
		{domain.ReleaseRollingBack, "", "Call watch_release."},
		{domain.ReleaseReleased, "", "Nothing to do."},
		{domain.ReleaseRolledBack, "", "Nothing to do."},
		{domain.ReleaseSuperseded, "", "Nothing to do."},
		{domain.ReleaseStatus("weird"), "", "No instruction for status weird — report it as-is."},
	}
	for _, c := range cases {
		got := releaseNextStep(domain.Release{Status: c.status, Mode: c.mode})
		if got != c.want {
			t.Errorf("releaseNextStep(%s/%s) = %q, want %q", c.status, c.mode, got, c.want)
		}
	}
}

// TestClaimRefusalPinned pins claimRefusal's two named cases (the third,
// default, is just err.Error() passthrough — nothing to pin).
func TestClaimRefusalPinned(t *testing.T) {
	if got := claimRefusal("T-3", domain.ErrTaskAlreadyClaimed); got != claimAlreadyAssignedKey.Render(refInput{Ref: "T-3"}) {
		t.Errorf("claimRefusal(already claimed) = %q", got)
	}
	if got := claimRefusal("T-3", domain.ErrBoardTaskNotFound); got != claimTaskNotFoundKey.Render(refInput{Ref: "T-3"}) {
		t.Errorf("claimRefusal(not found) = %q", got)
	}
}

// TestCrossRepositoryTaskMessageWraps pins that the message still carries
// domain.ErrTaskOutsideRepository via %w after the move (errors.Is must keep
// working for callers that branch on it).
func TestCrossRepositoryTaskMessageWraps(t *testing.T) {
	err := errors.New(crossRepositoryTaskMessage(uuid.MustParse("11111111-1111-1111-1111-111111111111"), uuid.MustParse("22222222-2222-2222-2222-222222222222")))
	if err.Error() != "board task 11111111-1111-1111-1111-111111111111 is not in repository 22222222-2222-2222-2222-222222222222, which this run is bound to; cross-repository actions are refused — leave a comment on your own task naming the other task instead" {
		t.Errorf("crossRepositoryTaskMessage = %q", err.Error())
	}
}
