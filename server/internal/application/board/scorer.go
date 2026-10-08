package board

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/rs/zerolog/log"
)

type ScoreApplier interface {
	ApplyDelta(ctx context.Context, input domain.ApplyScoreInput) (domain.AgentPerformanceScore, error)
}

type SpanOwnerLookup interface {
	OwnersForTask(ctx context.Context, taskID uuid.UUID) (map[string]uuid.UUID, error)
}

type TestCaseScoreLookup interface {
	ListByTask(ctx context.Context, taskID uuid.UUID) ([]domain.TaskTestCase, error)
	MarkScored(ctx context.Context, ids []uuid.UUID, at time.Time) error
}

type EventExistenceChecker interface {
	HasEventForTask(ctx context.Context, taskID uuid.UUID, eventType string) (bool, error)
}

type ScoreTracker struct {
	scores         ScoreApplier
	spans          SpanOwnerLookup
	testCases      TestCaseScoreLookup
	events         EventExistenceChecker
	workflows      port.WorkflowReader
	OnScoreUpdated func(agentID string, score float64, delta float64)
}

func NewScoreTracker(scores ScoreApplier) *ScoreTracker {
	return &ScoreTracker{scores: scores}
}

func (st *ScoreTracker) SetSpans(spans SpanOwnerLookup) {
	st.spans = spans
}

func (st *ScoreTracker) SetTestCases(testCases TestCaseScoreLookup) {
	st.testCases = testCases
}

func (st *ScoreTracker) SetEvents(events EventExistenceChecker) {
	st.events = events
}

func (st *ScoreTracker) SetWorkflows(w port.WorkflowReader) {
	st.workflows = w
}

var blameColumns = map[domain.TaskColumn][]string{
	domain.TaskColumnHumanUAT:   {"in_progress", "in_qa", "pm_uat"},
	domain.TaskColumnPMUAT:      {"in_progress", "in_qa"},
	domain.TaskColumnCodeReview: {"in_progress"},
	domain.TaskColumnReadyForQA: {"in_progress"},
	domain.TaskColumnInQA:       {"in_progress"},
}

type rejectionRule struct {
	evType string
	delta  float64
	reason string
}

var rejectionRules = map[domain.TaskColumn]rejectionRule{
	domain.TaskColumnHumanUAT:   {domain.ScoreEventHumanUATFailed, domain.ScoreDeltaHumanUATFailed, "Human UAT failed"},
	domain.TaskColumnPMUAT:      {domain.ScoreEventPMUATFailed, domain.ScoreDeltaPMUATFailed, "PM UAT failed"},
	domain.TaskColumnCodeReview: {domain.ScoreEventRevisionRequested, domain.ScoreDeltaRevisionRequested, "Code review returned for revision"},
	domain.TaskColumnReadyForQA: {domain.ScoreEventRevisionRequested, domain.ScoreDeltaRevisionRequested, "QA returned for revision"},
	domain.TaskColumnInQA:       {domain.ScoreEventRevisionRequested, domain.ScoreDeltaRevisionRequested, "QA returned for revision"},
}

func (st *ScoreTracker) OnColumnTransition(ctx context.Context, task domain.BoardTask, from, to domain.TaskColumn) {
	if st == nil || st.scores == nil {
		return
	}

	fromQA := from == domain.TaskColumnReadyForQA || from == domain.TaskColumnInQA

	if to == domain.TaskColumnNeedRevision {
		rule, ok := rejectionRules[from]
		if !ok {
			return
		}
		for _, agentID := range st.blamed(ctx, task, blameColumns[from]) {
			st.apply(ctx, task, agentID, rule.evType, rule.delta, rule.reason)
		}
		if fromQA {
			st.scoreQARound(ctx, task, false)
		}
		return
	}

	if to == domain.TaskColumnDone || to == domain.TaskColumnReleased {
		if task.AssigneeAgentID != nil {
			evType, delta, reason := domain.ScoreEventTaskCompleted, domain.ScoreDeltaTaskCompleted, "Task completed"
			if to == domain.TaskColumnReleased {
				evType, delta, reason = domain.ScoreEventTaskReleased, domain.ScoreDeltaTaskReleased, "Task released"
			}
			st.apply(ctx, task, *task.AssigneeAgentID, evType, delta, reason)
		}
	}

	if fromQA && st.isForwardExit(ctx, task.TaskType, to) {
		st.scoreQARound(ctx, task, true)
		st.creditRoleCompletion(ctx, task, "in_qa", domain.ScoreEventQATaskTested, domain.ScoreDeltaQATaskTested, "QA finished testing")
	}
	if from == domain.TaskColumnPMUAT && st.isForwardExit(ctx, task.TaskType, to) {
		st.creditRoleCompletion(ctx, task, "pm_uat", domain.ScoreEventPMUATCompleted, domain.ScoreDeltaPMUATCompleted, "PM finished UAT review")
	}
}

func (st *ScoreTracker) creditRoleCompletion(ctx context.Context, task domain.BoardTask, column, evType string, delta float64, reason string) {
	if st.events == nil {
		return
	}
	already, err := st.events.HasEventForTask(ctx, task.ID, evType)
	if err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("event existence check failed")
		return
	}
	if already {
		return
	}
	agentID, ok := st.columnOwner(ctx, task, column)
	if !ok {
		return
	}
	st.apply(ctx, task, agentID, evType, delta, reason)
}

func (st *ScoreTracker) isForwardExit(ctx context.Context, taskType domain.TaskType, to domain.TaskColumn) bool {
	if st.workflows == nil {
		return false
	}
	wf, err := st.workflows.Workflow(ctx, taskType)
	if err != nil {
		log.Warn().Err(err).Str("task_type", string(taskType)).
			Msg("scorer: workflow lookup failed, failing closed")
		return false
	}
	return wf.Has(to, domain.BehaviourForwardExit)
}

func (st *ScoreTracker) scoreQARound(ctx context.Context, task domain.BoardTask, forward bool) {
	if st.testCases == nil {
		return
	}
	qa, ok := st.qaOwner(ctx, task)
	if !ok {
		return
	}

	items, err := st.testCases.ListByTask(ctx, task.ID)
	if err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("qa round lookup failed")
		return
	}

	var bugs, valid, invalid int
	var scored []uuid.UUID
	for _, c := range items {
		if c.ScoredAt != nil {
			continue
		}
		switch c.Status {
		case domain.TestCaseStatusFailed:
			bugs++
			scored = append(scored, c.ID)
		case domain.TestCaseStatusPassed:
			if forward {
				valid++
				scored = append(scored, c.ID)
			}
		case domain.TestCaseStatusInvalid:
			if forward {
				invalid++
				scored = append(scored, c.ID)
			}
		case domain.TestCaseStatusSkipped:
			if forward {
				scored = append(scored, c.ID)
			}
		}
	}

	if bugs > 0 {
		st.apply(ctx, task, qa, domain.ScoreEventQABugFound,
			domain.ScoreDeltaQABugFound*float64(bugs), fmt.Sprintf("%d bug found", bugs))
	}
	if valid > 0 {
		st.apply(ctx, task, qa, domain.ScoreEventQAValidScenarioConfirmed,
			domain.ScoreDeltaQAValidScenarioConfirmed*float64(valid), fmt.Sprintf("%d valid scenario(s) confirmed", valid))
	}
	if invalid > 0 {
		st.apply(ctx, task, qa, domain.ScoreEventQAInvalidScenarioConfirmed,
			domain.ScoreDeltaQAInvalidScenarioConfirmed*float64(invalid), fmt.Sprintf("%d scenario(s) confirmed invalid", invalid))
	}
	if len(scored) > 0 {
		if err := st.testCases.MarkScored(ctx, scored, time.Now()); err != nil {
			log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("mark test cases scored failed")
		}
	}
}

func (st *ScoreTracker) ApplyReviewEscape(ctx context.Context, task domain.BoardTask, agentID uuid.UUID) {
	if st == nil || st.scores == nil {
		return
	}
	st.apply(ctx, task, agentID, domain.ScoreEventReviewEscape, domain.ScoreDeltaReviewEscape,
		"Human rejected a change the reviewer approved")
}

func (st *ScoreTracker) blamed(ctx context.Context, task domain.BoardTask, columns []string) []uuid.UUID {
	if st.spans == nil {
		return assigneeOnly(task)
	}
	owners, err := st.spans.OwnersForTask(ctx, task.ID)
	if err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("span owners lookup failed")
		return assigneeOnly(task)
	}
	seen := make(map[uuid.UUID]bool, len(columns))
	out := make([]uuid.UUID, 0, len(columns))
	for _, col := range columns {
		agentID, ok := owners[col]
		if !ok || seen[agentID] {
			continue
		}
		seen[agentID] = true
		out = append(out, agentID)
	}
	return out
}

func (st *ScoreTracker) qaOwner(ctx context.Context, task domain.BoardTask) (uuid.UUID, bool) {
	return st.columnOwner(ctx, task, "in_qa")
}

func (st *ScoreTracker) columnOwner(ctx context.Context, task domain.BoardTask, column string) (uuid.UUID, bool) {
	if st.spans == nil {
		return uuid.Nil, false
	}
	owners, err := st.spans.OwnersForTask(ctx, task.ID)
	if err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("span owners lookup failed")
		return uuid.Nil, false
	}
	agentID, ok := owners[column]
	return agentID, ok
}

func assigneeOnly(task domain.BoardTask) []uuid.UUID {
	if task.AssigneeAgentID == nil {
		return nil
	}
	return []uuid.UUID{*task.AssigneeAgentID}
}

func (st *ScoreTracker) apply(ctx context.Context, task domain.BoardTask, agentID uuid.UUID, evType string, delta float64, reason string) {
	taskIDPtr := &task.ID
	updated, err := st.scores.ApplyDelta(ctx, domain.ApplyScoreInput{
		AgentID:   agentID,
		TaskID:    taskIDPtr,
		EventType: evType,
		Delta:     delta,
		Reason:    fmt.Sprintf("%s: %s", reason, task.Title),
	})
	if err != nil {
		log.Warn().Err(err).Str("agent_id", agentID.String()).Msg("score delta failed")
		return
	}
	if st.OnScoreUpdated != nil {
		st.OnScoreUpdated(agentID.String(), updated.Score, delta)
	}
}
