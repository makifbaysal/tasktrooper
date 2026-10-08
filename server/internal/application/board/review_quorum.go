package board

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// ReviewQuorum turns an agent's move out of a quorum review column into one
// reviewer's verdict: the card leaves only once every required reviewer (each
// enabled subscriber of the column) has decided, to need_revision when any of
// them rejected and to the approver's exit otherwise.
type ReviewQuorum struct {
	store     port.TaskReviewStore
	workflows port.WorkflowReader
}

func NewReviewQuorum(store port.TaskReviewStore, workflows port.WorkflowReader) *ReviewQuorum {
	return &ReviewQuorum{store: store, workflows: workflows}
}

type QuorumDecision struct {
	// Target is where the card goes when Hold is false; it differs from the
	// requested column when an approval completes a round someone rejected.
	Target domain.TaskColumn
	Hold   bool
	// Pending names the required reviewers still to decide when Hold is set.
	Pending []string
}

func (q *ReviewQuorum) Decide(ctx context.Context, task domain.BoardTask, from, to domain.TaskColumn, actor domain.TaskActor, actorAgentID *uuid.UUID) (QuorumDecision, error) {
	pass := QuorumDecision{Target: to}
	if q == nil || q.store == nil || actor != domain.TaskActorAgent || actorAgentID == nil || !domain.QuorumReviewColumn(from) {
		return pass, nil
	}
	verdict, ok := q.verdictFor(ctx, task.TaskType, from, to)
	if !ok {
		return pass, nil
	}
	round, open, err := q.currentRound(ctx, task.ID, from)
	if err != nil {
		return QuorumDecision{}, err
	}
	if open == nil {
		log.Warn().Str("task_id", task.ID.String()).Str("column", string(from)).
			Msg("review quorum: no open span for the column the card is leaving, letting the move through")
		return pass, nil
	}
	if err := q.store.RecordReviewVerdict(ctx, domain.TaskReviewVerdict{
		TaskID: task.ID, SpanID: open.ID, AgentID: actorAgentID, Verdict: verdict,
	}); err != nil {
		return QuorumDecision{}, err
	}

	required, err := q.store.RequiredReviewers(ctx, string(from), string(task.TaskType))
	if err != nil {
		return QuorumDecision{}, err
	}
	if len(required) < 2 {
		return pass, nil
	}
	decided, err := q.roundVerdicts(ctx, task.ID, round)
	if err != nil {
		return QuorumDecision{}, err
	}
	byAgent := make(map[uuid.UUID]string, len(decided))
	rejected := false
	for _, v := range decided {
		if v.AgentID != nil {
			byAgent[*v.AgentID] = v.Verdict
		}
		if v.Verdict == domain.ReviewVerdictReject {
			rejected = true
		}
	}
	var pending []string
	for _, r := range required {
		if _, ok := byAgent[r.ID]; !ok {
			pending = append(pending, r.Name)
		}
	}
	if len(pending) > 0 {
		return QuorumDecision{Target: from, Hold: true, Pending: pending}, nil
	}
	if rejected {
		return QuorumDecision{Target: domain.TaskColumnNeedRevision}, nil
	}
	return pass, nil
}

// HasDecided reports whether agentID already gave a verdict in the current
// round of column — a reviewer whose move was held has finished its review and
// must not be asked for it again.
func (q *ReviewQuorum) HasDecided(ctx context.Context, taskID uuid.UUID, column domain.TaskColumn, agentID uuid.UUID) bool {
	if q == nil || q.store == nil || !domain.QuorumReviewColumn(column) {
		return false
	}
	round, open, err := q.currentRound(ctx, taskID, column)
	if err != nil || open == nil {
		return false
	}
	decided, err := q.roundVerdicts(ctx, taskID, round)
	if err != nil {
		return false
	}
	for _, v := range decided {
		if v.AgentID != nil && *v.AgentID == agentID {
			return true
		}
	}
	return false
}

// Approvers is every reviewer whose latest verdict in the current round of
// column is an approval.
func (q *ReviewQuorum) Approvers(ctx context.Context, taskID uuid.UUID, column domain.TaskColumn) []uuid.UUID {
	if q == nil || q.store == nil || !domain.QuorumReviewColumn(column) {
		return nil
	}
	round, open, err := q.currentRound(ctx, taskID, column)
	if err != nil || open == nil {
		return nil
	}
	decided, err := q.roundVerdicts(ctx, taskID, round)
	if err != nil {
		return nil
	}
	var out []uuid.UUID
	for _, v := range decided {
		if v.AgentID != nil && v.Verdict == domain.ReviewVerdictApprove {
			out = append(out, *v.AgentID)
		}
	}
	return out
}

// TaskReviews is every round the task spent in the code_review column, with
// each reviewer's verdict; the open round also lists the required reviewers
// that have not decided yet.
func (q *ReviewQuorum) TaskReviews(ctx context.Context, task domain.BoardTask) (domain.TaskReviews, error) {
	column := domain.TaskColumnCodeReview
	out := domain.TaskReviews{Column: string(column), Rounds: []domain.TaskReviewRound{}}
	if q == nil || q.store == nil {
		return out, nil
	}
	spans, err := q.store.TaskSpans(ctx, task.ID)
	if err != nil {
		return out, err
	}
	verdicts, err := q.store.ListReviewVerdicts(ctx, task.ID)
	if err != nil {
		return out, err
	}
	rounds := domain.ReviewRoundSpans(spans, string(column))
	for i, round := range rounds {
		ids := spanIDs(round)
		decided := domain.LatestVerdictPerReviewer(verdicts, ids)
		last := round[len(round)-1]
		isOpen := last.LeftAt == nil && task.Column == column
		entry := domain.TaskReviewRound{Round: i + 1, EnteredAt: round[0].EnteredAt, LeftAt: last.LeftAt}

		var required []domain.ReviewerRef
		if isOpen {
			required, err = q.store.RequiredReviewers(ctx, string(column), string(task.TaskType))
			if err != nil {
				return out, err
			}
		}
		requiredIDs := make(map[uuid.UUID]bool, len(required))
		for _, r := range required {
			requiredIDs[r.ID] = true
		}
		seen := make(map[uuid.UUID]bool, len(decided))
		rejected := false
		for _, v := range decided {
			at := v.DecidedAt
			status := domain.ReviewerStatus{AgentID: v.AgentID, AgentName: v.AgentName, Verdict: v.Verdict, DecidedAt: &at}
			if v.AgentID != nil {
				seen[*v.AgentID] = true
				status.Required = requiredIDs[*v.AgentID]
			}
			if v.Verdict == domain.ReviewVerdictReject {
				rejected = true
			}
			entry.Reviewers = append(entry.Reviewers, status)
		}
		pending := false
		for _, r := range required {
			if seen[r.ID] {
				continue
			}
			id := r.ID
			pending = true
			entry.Reviewers = append(entry.Reviewers, domain.ReviewerStatus{
				AgentID: &id, AgentName: r.Name, Verdict: domain.ReviewVerdictPending, Required: true,
			})
		}
		switch {
		case isOpen && (pending || len(decided) == 0):
			entry.Outcome = domain.ReviewRoundOpen
		case rejected:
			entry.Outcome = domain.ReviewRoundRejected
		case len(decided) > 0:
			entry.Outcome = domain.ReviewRoundApproved
		default:
			entry.Outcome = domain.ReviewRoundClosed
		}
		if entry.Reviewers == nil {
			entry.Reviewers = []domain.ReviewerStatus{}
		}
		out.Rounds = append(out.Rounds, entry)
	}
	return out, nil
}

// verdictFor maps a reviewer's requested move onto approve/reject; a move to
// anything but need_revision or the stage's own exit is not a verdict.
func (q *ReviewQuorum) verdictFor(ctx context.Context, taskType domain.TaskType, from, to domain.TaskColumn) (string, bool) {
	if to == domain.TaskColumnNeedRevision {
		return domain.ReviewVerdictReject, true
	}
	exit := domain.TaskColumnReadyForQA
	if q.workflows != nil {
		if wf, err := q.workflows.Workflow(ctx, taskType); err == nil {
			if target, ok := reviewExitColumn(wf, from); ok {
				exit = target
			}
		}
	}
	if to == exit {
		return domain.ReviewVerdictApprove, true
	}
	return "", false
}

func (q *ReviewQuorum) currentRound(ctx context.Context, taskID uuid.UUID, column domain.TaskColumn) ([]domain.TaskColumnSpan, *domain.TaskColumnSpan, error) {
	spans, err := q.store.TaskSpans(ctx, taskID)
	if err != nil {
		return nil, nil, fmt.Errorf("review quorum: spans: %w", err)
	}
	rounds := domain.ReviewRoundSpans(spans, string(column))
	if len(rounds) == 0 {
		return nil, nil, nil
	}
	round := rounds[len(rounds)-1]
	last := round[len(round)-1]
	if last.LeftAt != nil {
		return round, nil, nil
	}
	return round, &last, nil
}

func (q *ReviewQuorum) roundVerdicts(ctx context.Context, taskID uuid.UUID, round []domain.TaskColumnSpan) ([]domain.TaskReviewVerdict, error) {
	verdicts, err := q.store.ListReviewVerdicts(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("review quorum: verdicts: %w", err)
	}
	return domain.LatestVerdictPerReviewer(verdicts, spanIDs(round)), nil
}

func spanIDs(spans []domain.TaskColumnSpan) map[uuid.UUID]bool {
	ids := make(map[uuid.UUID]bool, len(spans))
	for _, sp := range spans {
		ids[sp.ID] = true
	}
	return ids
}
