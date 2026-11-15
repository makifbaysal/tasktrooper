package board

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

// This file registers every LLM-facing string the board_context group of
// this package renders — see catalog/system/prompts/board_context/**.
// Nothing here is a Go string literal; each Key's sample only has to be
// renderable, not realistic.

var commitMessageSystemKey = prompt.Define("board_context.commit_message_system", struct{}{})

type commitMessageUserInputData struct{ Title, Summary string }

var commitMessageUserInputKey = prompt.Define("board_context.commit_message_user_input",
	commitMessageUserInputData{Title: "Add the android app link", Summary: "Added the link."})

var criteriaSweepOpenFirstKey = prompt.Define("board_context.criteria_sweep_open_first", struct{}{})

type criteriaSweepOpenRepeatData struct{ Round int }

var criteriaSweepOpenRepeatKey = prompt.Define("board_context.criteria_sweep_open_repeat", criteriaSweepOpenRepeatData{Round: 1})

var criteriaSweepInstructionsKey = prompt.Define("board_context.criteria_sweep_instructions", struct{}{})

var criteriaSweepClosingFirstKey = prompt.Define("board_context.criteria_sweep_closing_first", struct{}{})

var criteriaSweepClosingRepeatKey = prompt.Define("board_context.criteria_sweep_closing_repeat", struct{}{})

type reviewVerdictAskData struct{ Exit string }

var reviewVerdictAskKey = prompt.Define("board_context.review_verdict_ask", reviewVerdictAskData{Exit: "ready_for_qa"})

type reviewVerdictSweepIntroData struct{ Column string }

var reviewVerdictSweepIntroKey = prompt.Define("board_context.review_verdict_sweep_intro", reviewVerdictSweepIntroData{Column: "code_review"})

type reviewVerdictSweepMissingIntroData struct{ Role string }

var reviewVerdictSweepMissingIntroKey = prompt.Define("board_context.review_verdict_sweep_missing_intro", reviewVerdictSweepMissingIntroData{Role: "qa"})

var reviewVerdictSweepCasesNoneKey = prompt.Define("board_context.review_verdict_sweep_cases_none", struct{}{})

type reviewVerdictSweepCasesPlannedData struct{ Count int }

var reviewVerdictSweepCasesPlannedKey = prompt.Define("board_context.review_verdict_sweep_cases_planned", reviewVerdictSweepCasesPlannedData{Count: 2})

var reviewVerdictSweepCasesFooterKey = prompt.Define("board_context.review_verdict_sweep_cases_footer", struct{}{})

var reviewVerdictSweepMissingFooterKey = prompt.Define("board_context.review_verdict_sweep_missing_footer", struct{}{})

type reviewVerdictSweepClosingData struct{ Exit string }

var reviewVerdictSweepClosingKey = prompt.Define("board_context.review_verdict_sweep_closing", reviewVerdictSweepClosingData{Exit: "ready_for_qa"})

type verifyFixPromptData struct{ FailReport string }

var verifyFixPromptKey = prompt.Define("board_context.verify_fix_prompt", verifyFixPromptData{FailReport: "$ go build ./...\nundefined: Foo"})

type coverageOverallWarningData struct{ Marker, Percent, Threshold string }

var coverageOverallWarningKey = prompt.Define("board_context.coverage_overall_warning",
	coverageOverallWarningData{Marker: coverageWarningMarker, Percent: "48.5", Threshold: "80"})

type coverageOverallNoteAdvisoryData struct{ Percent, Threshold string }

var coverageOverallNoteAdvisoryKey = prompt.Define("board_context.coverage_overall_note_advisory",
	coverageOverallNoteAdvisoryData{Percent: "48.5", Threshold: "80"})

type coverageOverallNoteUnsetData struct{ Percent string }

var coverageOverallNoteUnsetKey = prompt.Define("board_context.coverage_overall_note_unset",
	coverageOverallNoteUnsetData{Percent: "48.5"})

type mutationNoteData struct{ Percent string }

var mutationNoteKey = prompt.Define("board_context.mutation_note", mutationNoteData{Percent: "72.3"})

type mutationWarningData struct{ Marker, Percent, Threshold string }

var mutationWarningKey = prompt.Define("board_context.mutation_warning",
	mutationWarningData{Marker: mutationWarningMarker, Percent: "55.0", Threshold: "60"})

type mutationGateMetData struct{ Threshold string }

var mutationGateMetKey = prompt.Define("board_context.mutation_gate_met", mutationGateMetData{Threshold: "60"})

type newCodeCoverageTooFewData struct{ Covered, Total int }

var newCodeCoverageTooFewKey = prompt.Define("board_context.new_code_coverage_too_few", newCodeCoverageTooFewData{Covered: 2, Total: 3})

type newCodeCoverageOKData struct {
	Percent        string
	Covered, Total int
	Threshold      string
}

var newCodeCoverageOKKey = prompt.Define("board_context.new_code_coverage_ok",
	newCodeCoverageOKData{Percent: "100.0", Covered: 10, Total: 10, Threshold: "90"})

type newCodeCoverageWarningHeaderData struct {
	Marker         string
	Percent        string
	Covered, Total int
	Threshold      string
}

var newCodeCoverageWarningHeaderKey = prompt.Define("board_context.new_code_coverage_warning_header",
	newCodeCoverageWarningHeaderData{Marker: coverageWarningMarker, Percent: "50.0", Covered: 3, Total: 6, Threshold: "90"})

var newCodeCoverageScopeNoteKey = prompt.Define("board_context.new_code_coverage_scope_note", struct{}{})

var newCodeCoverageUncoveredLabelKey = prompt.Define("board_context.new_code_coverage_uncovered_label", struct{}{})

type newCodeCoverageMoreData struct{ Missing int }

var newCodeCoverageMoreKey = prompt.Define("board_context.new_code_coverage_more", newCodeCoverageMoreData{Missing: 1})

var newCodeCoverageClosingKey = prompt.Define("board_context.new_code_coverage_closing", struct{}{})

var humanRequirementsHeaderKey = prompt.Define("board_context.human_requirements_header", struct{}{})

type reviewPRContextData struct{ URL string }

var reviewPRContextKey = prompt.Define("board_context.review_pr_context", reviewPRContextData{URL: "https://github.com/acme/acme-web/pull/7"})

type reviewPRCommentsHeaderData struct{ URL string }

var reviewPRCommentsHeaderKey = prompt.Define("board_context.review_pr_comments_header", reviewPRCommentsHeaderData{URL: "https://github.com/acme/acme-web/pull/7"})

var reviewDiffHeadingBranchKey = prompt.Define("board_context.review_diff_heading_branch", struct{}{})

var reviewDiffHeadingPRKey = prompt.Define("board_context.review_diff_heading_pr", struct{}{})

type reviewAnnotationsHeaderData struct {
	Count   int
	DocIDs  string
	TaskRef string
}

var reviewAnnotationsHeaderKey = prompt.Define("board_context.review_annotations_header",
	reviewAnnotationsHeaderData{Count: 1, DocIDs: "doc-1", TaskRef: "A-7"})

type reviewAnnotationsOmittedData struct {
	Omitted int
	Limit   int
	TaskRef string
}

var reviewAnnotationsOmittedKey = prompt.Define("board_context.review_annotations_omitted",
	reviewAnnotationsOmittedData{Omitted: 3, Limit: reviewAnnotationsLimit, TaskRef: "A-7"})

type pipelineFailureCommentData struct{ Stage, Report string }

var pipelineFailureCommentKey = prompt.Define("board_context.pipeline_failure_comment",
	pipelineFailureCommentData{Stage: "build", Report: "exit status 1"})

var pipelineFailureBlockedCICommentKey = prompt.Define("board_context.pipeline_failure_blocked_ci_comment",
	pipelineFailureCommentData{Stage: "deploy", Report: "GitHub Actions billing limit reached"})

type analysisQuestionBlock struct {
	Key, Kind, Prompt, Answer, RecommendedAnswer string
	Blocking, Answered                           bool
}

type analysisQuestionsData struct {
	Label        string
	TaskRef      string
	Questions    []analysisQuestionBlock
	OmittedCount int
}

var analysisQuestionsKey = prompt.Define("board_context.analysis_questions", analysisQuestionsData{
	TaskRef: "A-7",
	Questions: []analysisQuestionBlock{
		{Key: "Q1", Kind: "technical", Prompt: "Which queue?", Blocking: true},
	},
})

var qaAutomationOnKey = prompt.Define("board.qa_automation_on", struct{}{})

var automationNoWorkspaceKey = prompt.Define("board_context.automation_no_workspace", struct{}{})

var automationNoTestsKey = prompt.Define("board_context.automation_no_tests", struct{}{})

var automationNoEntrypointKey = prompt.Define("board_context.automation_no_entrypoint", struct{}{})

type automationFixPromptInput struct{ Report string }

var automationFixPromptKey = prompt.Define("board_context.automation_fix_prompt", automationFixPromptInput{Report: "$ bash qa-automation/scripts/e2e.sh\nFAIL api/export_test"})

type automationFailedCommentInput struct {
	Rounds int
	Report string
}

var automationFailedCommentKey = prompt.Define("board_context.automation_failed_comment", automationFailedCommentInput{Rounds: 2, Report: "FAIL api/export_test"})
