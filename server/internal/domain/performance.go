package domain

import (
	"time"

	"github.com/google/uuid"
)

const (
	ScoreDeltaRevisionRequested float64 = -10.0
	ScoreDeltaPMUATFailed       float64 = -5.0
	// A defect that reached the human escaped every automated gate, so it
	// costs more than one the PM caught first.
	ScoreDeltaHumanUATFailed float64 = -8.0
	// ScoreDeltaReviewEscape is charged to a reviewer whose approval a human
	// overturned at the same gate: the review itself, not just the code, failed.
	ScoreDeltaReviewEscape  float64 = -10.0
	ScoreDeltaTaskCompleted float64 = +5.0
	ScoreDeltaTaskReleased  float64 = +5.0
	// ScoreDeltaQABugFound rewards QA for a bug it actually caught: a
	// one-time credit per failed case, deliberately half of the developer's
	// revision penalty so a round with a couple of bugs does not outweigh
	// task_completed.
	ScoreDeltaQABugFound float64 = +6.0
	// ScoreDeltaQAValidScenarioConfirmed/ScoreDeltaQAInvalidScenarioConfirmed
	// are charged per case when QA's round exits forward with a signed-off
	// verdict; kept small so a whole round never outweighs a single event.
	ScoreDeltaQAValidScenarioConfirmed   float64 = +1.0
	ScoreDeltaQAInvalidScenarioConfirmed float64 = -1.0
	// ScoreDeltaQATaskTested/ScoreDeltaPMUATCompleted credit QA/PM with the same
	// "a unit of work finished" weight as the developer's own completion
	// credit, not the smaller per-scenario QA deltas above.
	ScoreDeltaQATaskTested   float64 = +5.0
	ScoreDeltaPMUATCompleted float64 = +5.0

	ScoreEventRevisionRequested = "revision_requested"
	ScoreEventPMUATFailed       = "pm_uat_failed"
	ScoreEventHumanUATFailed    = "human_uat_failed"
	ScoreEventTaskCompleted     = "task_completed"
	ScoreEventTaskReleased      = "task_released"
	// ScoreEventReviewEscape marks a reviewing agent approving a change a human
	// then rejected at the same review gate.
	ScoreEventReviewEscape               = "review_escape"
	ScoreEventQABugFound                 = "qa_bug_found"
	ScoreEventQAValidScenarioConfirmed   = "qa_valid_scenario_confirmed"
	ScoreEventQAInvalidScenarioConfirmed = "qa_invalid_scenario_confirmed"
	ScoreEventQATaskTested               = "qa_task_tested"
	ScoreEventPMUATCompleted             = "pm_uat_completed"
)

type AgentPerformanceScore struct {
	ID          uuid.UUID `json:"id"`
	AgentID     uuid.UUID `json:"agent_id"`
	Score       float64   `json:"score"`
	RunsTotal   int       `json:"runs_total"`
	RunsPassed  int       `json:"runs_passed"`
	RunsRevised int       `json:"runs_revised"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type AgentScoreEvent struct {
	ID         uuid.UUID  `json:"id"`
	AgentID    uuid.UUID  `json:"agent_id"`
	TaskID     *uuid.UUID `json:"task_id,omitempty"`
	EventType  string     `json:"event_type"`
	Delta      float64    `json:"delta"`
	ScoreAfter float64    `json:"score_after"`
	Reason     string     `json:"reason,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

type ApplyScoreInput struct {
	AgentID   uuid.UUID
	TaskID    *uuid.UUID
	EventType string
	Delta     float64
	Reason    string
}
