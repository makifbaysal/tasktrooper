package board

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

// This file registers every LLM-facing string board's system comments
// render — see catalog/system/prompts/notices/**. runner.go's
// revisionCommentsMessage replays every non-"user" task comment (system
// comments included) into the next agent run's history, so these read back
// to a model exactly like any other prompt. Nothing here is a Go string
// literal; each Key's sample only has to be renderable, not realistic.

type unsettledCriteriaHeaderInput struct{ Count, Rounds int }

var unsettledCriteriaHeaderKey = prompt.Define("notices.unsettled_criteria_header", unsettledCriteriaHeaderInput{Count: 1, Rounds: 3})

var unsettledCriteriaFooterKey = prompt.Define("notices.unsettled_criteria_footer", struct{}{})

type unsettledCriteriaSummaryInput struct {
	Marker string
	Open   int
}

var unsettledCriteriaSummaryKey = prompt.Define("notices.unsettled_criteria_summary", unsettledCriteriaSummaryInput{Marker: "x", Open: 1})

var planVerificationIntroKey = prompt.Define("notices.plan_verification_intro", struct{}{})
var planVerificationIssuesHeaderKey = prompt.Define("notices.plan_verification_issues_header", struct{}{})
var planVerificationFooterKey = prompt.Define("notices.plan_verification_footer", struct{}{})

type verificationFailureCommentInput struct{ Report string }

var verificationFailureCommentKey = prompt.Define("notices.verification_failure_comment", verificationFailureCommentInput{Report: "exit status 1"})

var verificationFailureRevisionCommentKey = prompt.Define("notices.verification_failure_revision_comment", verificationFailureCommentInput{Report: "exit status 1"})

type verificationExhaustedNoteInput struct{ Attempts int }

var verificationExhaustedNoteKey = prompt.Define("notices.verification_exhausted_note", verificationExhaustedNoteInput{Attempts: 1})

type stuckVerdictNoteInput struct {
	Count int
	Role  string
	Texts string
}

var stuckVerdictNoteKey = prompt.Define("notices.stuck_verdict_note", stuckVerdictNoteInput{Count: 1, Role: "qa", Texts: "x"})

type stuckColumnCommentInput struct{ Column, Exit, Note string }

var stuckColumnCommentKey = prompt.Define("notices.stuck_column_comment", stuckColumnCommentInput{Column: "code_review", Exit: "ready_for_qa"})

type reviewNoPRReasonInput struct{ Cause string }

var reviewNoPRReasonKey = prompt.Define("notices.review_no_pr_reason", reviewNoPRReasonInput{Cause: "x"})

var reviewPassageLabelKey = prompt.Define("notices.review_passage_label", struct{}{})
var reviewCommentLabelKey = prompt.Define("notices.review_comment_label", struct{}{})

type postDeployNotesInput struct{ After string }

var postDeployNotesKey = prompt.Define("notices.post_deploy_notes", postDeployNotesInput{After: "x"})

type deploySkipCommentInput struct{ Note string }

var deploySkipCommentKey = prompt.Define("notices.deploy_skip_comment", deploySkipCommentInput{Note: "x"})

type pipelineCouldNotStartInput struct{ Note string }

var pipelineCouldNotStartKey = prompt.Define("notices.pipeline_could_not_start", pipelineCouldNotStartInput{Note: "x"})

type diffSkipSummaryInput struct{ Column, ShortID, ApprovedAt string }

var diffSkipSummaryKey = prompt.Define("notices.diff_skip_summary", diffSkipSummaryInput{Column: "code_review", ShortID: "x", ApprovedAt: "x"})

var rejectedDraftLabelKey = prompt.Define("notices.rejected_draft_label", struct{}{})
var rejectedRunReportReplanKey = prompt.Define("notices.rejected_run_report_replan", struct{}{})
var rejectedRunReportReapproveKey = prompt.Define("notices.rejected_run_report_reapprove", struct{}{})

var pipelineGateNoGreenBuildKey = prompt.Define("notices.pipeline_gate_no_green_build", struct{}{})

var workOrderParkPrefixKey = prompt.Define("notices.work_order_park_prefix", struct{}{})
var workOrderParkSuffixKey = prompt.Define("notices.work_order_park_suffix", struct{}{})
var workOrderResumedKey = prompt.Define("notices.work_order_resumed", struct{}{})

type reviewLoopParkCommentInput struct{ Entries int }

var reviewLoopParkCommentKey = prompt.Define("notices.review_loop_park_comment", reviewLoopParkCommentInput{Entries: 3})

type reviewLoopParkDetailInput struct{ Entries int }

var reviewLoopParkDetailKey = prompt.Define("notices.review_loop_park_detail", reviewLoopParkDetailInput{Entries: 3})

type criteriaLoopParkHeaderInput struct{ RunCount int }

var criteriaLoopParkHeaderKey = prompt.Define("notices.criteria_loop_park_header", criteriaLoopParkHeaderInput{RunCount: 3})

var stillOpenLabelKey = prompt.Define("notices.still_open_label", struct{}{})
var criteriaLoopParkFooterKey = prompt.Define("notices.criteria_loop_park_footer", struct{}{})

type criteriaLoopParkDetailInput struct{ RunCount int }

var criteriaLoopParkDetailKey = prompt.Define("notices.criteria_loop_park_detail", criteriaLoopParkDetailInput{RunCount: 3})

type pipelineBounceCommentInput struct{ SHA string }

var pipelineBounceCommentKey = prompt.Define("notices.pipeline_bounce_comment", pipelineBounceCommentInput{SHA: "abc1234"})

var pipelineBounceNoteLabelKey = prompt.Define("notices.pipeline_bounce_note_label", struct{}{})
