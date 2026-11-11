package repository

import (
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// This file registers every LLM-facing string the repository gates render —
// see catalog/system/guards/** and catalog/system/prompts/repository/**.
// Nothing here is a Go string literal; each Key's sample only has to be
// renderable, not realistic.

type criteriaIncompleteInput struct {
	Target string
	Count  int
	Open   []string
}

var criteriaIncompleteKey = prompt.Define("guard.criteria_incomplete", criteriaIncompleteInput{
	Target: "ready_for_qa", Count: 1, Open: []string{"[id] a"},
})

type criteriaRejectedInput struct {
	Target   string
	Count    int
	Role     string
	Rejected []string
}

var criteriaRejectedKey = prompt.Define("guard.criteria_rejected", criteriaRejectedInput{
	Target: "pm_uat", Count: 1, Role: "qa", Rejected: []string{"[id] a (note)"},
})

type criteriaUncheckedInput struct {
	Target    string
	Count     int
	Role      string
	Unchecked []string
}

var criteriaUncheckedKey = prompt.Define("guard.criteria_unchecked", criteriaUncheckedInput{
	Target: "done", Count: 1, Role: "qa", Unchecked: []string{"[id] a"},
})

type testCasesMissingInput struct{ Target string }

var testCasesMissingKey = prompt.Define("guard.test_cases_missing", testCasesMissingInput{Target: "pm_uat"})

type testCasesPlannedInput struct {
	Target  string
	Count   int
	Planned []string
}

var testCasesPlannedKey = prompt.Define("guard.test_cases_planned", testCasesPlannedInput{
	Target: "pm_uat", Count: 1, Planned: []string{"a case"},
})

var selfMoveNeedRevisionKey = prompt.Define("guard.self_move_need_revision", struct{}{})

var selfMoveTodoKey = prompt.Define("guard.self_move_todo", struct{}{})

type workOrderBlockedInput struct{ Labels []string }

var workOrderBlockedKey = prompt.Define("guard.work_order_blocked", workOrderBlockedInput{Labels: []string{"T-5 (API migration) [in_progress]"}})

type criterionVerdictWrongColumnInput struct{ Key, Column string }

var criterionVerdictWrongColumnKey = prompt.Define("guard.criterion_verdict_wrong_column", criterionVerdictWrongColumnInput{Key: "T-4", Column: "in_progress"})

var criterionCancelReasonRequiredKey = prompt.Define("guard.criterion_cancel_reason_required", struct{}{})
var criterionRejectNoteRequiredKey = prompt.Define("guard.criterion_reject_note_required", struct{}{})
var documentTooLargeHintKey = prompt.Define("guard.document_too_large_hint", struct{}{})
var noRepositoriesKey = prompt.Define("guard.no_repositories", struct{}{})
var relationTargetRequiredKey = prompt.Define("guard.relation_target_required", struct{}{})

type stageNotConfiguredInput struct{ TaskType, Column string }

var stageNotConfiguredKey = prompt.Define("guard.stage_not_configured", stageNotConfiguredInput{TaskType: "backend", Column: "in_progress"})

type workOrderCycleInput struct{ Task, Blocker, Path string }

var workOrderCycleKey = prompt.Define("guard.work_order_cycle", workOrderCycleInput{Task: "T-1", Blocker: "T-2", Path: "T-1 → T-2"})

type deployOrderCycleInput struct{ Dependency, Task, Path string }

var deployOrderCycleKey = prompt.Define("guard.deploy_order_cycle", deployOrderCycleInput{Dependency: "T-1", Task: "T-2", Path: "T-1 → T-2"})

type testCaseDuplicateTitleInput struct{ Title string }

var testCaseDuplicateTitleKey = prompt.Define("guard.test_case_duplicate_title", testCaseDuplicateTitleInput{Title: `"logs in"`})

type testCaseCriterionNotOnTaskInput struct{ CriterionID string }

var testCaseCriterionNotOnTaskKey = prompt.Define("guard.test_case_criterion_not_on_task", testCaseCriterionNotOnTaskInput{CriterionID: "11111111-1111-1111-1111-111111111111"})

type reviewChainErrInput struct{ Err string }

var reviewChainWorkflowUnreadableKey = prompt.Define("guard.review_chain_workflow_unreadable", reviewChainErrInput{Err: "no workflow configured"})
var reviewChainHistoryUnreadableKey = prompt.Define("guard.review_chain_history_unreadable", reviewChainErrInput{Err: "db down"})

var reviewChainNoSpanStoreKey = prompt.Define("guard.review_chain_no_span_store", struct{}{})

type reviewChainStageRejectedInput struct{ Task, Target, Rejected string }

var reviewChainStageRejectedKey = prompt.Define("guard.review_chain_stage_rejected", reviewChainStageRejectedInput{
	Task: "T-1", Target: "done", Rejected: "QA (in_qa) — send back to need_revision",
})

type reviewChainMissingStagesInput struct{ Task, Target, Missing string }

var reviewChainMissingStagesKey = prompt.Define("guard.review_chain_missing_stages", reviewChainMissingStagesInput{
	Task: "T-1", Target: "done", Missing: "QA (in_qa) — needs a verdict",
})

type reviewStageSkippedInput struct{ Task, From, Target, TaskType, Skipped, Next string }

var reviewStageSkippedKey = prompt.Define("guard.review_stage_skipped", reviewStageSkippedInput{
	Task: "T-1", From: "in_qa", Target: "human_uat", TaskType: "task", Skipped: "UAT (pm_uat)", Next: "pm_uat",
})

type workflowSetupTaskBriefInput struct{ Kind string }

var workflowSetupTaskBriefKey = prompt.Define("repository.workflow_setup_task_brief", workflowSetupTaskBriefInput{Kind: "backend"})

type orderNoteInput struct {
	DeployAfter []string
	WorkAfter   []string
}

var orderNoteKey = prompt.Define[orderNoteInput]("briefs.repository.order_note", orderNoteInput{
	DeployAfter: []string{"T-12"},
})

// renderOrderNote is WP7's application-consumer render of domain's
// order-note data (domain must not import application, so the generated
// block's prose lives in catalog/system and renders here, wrapped in the
// domain-owned fence markers).
func renderOrderNote(deployAfter, workAfter []string) string {
	if domain.OrderNoteEmpty(deployAfter, workAfter) {
		return ""
	}
	return domain.OrderNoteOpen + "\n" + orderNoteKey.Render(orderNoteInput{DeployAfter: deployAfter, WorkAfter: workAfter}) + domain.OrderNoteClose
}
