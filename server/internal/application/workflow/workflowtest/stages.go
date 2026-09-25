package workflowtest

import (
	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// The instruction strings below are copied verbatim from
// board.taskTypeInstruction/analizProducesDocuments (runner.go) at the time
// migration 143 was written. They are duplicated rather than imported to
// avoid a dependency from this package onto application/board (which will
// itself depend on workflowtest in its tests); WP-B2b's golden test is what
// keeps them honest against the runner's own constants until the runner is
// switched to read stage.Instructions instead of calling taskTypeInstruction.
const analizProducesDocuments = "Your deliverable is a SPEC and an IMPLEMENTATION PLAN attached to this task with add_task_document, " +
	"grounded in code you actually read (get_repo_tree, codebase_search, grep_code, get_symbol_skeleton, expand_symbol_context) — " +
	"a document attached by a run that explored nothing is rejected and the run is failed. " +
	"If this task already carries a spec or a plan — a revision pass, a need_revision bounce, a change the human asked for — rewrite THAT document with update_task_document instead of attaching another one: the card must end with one current spec and one current plan. " +
	"Never write, edit, move or delete a file in the repository and never commit: an analysis produces documents, not a diff, " +
	"and there is no automatic hand-off to code_review for this task type — a run that ends with file edits has done the implementer's job on the wrong task. " +
	"Finish with a summary comment (approach, the document titles, the task split you intend), then STOP: " +
	"when this run ends with a document attached, the system moves the task to `analiz_review` for you — do NOT move it yourself and never plan a step for the move — " +
	"the human approves there, and no implementation task is created before they do."

const analizTodoInstructions = "This is an ANALIZ task (task_type=analiz) in `todo` — an ANALYSIS, not an implementation. If it is not relevant to your role, take no action. " +
	"If it is: claim it and move it to in_progress as the opening action of the step that does the analysis (never a step of its own), " +
	"then investigate in this same run — clone/pull every repository the task names, read the relevant code, and decide WHAT is needed and WHERE. " +
	analizProducesDocuments

const analizInProgressInstructions = "This is an ANALIZ task (task_type=analiz) ALREADY claimed and ALREADY in `in_progress` — an ANALYSIS, not an implementation, " +
	"and the move you might be tempted to plan first has happened. Continue the investigation from where it stands and finish it in this run. " +
	analizProducesDocuments

const analizNeedRevisionInstructions = "This is an ANALIZ task (task_type=analiz) in `need_revision`: the human rejected the analysis. Their comment is in the task comments in your context. " +
	"Revise the spec/plan at the ROOT of the concern — re-read the code where you are unsure — and attach the corrected documents. " +
	"Create no implementation task from a rejected analysis. " + analizProducesDocuments

const analizReviewInstructions = "This is an ANALIZ task (task_type=analiz) in `analiz_review`: it is waiting on a HUMAN to approve or reject the spec/plan. " +
	"Nothing is yours to do here — do not move it, do not rewrite the documents, and do not create implementation tasks. Take no action."

const analizDoneInstructions = "This is an ANALIZ task (task_type=analiz) the human moved to `done` — that move IS the approval of your spec and plan. " +
	"Now decompose it: one implementation task per repository and per layer, each with its own plan slice, testable acceptance criteria and an assignee " +
	"(call list_team for the roster; order them by dependency — backend API before the frontend/mobile that consumes it). " +
	"Write no code yourself. List the created tasks in a comment and move this analiz task to `released` as the last action of the step that created them."

// column is one of the 13 default columns every task type gets a stage row
// for, in migration/fallback-position order.
type column struct {
	Slug domain.TaskColumn
	Kind domain.StageKind
}

var columns = []column{
	{domain.TaskColumnBacklog, domain.StageKindIntake},
	{domain.TaskColumnTodo, domain.StageKindQueue},
	{domain.TaskColumnInProgress, domain.StageKindWork},
	{domain.TaskColumnAnalizReview, domain.StageKindApproval},
	{domain.TaskColumnCodeReview, domain.StageKindReview},
	{domain.TaskColumnReadyForQA, domain.StageKindQueue},
	{domain.TaskColumnInQA, domain.StageKindReview},
	{domain.TaskColumnNeedRevision, domain.StageKindRework},
	{domain.TaskColumnPMUAT, domain.StageKindReview},
	{domain.TaskColumnHumanUAT, domain.StageKindApproval},
	{domain.TaskColumnBlocked, domain.StageKindParked},
	{domain.TaskColumnDone, domain.StageKindTerminal},
	{domain.TaskColumnReleased, domain.StageKindTerminal},
}

// allTypesBehaviours is the behaviour set every task type carries for a
// column — release-b-plan.md §3's "all types" cells.
var allTypesBehaviours = map[domain.TaskColumn][]domain.BehaviourRef{
	domain.TaskColumnBacklog: {},
	domain.TaskColumnTodo: {
		ref(domain.BehaviourAutoEnter, "to", "in_progress", "assignee_only", "true"),
		ref(domain.BehaviourBlockOnDependencies),
		ref(domain.BehaviourCommitOnFinish),
	},
	domain.TaskColumnInProgress: {
		ref(domain.BehaviourBlockOnDependencies, "refuse_move", "true"),
		ref(domain.BehaviourCommitOnFinish),
	},
	domain.TaskColumnAnalizReview: {
		ref(domain.BehaviourRouteToSubscribers), ref(domain.BehaviourStripWriters),
	},
	domain.TaskColumnCodeReview: {
		ref(domain.BehaviourRouteToSubscribers), ref(domain.BehaviourWaitForCI), ref(domain.BehaviourEnsurePROnEnter),
		ref(domain.BehaviourDetectMigrationOnEnter),
		ref(domain.BehaviourStageDeployOnEnter, "when", "per_step"),
		ref(domain.BehaviourRequirePRForReview),
		ref(domain.BehaviourRequireCriteriaComplete), ref(domain.BehaviourStripWriters),
	},
	domain.TaskColumnReadyForQA: {
		ref(domain.BehaviourRouteToSubscribers), ref(domain.BehaviourDetectMigrationOnEnter),
		ref(domain.BehaviourStageDeployOnEnter, "when", "qa"),
		ref(domain.BehaviourRequireCriteriaComplete),
		ref(domain.BehaviourCriterionVerdict, "channel", "qa"),
		ref(domain.BehaviourRequireTestCases), ref(domain.BehaviourStripWriters), ref(domain.BehaviourCommitOnFinish),
	},
	domain.TaskColumnInQA: {
		ref(domain.BehaviourRouteToSubscribers),
		ref(domain.BehaviourCriterionVerdict, "channel", "qa"),
		ref(domain.BehaviourRequireTestCases), ref(domain.BehaviourStripWriters), ref(domain.BehaviourCommitOnFinish),
	},
	domain.TaskColumnNeedRevision: {
		ref(domain.BehaviourAutoEnter, "to", "in_progress", "assignee_only", "true"),
		ref(domain.BehaviourCommitOnFinish),
	},
	domain.TaskColumnPMUAT: {
		ref(domain.BehaviourRouteToSubscribers), ref(domain.BehaviourEnsurePROnEnter), ref(domain.BehaviourForwardExit),
		ref(domain.BehaviourCriterionVerdict, "channel", "pm"),
		ref(domain.BehaviourRequireProductCheck), ref(domain.BehaviourStripWriters),
		ref(domain.BehaviourNoCodeReading),
	},
	domain.TaskColumnHumanUAT: {
		ref(domain.BehaviourRouteToSubscribers), ref(domain.BehaviourForwardExit), ref(domain.BehaviourStripWriters),
		ref(domain.BehaviourNoCodeReading), ref(domain.BehaviourCommitOnFinish),
	},
	domain.TaskColumnBlocked: {},
	domain.TaskColumnDone: {
		ref(domain.BehaviourEnsurePROnEnter), ref(domain.BehaviourForwardExit), ref(domain.BehaviourRequireCriteriaComplete),
		ref(domain.BehaviourStripWriters, "allow", "merge_task_pull_request,release_control"),
	},
	domain.TaskColumnReleased: {
		ref(domain.BehaviourForwardExit), ref(domain.BehaviourRequireCriteriaComplete),
		ref(domain.BehaviourStripWriters, "allow", "release_control"),
	},
}

// codingExtra is the extra behaviour set task/bug/technical carry for a
// column, ON TOP of allTypesBehaviours — §3's "extra task/bug/technical"
// cells, for task and bug exactly (technical overrides two cells below).
var codingExtra = map[domain.TaskColumn][]domain.BehaviourRef{
	domain.TaskColumnTodo: {ref(domain.BehaviourBuildVerify)},
	domain.TaskColumnInProgress: {
		ref(domain.BehaviourBuildVerify), ref(domain.BehaviourAdvanceOnDiff, "to", "code_review"),
	},
	domain.TaskColumnCodeReview: {
		ref(domain.BehaviourReviewVerdictSweep, "pass_to", "ready_for_qa"),
		ref(domain.BehaviourReviewChainStage, "label", "code review", "remedy", "move it to code_review so the diff is reviewed"),
	},
	domain.TaskColumnReadyForQA: {
		ref(domain.BehaviourBuildVerify),
		ref(domain.BehaviourAutoEnter, "to", "in_qa", "assignee_only", "false"),
		ref(domain.BehaviourRequireExecutionEvidence), ref(domain.BehaviourReviewVerdictSweep, "pass_to", "pm_uat"),
	},
	domain.TaskColumnInQA: {
		ref(domain.BehaviourBuildVerify), ref(domain.BehaviourRequireExecutionEvidence), ref(domain.BehaviourReviewVerdictSweep, "pass_to", "pm_uat"),
		ref(domain.BehaviourReviewChainStage, "label", "QA", "remedy", "move it to ready_for_qa; QA takes it into in_qa and tests it there"),
	},
	domain.TaskColumnNeedRevision: {
		ref(domain.BehaviourBuildVerify), ref(domain.BehaviourAdvanceOnDiff, "to", "code_review"),
	},
	domain.TaskColumnPMUAT: {
		ref(domain.BehaviourReviewVerdictSweep, "pass_to", "human_uat"),
		ref(domain.BehaviourReviewChainStage, "label", "UAT", "remedy", "move it to pm_uat so every acceptance criterion is verified against QA's evidence"),
	},
	domain.TaskColumnHumanUAT: {ref(domain.BehaviourBuildVerify)},
	domain.TaskColumnDone: {
		ref(domain.BehaviourDispatchSuspended), ref(domain.BehaviourMergePROnEnter), ref(domain.BehaviourWatchDeployOnResume),
	},
	domain.TaskColumnReleased: {
		ref(domain.BehaviourWatchDeployOnResume),
	},
}

// analizExtraBehaviours is analiz's own extra set — §3's "analiz extra" cells
// that carry a behaviour (not only instructions).
var analizExtraBehaviours = map[domain.TaskColumn][]domain.BehaviourRef{
	domain.TaskColumnInProgress:   {ref(domain.BehaviourAdvanceOnDocument, "to", "analiz_review")},
	domain.TaskColumnAnalizReview: {ref(domain.BehaviourReviewChainStage, "label", "analiz review", "remedy", "move it to analiz_review and approve the spec/plan there")},
	domain.TaskColumnNeedRevision: {ref(domain.BehaviourAdvanceOnDocument, "to", "analiz_review")},
}

var analizInstructions = map[domain.TaskColumn]string{
	domain.TaskColumnTodo:         analizTodoInstructions,
	domain.TaskColumnInProgress:   analizInProgressInstructions,
	domain.TaskColumnAnalizReview: analizReviewInstructions,
	domain.TaskColumnNeedRevision: analizNeedRevisionInstructions,
	domain.TaskColumnDone:         analizDoneInstructions,
}

// onPath reports whether col is on the type's happy-path spine.
// need_revision/blocked are off-path for every type; pm_uat is additionally
// off-path for technical (migration 140's intent: technical skips pm_uat);
// analiz's spine is the 6 columns its own workflow actually uses.
func onPath(taskType domain.TaskType, col domain.TaskColumn) bool {
	if col == domain.TaskColumnNeedRevision || col == domain.TaskColumnBlocked {
		return false
	}
	if taskType == taskTypeAnaliz {
		switch col {
		case domain.TaskColumnBacklog, domain.TaskColumnTodo, domain.TaskColumnInProgress,
			domain.TaskColumnAnalizReview, domain.TaskColumnDone, domain.TaskColumnReleased:
			return true
		default:
			return false
		}
	}
	if taskType == taskTypeTechnical && col == domain.TaskColumnPMUAT {
		return false
	}
	// task/bug/technical spine excludes analiz_review.
	return col != domain.TaskColumnAnalizReview
}

// analizRemovedColumns are the 5 columns migration 148 deletes for analiz:
// vestigial copy-paste rows from the coding task-type template that never
// carried participants, never carried real instructions, and nothing in
// analiz's own behaviour set (advance_on_document routes only through
// in_progress/need_revision -> analiz_review) ever reaches. analiz's real
// spine is backlog/todo/in_progress/analiz_review/need_revision/blocked/
// done/released — 8 stages, not 13.
var analizRemovedColumns = map[domain.TaskColumn]bool{
	domain.TaskColumnCodeReview: true,
	domain.TaskColumnReadyForQA: true,
	domain.TaskColumnInQA:       true,
	domain.TaskColumnPMUAT:      true,
	domain.TaskColumnHumanUAT:   true,
}

// stagesFor builds the default-column stages for taskType, in column order
// (which is also fallback position order — see column above; task/bug/
// technical get all 13, analiz gets 8, see analizRemovedColumns). Position
// stays the source column's index in the full 13-element list even when a
// column is skipped, so it matches what migration 143 + 148 leave in a real
// database (positions sparse, not renumbered) rather than a dense 0..N-1
// over the surviving stages only.
func stagesFor(taskType domain.TaskType) []domain.WorkflowStage {
	stages := make([]domain.WorkflowStage, 0, len(columns))
	for i, c := range columns {
		if taskType == taskTypeAnaliz && analizRemovedColumns[c.Slug] {
			continue
		}
		var behaviours []domain.BehaviourRef
		behaviours = append(behaviours, allTypesBehaviours[c.Slug]...)
		instructions := ""
		switch taskType {
		case taskTypeAnaliz:
			behaviours = append(behaviours, analizExtraBehaviours[c.Slug]...)
			instructions = analizInstructions[c.Slug]
		default: // task, bug, technical
			extra := codingExtra[c.Slug]
			if taskType == taskTypeTechnical {
				extra = technicalOverride(c.Slug, extra)
			}
			behaviours = append(behaviours, extra...)
		}
		stages = append(stages, domain.WorkflowStage{
			TaskType:     taskType,
			Column:       c.Slug,
			Position:     i,
			OnPath:       onPath(taskType, c.Slug),
			Kind:         c.Kind,
			Behaviours:   behaviours,
			Instructions: instructions,
			Participants: participantsFor(taskType, c.Slug),
		})
	}
	return stages
}

// technicalOverride applies the two deliberate technical-type differences
// from §3: ready_for_qa/in_qa sweep to human_uat instead of pm_uat, and
// pm_uat never carries review_chain_stage (technical's review chain does not
// require pm_uat).
func technicalOverride(col domain.TaskColumn, extra []domain.BehaviourRef) []domain.BehaviourRef {
	switch col {
	case domain.TaskColumnReadyForQA, domain.TaskColumnInQA:
		out := make([]domain.BehaviourRef, 0, len(extra))
		for _, b := range extra {
			if b.Key == domain.BehaviourReviewVerdictSweep {
				out = append(out, ref(domain.BehaviourReviewVerdictSweep, "pass_to", "human_uat"))
				continue
			}
			out = append(out, b)
		}
		return out
	case domain.TaskColumnPMUAT:
		out := make([]domain.BehaviourRef, 0, len(extra))
		for _, b := range extra {
			if b.Key == domain.BehaviourReviewChainStage {
				continue
			}
			out = append(out, b)
		}
		return out
	default:
		return extra
	}
}

// participantsFor is §3's "Participants (informational in B)" table.
func participantsFor(taskType domain.TaskType, col domain.TaskColumn) []domain.StageParticipant {
	developerID, analystID, architectID, qaID, pmID := RoleID("developer"), RoleID("analyst"), RoleID("architect"), RoleID("qa"), RoleID("product_manager")
	releaseID := RoleID("release")
	worker := func(roleID uuid.UUID) domain.StageParticipant {
		return domain.StageParticipant{RoleID: roleID, Mode: domain.ParticipantModeWorker}
	}
	approver := func(roleID uuid.UUID) domain.StageParticipant {
		return domain.StageParticipant{RoleID: roleID, Mode: domain.ParticipantModeApprover}
	}
	if taskType == taskTypeAnaliz {
		switch col {
		case domain.TaskColumnInProgress, domain.TaskColumnNeedRevision, domain.TaskColumnDone:
			return []domain.StageParticipant{worker(analystID)}
		default:
			return nil
		}
	}
	switch col {
	case domain.TaskColumnInProgress, domain.TaskColumnNeedRevision:
		return []domain.StageParticipant{worker(developerID)}
	case domain.TaskColumnCodeReview:
		return []domain.StageParticipant{approver(architectID)}
	case domain.TaskColumnReadyForQA, domain.TaskColumnInQA:
		return []domain.StageParticipant{worker(qaID)}
	case domain.TaskColumnDone, domain.TaskColumnReleased:
		return []domain.StageParticipant{worker(releaseID)}
	case domain.TaskColumnPMUAT:
		return []domain.StageParticipant{approver(pmID)}
	default:
		return nil
	}
}
