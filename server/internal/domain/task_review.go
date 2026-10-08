package domain

import (
	"time"

	"github.com/google/uuid"
)

// ReviewVerdictPending marks a required reviewer that has not decided yet in
// the current round; it is never stored.
const ReviewVerdictPending = "pending"

// QuorumReviewColumn reports whether every subscriber of col is a required
// reviewer whose verdict the card waits for. Only code_review: it is the one
// column an agent approves its way out of, and the one where a second opinion
// (security next to design) is a different review rather than the same one
// twice — two QA agents testing the same card is not.
func QuorumReviewColumn(col TaskColumn) bool {
	return col == TaskColumnCodeReview
}

// TaskReviewVerdict is one reviewer's decision in one visit to a review column.
type TaskReviewVerdict struct {
	TaskID    uuid.UUID  `json:"task_id"`
	SpanID    uuid.UUID  `json:"span_id"`
	AgentID   *uuid.UUID `json:"agent_id,omitempty"`
	AgentName string     `json:"agent_name"`
	Verdict   string     `json:"verdict"`
	DecidedAt time.Time  `json:"decided_at"`
}

type ReviewerRef struct {
	ID   uuid.UUID
	Name string
}

// ReviewerStatus is a reviewer's standing in one round: approve, reject, or
// pending while a required reviewer has not decided.
type ReviewerStatus struct {
	AgentID   *uuid.UUID `json:"agent_id,omitempty"`
	AgentName string     `json:"agent_name"`
	Verdict   string     `json:"verdict"`
	DecidedAt *time.Time `json:"decided_at,omitempty"`
	Required  bool       `json:"required"`
}

const (
	ReviewRoundOpen     = "open"
	ReviewRoundApproved = "approved"
	ReviewRoundRejected = "rejected"
	// ReviewRoundClosed is a round that ended without a verdict from every
	// reviewer, e.g. a person moved the card themselves.
	ReviewRoundClosed = "closed"
)

// TaskReviewRound is one pass of a task through a review column.
type TaskReviewRound struct {
	Round     int              `json:"round"`
	EnteredAt time.Time        `json:"entered_at"`
	LeftAt    *time.Time       `json:"left_at,omitempty"`
	Outcome   string           `json:"outcome"`
	Reviewers []ReviewerStatus `json:"reviewers"`
}

type TaskReviews struct {
	Column string            `json:"column"`
	Rounds []TaskReviewRound `json:"rounds"`
}

// ReviewRoundSpans groups a task's visits to column into review rounds. spans
// must be ordered by entered_at. A park to blocked interrupts a review without
// changing the code under it, so the visits on either side of one belong to
// the same round; a move to any other column ends the round.
func ReviewRoundSpans(spans []TaskColumnSpan, column string) [][]TaskColumnSpan {
	var rounds [][]TaskColumnSpan
	open := false
	for _, sp := range spans {
		switch sp.BoardColumn {
		case column:
			if open {
				rounds[len(rounds)-1] = append(rounds[len(rounds)-1], sp)
				continue
			}
			rounds = append(rounds, []TaskColumnSpan{sp})
			open = true
		case string(TaskColumnBlocked):
		default:
			open = false
		}
	}
	return rounds
}

// LatestVerdictPerReviewer keeps each reviewer's most recent verdict among
// those given in spanIDs: a reviewer woken again in the same round (a human
// comment, an unpark) may change its mind, and only the last word counts.
func LatestVerdictPerReviewer(verdicts []TaskReviewVerdict, spanIDs map[uuid.UUID]bool) []TaskReviewVerdict {
	latest := make(map[string]int)
	var out []TaskReviewVerdict
	for _, v := range verdicts {
		if !spanIDs[v.SpanID] {
			continue
		}
		key := v.AgentName
		if v.AgentID != nil {
			key = v.AgentID.String()
		}
		if i, ok := latest[key]; ok {
			if v.DecidedAt.After(out[i].DecidedAt) {
				out[i] = v
			}
			continue
		}
		latest[key] = len(out)
		out = append(out, v)
	}
	return out
}
