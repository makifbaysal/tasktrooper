package board

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

// This file registers every LLM-facing string runner.go renders — see
// catalog/system/prompts/board/** and catalog/system/guards/**. Nothing here
// is a Go string literal; each Key's sample only has to be renderable, not
// realistic. board/prompts_context.go (a different file, owned separately)
// registers the keys for the rest of the board package.

var columnTodoKey = prompt.Define("board.column_todo", struct{}{})
var columnTodoAnalizKey = prompt.Define("board.column_todo_analiz", struct{}{})
var columnInProgressKey = prompt.Define("board.column_in_progress", struct{}{})
var columnInProgressAnalizKey = prompt.Define("board.column_in_progress_analiz", struct{}{})
var columnNeedRevisionKey = prompt.Define("board.column_need_revision", struct{}{})
var columnNeedRevisionAnalizKey = prompt.Define("board.column_need_revision_analiz", struct{}{})
var columnCodeReviewKey = prompt.Define("board.column_code_review", struct{}{})
var columnPMUATKey = prompt.Define("board.column_pm_uat", struct{}{})
var columnDoneKey = prompt.Define("board.column_done", struct{}{})
var columnDoneAnalizKey = prompt.Define("board.column_done_analiz", struct{}{})
var columnReleasedKey = prompt.Define("board.column_released", struct{}{})

type columnPassToInput struct{ PassTo string }

var columnReadyForQAKey = prompt.Define("board.column_ready_for_qa", columnPassToInput{PassTo: "pm_uat"})
var columnInQAKey = prompt.Define("board.column_in_qa", columnPassToInput{PassTo: "pm_uat"})

type columnDefaultInput struct{ Column string }

var columnDefaultKey = prompt.Define("board.column_default", columnDefaultInput{Column: "backlog"})

var columnSpecificNoteKey = prompt.Define("board.column_specific_note", struct{}{})

var reviewCriteriaHeaderQAPMKey = prompt.Define("board.review_criteria_header_qa_pm", struct{}{})
var reviewCriteriaHeaderPMUATKey = prompt.Define("board.review_criteria_header_pm_uat", struct{}{})
var reviewCriteriaHeaderCodeReviewKey = prompt.Define("board.review_criteria_header_code_review", struct{}{})
var reviewCriteriaHeaderDefaultKey = prompt.Define("board.review_criteria_header_default", struct{}{})

var criteriaOpenHeaderKey = prompt.Define("board.criteria_open_header", struct{}{})

type criterionOpenLineInput struct{ ID, Text string }

var criterionOpenLineKey = prompt.Define("board.criterion_open_line", criterionOpenLineInput{ID: "id", Text: "text"})

type criterionImplementerInput struct{ Completed bool }

var criterionImplementerLabelKey = prompt.Define("board.criterion_implementer_label", criterionImplementerInput{Completed: true})

type criterionLineInput struct{ ID, Text, Implementer, QA, PM string }

var criterionLineKey = prompt.Define("board.criterion_line", criterionLineInput{
	ID: "id", Text: "text", Implementer: "ticked", QA: "approved", PM: "—",
})

// criterionVerdictInput drives one role's verdict label on one criterion.
// Found is false when that role never checked it at all ("—"); Approved
// selects the "approved[, changed-since note]" branch; HasSHA/HasFiles/SHA/
// Files/More carry the changed-since note's own facts (files already
// truncated to the noted cap, More holding the overflow count) so the
// template stays pure presentation.
type criterionVerdictInput struct {
	Found, Approved  bool
	Note             string
	HasSHA, HasFiles bool
	SHA              string
	Files            []string
	More             int
}

var criterionVerdictKey = prompt.Define("board.criterion_verdict", criterionVerdictInput{Found: true, Approved: true})

type triggerMessageInput struct{ RunInstruction, ClosingStep, TaskJSON, CriteriaMessage string }

var triggerMessageKey = prompt.Define("board.trigger_message", triggerMessageInput{
	RunInstruction: "x", ClosingStep: "y", TaskJSON: "{}", CriteriaMessage: "",
})

var closingStepBuildVerifyKey = prompt.Define("board.closing_step_build_verify", struct{}{})
var closingStepDefaultKey = prompt.Define("board.closing_step_default", struct{}{})

// closingStepReviewKey covers every StageKindReview column (code_review,
// in_qa, pm_uat): the run judges or tests, it never changes the diff, so
// closing_step_default's "state what you changed" is always wrong there.
var closingStepReviewKey = prompt.Define("board.closing_step_review", struct{}{})

// closingStepReleaseKey covers the release engineer's done/released runs
// (StageKindTerminal on a non-analiz task — analiz's own done/released runs
// are the architect's and keep closing_step_default).
var closingStepReleaseKey = prompt.Define("board.closing_step_release", struct{}{})

type projectContextInput struct{ Description string }

var projectContextNoteKey = prompt.Define("board.project_context_note", projectContextInput{Description: "x"})

var revisionCommentsHeaderKey = prompt.Define("board.revision_comments_header", struct{}{})

type previousRunFailuresInput struct{ ErrorPattern string }

var previousRunFailuresKey = prompt.Define("board.previous_run_failures", previousRunFailuresInput{ErrorPattern: "x"})

var analysisContextIntroKey = prompt.Define("board.analysis_context_intro", struct{}{})

type analysisContextNoDocsInput struct{ Label string }

var analysisContextNoDocsKey = prompt.Define("board.analysis_context_no_docs", analysisContextNoDocsInput{Label: "x"})

type analysisContextRefHeaderInput struct{ Label, Key string }

var analysisContextRefHeaderKey = prompt.Define("board.analysis_context_ref_header", analysisContextRefHeaderInput{Label: "x", Key: "y"})

type analysisContextDocInput struct{ Title, Content string }

var analysisContextDocKey = prompt.Define("board.analysis_context_doc", analysisContextDocInput{Title: "x", Content: "y"})

type analysisContextTruncNoteInput struct{ Key string }

var analysisContextTruncNoteKey = prompt.Define("board.analysis_context_trunc_note", analysisContextTruncNoteInput{Key: "x"})

type openQuestionsBlockDetailInput struct{ Lines []string }

// openQuestionsBlockDetailKey is BlockOnResource's `detail` line when an
// analiz run ends with a pending blocking question: see
// blockOnPendingQuestions in questions_context.go.
var openQuestionsBlockDetailKey = prompt.Define("board.open_questions_block_detail",
	openQuestionsBlockDetailInput{Lines: []string{"Q1: Which queue should this use?"}})

var ungroundedAnalysisGuardKey = prompt.Define("guard.ungrounded_analysis", struct{}{})
var ungroundedQAGuardKey = prompt.Define("guard.ungrounded_qa", struct{}{})
var noUIEvidenceGuardKey = prompt.Define("guard.no_ui_evidence", struct{}{})
var ungroundedPMUATGuardKey = prompt.Define("guard.ungrounded_pmuat", struct{}{})
var pmUncoveredCriterionGuardKey = prompt.Define("guard.pm_uncovered_criterion", struct{}{})

var ungroundedAnalysisReason = prompt.Text(ungroundedAnalysisGuardKey)
var ungroundedQAReason = prompt.Text(ungroundedQAGuardKey)
var noUIEvidenceReason = prompt.Text(noUIEvidenceGuardKey)
var ungroundedPMUATReason = prompt.Text(ungroundedPMUATGuardKey)
var pmUncoveredCriterionReason = prompt.Text(pmUncoveredCriterionGuardKey)

var outOfBudgetKeptPublishedKey = prompt.Define("guard.out_of_budget_kept_published", struct{}{})
var outOfBudgetKeptLocalKey = prompt.Define("guard.out_of_budget_kept_local", struct{}{})

type outOfBudgetCommentInput struct {
	ErrorMessage string
	Kept         string
	HasPartial   bool
	Partial      string
}

var outOfBudgetCommentKey = prompt.Define("guard.out_of_budget_comment", outOfBudgetCommentInput{
	ErrorMessage: "x", Kept: "y",
})

var handoffUnverifiedRunKey = prompt.Define("guard.handoff_unverified_run", struct{}{})
var handoffUnseenUIKey = prompt.Define("guard.handoff_unseen_ui", struct{}{})

var pushWorkflowScopeCommentKey = prompt.Define("notices.push_workflow_scope_comment", struct{}{})
var pushWorkflowScopeDetailKey = prompt.Define("notices.push_workflow_scope_detail", struct{}{})

type handoffFailedCommandsInput struct{ Failed int }

var handoffFailedCommandsKey = prompt.Define("guard.handoff_failed_commands", handoffFailedCommandsInput{Failed: 3})

type handoffReasonInput struct{ Reason string }

var handoffCodeReviewRefusedKey = prompt.Define("guard.handoff_code_review_refused", handoffReasonInput{Reason: "x"})
var handoffAnalizReviewRefusedKey = prompt.Define("guard.handoff_analiz_review_refused", handoffReasonInput{Reason: "x"})
var handoffCodeReviewRefusedRevisionKey = prompt.Define("guard.handoff_code_review_refused_revision", handoffReasonInput{Reason: "x"})
var handoffAnalizReviewRefusedRevisionKey = prompt.Define("guard.handoff_analiz_review_refused_revision", handoffReasonInput{Reason: "x"})
