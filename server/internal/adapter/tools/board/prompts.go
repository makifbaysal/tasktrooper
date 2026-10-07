package board

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

// This file registers every LLM-facing string this package's Execute() calls
// render — see catalog/system/guards/board_*.md and
// catalog/system/prompts/tool_results/board_*.md. Nothing here is a Go string
// literal; each Key's sample only has to be renderable, not realistic.
// tooldocs.go (a different file) registers the tool description keys.

type refInput struct{ Ref string }

var claimAlreadyAssignedKey = prompt.Define("guard.board_claim_already_assigned", refInput{Ref: "T-1"})
var claimTaskNotFoundKey = prompt.Define("guard.board_claim_task_not_found", refInput{Ref: "T-1"})
var assigneeNotAssignableKey = prompt.Define("guard.board_assignee_not_assignable", struct{}{})
var unknownTaskRefKey = prompt.Define("guard.board_unknown_task_ref", refInput{Ref: "T-1"})
var unknownProjectRefEmptyKey = prompt.Define("guard.board_unknown_project_ref_empty", refInput{Ref: "x"})

type rawIDInput struct{ RawID string }

var invalidCriterionIDKey = prompt.Define("guard.board_invalid_criterion_id", rawIDInput{RawID: "x"})
var criterionNotFoundStaleKey = prompt.Define("guard.board_criterion_not_found_stale", rawIDInput{RawID: "x"})

type crossRepositoryTaskInput struct{ TaskID, RepositoryID string }

var crossRepositoryTaskKey = prompt.Define("guard.board_cross_repository_task", crossRepositoryTaskInput{TaskID: "x", RepositoryID: "y"})

type indexInput struct{ Index int }

var documentEditNotFoundKey = prompt.Define("guard.board_document_edit_not_found", indexInput{Index: 0})

type indexCountInput struct {
	Index, Count int
}

var documentEditAmbiguousKey = prompt.Define("guard.board_document_edit_ambiguous", indexCountInput{Index: 0, Count: 2})

var documentNoneYetKey = prompt.Define("guard.board_document_none_yet", struct{}{})

type testCaseInvalidCriterionIDInput struct{ Title, RawID string }

var testCaseInvalidCriterionIDKey = prompt.Define("guard.board_test_case_invalid_criterion_id", testCaseInvalidCriterionIDInput{Title: "x", RawID: "y"})

var releaseNoReleaseHintKey = prompt.Define("guard.board_release_no_release_hint", struct{}{})
var deployReleaseRefusedKey = prompt.Define("guard.board_deploy_release_refused", struct{}{})
var finishReleaseRefusedKey = prompt.Define("guard.board_finish_release_refused", struct{}{})
var rollbackReleaseRefusedKey = prompt.Define("guard.board_rollback_release_refused", struct{}{})
var mergeTaskRefusedKey = prompt.Define("guard.board_merge_task_refused", struct{}{})
var pushWorkflowScopeToolKey = prompt.Define("guard.board_push_workflow_scope", struct{}{})

type ungroundedAnalysisGroundingInput struct{ Tools []string }

var ungroundedAnalysisGroundingKey = prompt.Define("guard.board_ungrounded_analysis_grounding", ungroundedAnalysisGroundingInput{Tools: []string{"x"}})

var documentRewrittenInPlaceKey = prompt.Define("tool_results.board_document_rewritten_in_place", struct{}{})
var duplicateTaskHintKey = prompt.Define("tool_results.board_duplicate_task_hint", struct{}{})

type countInput struct{ Count int }

var criteriaDroppedHintKey = prompt.Define("tool_results.board_criteria_dropped_hint", countInput{Count: 1})

var deleteTaskBlockedHintKey = prompt.Define("tool_results.board_delete_task_blocked_hint", struct{}{})

type stateInput struct{ State string }

var deployLogsNoFailedJobKey = prompt.Define("tool_results.board_deploy_logs_no_failed_job", stateInput{State: "x"})

var localPreviewFailedKey = prompt.Define("tool_results.board_local_preview_failed", struct{}{})
var localPreviewReadyKey = prompt.Define("tool_results.board_local_preview_ready", struct{}{})
var localPreviewPendingKey = prompt.Define("tool_results.board_local_preview_pending", struct{}{})
var pipelineSkippedHintKey = prompt.Define("tool_results.board_pipeline_skipped_hint", struct{}{})
var taskPreviewNoneKey = prompt.Define("tool_results.board_task_preview_none", struct{}{})

type branchInput struct{ Branch string }

var taskPreviewNotBuiltKey = prompt.Define("tool_results.board_task_preview_not_built", branchInput{Branch: "x"})

var taskPreviewBuildingKey = prompt.Define("tool_results.board_task_preview_building", struct{}{})
var taskPreviewFailedKey = prompt.Define("tool_results.board_task_preview_failed", struct{}{})

type staleBuildInput struct{ Built, Head string }

var taskPreviewStaleBuildKey = prompt.Define("tool_results.board_task_preview_stale_build", staleBuildInput{Built: "a", Head: "b"})

var taskPreviewProtectedKey = prompt.Define("tool_results.board_task_preview_protected", struct{}{})

type watchReleaseParkedInput struct{ Version, Mode, Executor, Status string }

var watchReleaseParkedKey = prompt.Define("tool_results.board_watch_release_parked", watchReleaseParkedInput{
	Version: "v1", Mode: "m", Executor: "e", Status: "s",
})

var releaseNextDraftKey = prompt.Define("tool_results.board_release_next_draft", struct{}{})
var releaseNextAwaitingVerdictBatchKey = prompt.Define("tool_results.board_release_next_awaiting_verdict_batch", struct{}{})
var releaseNextAwaitingVerdictKey = prompt.Define("tool_results.board_release_next_awaiting_verdict", struct{}{})
var releaseNextFailedBatchKey = prompt.Define("tool_results.board_release_next_failed_batch", struct{}{})
var releaseNextFailedKey = prompt.Define("tool_results.board_release_next_failed", struct{}{})
var releaseNextPendingBatchKey = prompt.Define("tool_results.board_release_next_pending_batch", struct{}{})
var releaseNextPendingKey = prompt.Define("tool_results.board_release_next_pending", struct{}{})
var releaseNextWatchKey = prompt.Define("tool_results.board_release_next_watch", struct{}{})
var releaseNextDoneKey = prompt.Define("tool_results.board_release_next_done", struct{}{})

type releaseNextUnknownInput struct{ Status string }

var releaseNextUnknownKey = prompt.Define("tool_results.board_release_next_unknown", releaseNextUnknownInput{Status: "x"})

var deployReleaseNextKey = prompt.Define("tool_results.board_deploy_release_next", struct{}{})
var finishReleaseDoneKey = prompt.Define("tool_results.board_finish_release_done", struct{}{})
var rollbackReleaseNextKey = prompt.Define("tool_results.board_rollback_release_next", struct{}{})

var criterionRuleCardMoveKey = prompt.Define("guard.board_criterion_rule_card_move", struct{}{})
var criterionRuleCardWaitingKey = prompt.Define("guard.board_criterion_rule_card_waiting", struct{}{})
var criterionRuleNextTasksKey = prompt.Define("guard.board_criterion_rule_next_tasks", struct{}{})
var criterionRuleToolMechanicsKey = prompt.Define("guard.board_criterion_rule_tool_mechanics", struct{}{})
var criterionRuleHandoffKey = prompt.Define("guard.board_criterion_rule_handoff", struct{}{})
