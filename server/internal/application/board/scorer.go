package board

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/rs/zerolog/log"
)

// ScoreApplier is the slice of AgentPerformanceStore the tracker writes through.
type ScoreApplier interface {
	ApplyDelta(ctx context.Context, input domain.ApplyScoreInput) (domain.AgentPerformanceScore, error)
}

// SpanOwnerLookup resolves which agent worked each column of a task.
type SpanOwnerLookup interface {
	OwnersForTask(ctx context.Context, taskID uuid.UUID) (map[string]uuid.UUID, error)
}

// TestCaseScoreLookup is the slice of TaskTestCaseStore the tracker reads a
// QA round from and stamps once that round has been turned into score events.
type TestCaseScoreLookup interface {
	ListByTask(ctx context.Context, taskID uuid.UUID) ([]domain.TaskTestCase, error)
	MarkScored(ctx context.Context, ids []uuid.UUID, at time.Time) error
}

type ScoreTracker struct {
	scores         ScoreApplier
	spans          SpanOwnerLookup
	testCases      TestCaseScoreLookup
	OnScoreUpdated func(agentID string, score float64, delta float64)
}

func NewScoreTracker(scores ScoreApplier) *ScoreTracker {
	return &ScoreTracker{scores: scores}
}

// SetSpans attaches the span ledger used to decide who a defect belongs to.
// Without it the tracker falls back to the task's assignee, which is the wrong
// agent as soon as a task has changed hands.
func (st *ScoreTracker) SetSpans(spans SpanOwnerLookup) {
	st.spans = spans
}

// SetTestCases attaches the QA round store used to score bugs found and
// scenario verdicts confirmed. Without it QA scoring is a no-op.
func (st *ScoreTracker) SetTestCases(testCases TestCaseScoreLookup) {
	st.testCases = testCases
}

// blameColumns lists, per rejecting column, the columns whose owners are
// accountable for the defect. A defect that reached the human escaped every
// gate before it, so all of them are charged; one the PM caught only reaches
// the dev and QA.
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
	domain.TaskColumnCodeReview: {domain.ScoreEventRevisionRequested, domain.ScoreDeltaRevisionRequested, "Architect returned for revision"},
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

	// Completion credit follows the assignee: it is the task's outcome, not one
	// stage's, and every contributor already carries their own penalties.
	if to == domain.TaskColumnDone || to == domain.TaskColumnReleased {
		if task.AssigneeAgentID != nil {
			evType, delta, reason := domain.ScoreEventTaskCompleted, domain.ScoreDeltaTaskCompleted, "Task completed"
			if to == domain.TaskColumnReleased {
				evType, delta, reason = domain.ScoreEventTaskReleased, domain.ScoreDeltaTaskReleased, "Task released"
			}
			st.apply(ctx, task, *task.AssigneeAgentID, evType, delta, reason)
		}
	}

	if fromQA && isQAForwardExit(to) {
		st.scoreQARound(ctx, task, true)
	}
}

// isQAForwardExit reports whether a task leaving ready_for_qa/in_qa for this
// column means QA signed the round off. It mirrors
// repository.isForwardReviewExit's column set, but is defined independently
// here because board cannot import repository without a cycle — the two sets
// must change together.
func isQAForwardExit(to domain.TaskColumn) bool {
	switch to {
	case domain.TaskColumnPMUAT, domain.TaskColumnHumanUAT,
		domain.TaskColumnDone, domain.TaskColumnReleased:
		return true
	default:
		return false
	}
}

// scoreQARound turns one QA round's unscored test cases into score events for
// the agent that owned the in_qa column. forward=false is the round ending in
// need_revision, where only failed cases (bugs QA actually caught) count.
// forward=true is the round signing off forward, where passed/invalid cases
// are also confirmed verdicts. Cases already stamped scored_at are skipped so
// neither path ever counts the same case twice.
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

// ApplyReviewEscape charges a reviewing agent for approving something a human
// then rejected at the same gate.
func (st *ScoreTracker) ApplyReviewEscape(ctx context.Context, task domain.BoardTask, agentID uuid.UUID) {
	if st == nil || st.scores == nil {
		return
	}
	st.apply(ctx, task, agentID, domain.ScoreEventReviewEscape, domain.ScoreDeltaReviewEscape,
		"Human rejected a change the reviewer approved")
}

// blamed resolves the accountable agents, de-duplicated: one agent that both
// developed and tested a task is charged once, not twice.
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

// qaOwner resolves the agent that owned the in_qa column for a QA-scoring
// decision. Unlike blamed, it never falls back to the task's assignee: the
// assignee is normally the developer, and crediting/charging QA scores to the
// wrong agent because the span ledger is not wired would be worse than
// scoring nothing.
func (st *ScoreTracker) qaOwner(ctx context.Context, task domain.BoardTask) (uuid.UUID, bool) {
	if st.spans == nil {
		return uuid.Nil, false
	}
	owners, err := st.spans.OwnersForTask(ctx, task.ID)
	if err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("span owners lookup failed")
		return uuid.Nil, false
	}
	agentID, ok := owners["in_qa"]
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
