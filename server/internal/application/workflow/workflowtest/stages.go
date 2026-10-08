package workflowtest

import (
	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

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
	domain.TaskColumnHumanUAT: {ref(domain.BehaviourBuildVerify), ref(domain.BehaviourStoreTestBuildOnEnter)},
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

// designExtraBehaviours is the design type's extra set (migration 178): the
// analiz spine, reviewed in analiz_review, and approving the task approves the
// design system versions it proposed.
var designExtraBehaviours = map[domain.TaskColumn][]domain.BehaviourRef{
	domain.TaskColumnInProgress:   {ref(domain.BehaviourAdvanceOnDocument, "to", "analiz_review")},
	domain.TaskColumnAnalizReview: {ref(domain.BehaviourReviewChainStage, "label", "design review", "remedy", "move it to analiz_review and approve the design there")},
	domain.TaskColumnNeedRevision: {ref(domain.BehaviourAdvanceOnDocument, "to", "analiz_review")},
	domain.TaskColumnDone:         {ref(domain.BehaviourApproveDesignSystem)},
}

// onPath reports whether col is on the type's happy-path spine.
// need_revision/blocked are off-path for every type; pm_uat is additionally
// off-path for technical (migration 140's intent: technical skips pm_uat);
// analiz's spine is the 6 columns its own workflow actually uses.
func onPath(taskType domain.TaskType, col domain.TaskColumn) bool {
	if col == domain.TaskColumnNeedRevision || col == domain.TaskColumnBlocked {
		return false
	}
	if taskType == taskTypeAnaliz || taskType == taskTypeDesign {
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
		if (taskType == taskTypeAnaliz || taskType == taskTypeDesign) && analizRemovedColumns[c.Slug] {
			continue
		}
		var behaviours []domain.BehaviourRef
		behaviours = append(behaviours, allTypesBehaviours[c.Slug]...)
		instructions := ""
		switch taskType {
		case taskTypeAnaliz:
			// Migration 166 cleared workflow_stages.instructions for analiz: the
			// full prompt now lives in catalog/agents/system-architect's column
			// md files (see the catalog content test), not in the DB or here.
			behaviours = append(behaviours, analizExtraBehaviours[c.Slug]...)
		case taskTypeDesign:
			behaviours = append(behaviours, designExtraBehaviours[c.Slug]...)
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
	designerID := RoleID("designer")
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
	if taskType == taskTypeDesign {
		switch col {
		case domain.TaskColumnInProgress, domain.TaskColumnNeedRevision, domain.TaskColumnDone:
			return []domain.StageParticipant{worker(designerID)}
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
