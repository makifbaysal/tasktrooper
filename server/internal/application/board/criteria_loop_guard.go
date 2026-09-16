package board

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// CriteriaReader lists a task's acceptance criteria. port.TaskCriterionStore
// (via repository.Service), narrowed to the one read this guard makes — the
// same interface Runner.allCriteria consumes under the name
// taskCriteriaReader, given its own name here because the two packages must
// not share an unexported type across files that do not otherwise depend on
// each other.
type CriteriaReader interface {
	ListTaskCriteria(ctx context.Context, taskID uuid.UUID) ([]domain.AcceptanceCriterion, error)
}

// CriteriaLoopGuard is ReviewLoopGuard's shape, aimed at a different loop.
//
// ReviewLoopGuard closes "the same task keeps arriving in need_revision with
// nobody watching"; PipelineBounceGuard closes "the same commit fails CI
// forever". This one closes a third machine-talking-to-itself shape: a run
// exhausts its criteria-sweep rounds (criteria_sweep.go) with a criterion
// still open, gets marked Failed for it (see runner.go, isUnsettledCriteriaRun),
// the reconciler's existing retry_failed_run path (Reconciler.dispatchNeverStarted)
// dispatches it again exactly as it would a crash — and if the SAME criteria
// come back open three times running with no human ever having looked, that is
// not "not done yet", it is the same evidence the other two guards already
// act on: the unattended board has run out of ways to finish this on its own.
//
// It is invoked from the reconciler rather than the dispatcher, because
// unlike the other two loops this one is not detected on an incoming board
// event — a task sitting on three unsettled-criteria failures with no run in
// flight produces no event at all, so the reconciler's periodic sweep is the
// only place that ever looks again.
type CriteriaLoopGuard struct {
	events   TaskEventHistory
	parker   ResourceParker
	criteria CriteriaReader
	comments TaskCommenter
	parks    *ParkJournal
}

// NewCriteriaLoopGuard wires the guard. events is what tells it a human
// touched the card since the streak started; parker is what it acts with —
// without a parker there is nothing this guard can do, so Hold always fails
// open in that case, the same rule the other two guards follow.
func NewCriteriaLoopGuard(events TaskEventHistory, parker ResourceParker, criteria CriteriaReader) *CriteriaLoopGuard {
	return &CriteriaLoopGuard{events: events, parker: parker, criteria: criteria}
}

// SetCommenter attaches the store used to explain the park on the card.
// Nil-safe: without it the park still happens and still shows in the blocked
// badge.
func (g *CriteriaLoopGuard) SetCommenter(c TaskCommenter) {
	if g != nil {
		g.comments = c
	}
}

// SetParkJournal attaches the writer that records the park as a board move.
// Nil-safe: without it the card still reaches `blocked`, the timeline just
// does not show how it got there.
func (g *CriteriaLoopGuard) SetParkJournal(j *ParkJournal) {
	if g != nil {
		g.parks = j
	}
}

// Hold reports whether a task stuck on unsettled-criteria failures must be
// parked instead of retried again.
//
// runs is the window the caller already read to decide the task is at the
// retry cap — Reconciler.dispatchNeverStarted's maxConsecutiveFailedRuns most
// recent runs, newest first. The caller is expected to have already checked
// that every one of them is domain.TaskAgentRunStatusFailed; this method's own
// job is the two questions only it can answer: were they ALL failed for the
// same reason (unsettled criteria, not a crash), and has a human touched the
// card since the oldest of them started.
//
// It fails open — returns false, changes nothing — on every uncertainty: no
// parker wired, no event history, an unreadable history, fewer than
// maxConsecutiveFailedRuns runs to judge, or a mixed bag of failure reasons.
// A guard that parked on a bad read would strand a task that might otherwise
// have been retried straight back to a clean run.
func (g *CriteriaLoopGuard) Hold(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask, runs []domain.TaskAgentRun) bool {
	if g == nil || g.events == nil || g.parker == nil {
		return false
	}
	if len(runs) < maxConsecutiveFailedRuns || !allUnsettledCriteriaFailures(runs) {
		return false
	}
	oldest := runs[len(runs)-1].CreatedAt

	history, err := g.events.ListByTask(ctx, task.ID, reviewLoopHistoryDepth)
	if err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).
			Msg("criteria loop guard: reading board history failed, leaving the retry in place")
		return false
	}
	if at, ok := lastHumanEventAt(history); ok && !at.Before(oldest) {
		// A person has touched this card since (or during) the streak the
		// caller is judging — see the guard-wide reset rule in
		// ReviewLoopGuard.reviewLoopEntries. The task earns a fresh
		// maxConsecutiveFailedRuns attempts before this can fire again.
		return false
	}

	log.Warn().
		Str("task_id", task.ID.String()).
		Int("consecutive_unsettled_failures", len(runs)).
		Msg("criteria loop guard: same acceptance criteria left open run after run with no human input, parking it")

	open := g.openCriteria(ctx, task.ID)
	// Park FIRST, then explain — same ordering ReviewLoopGuard uses and for
	// the same reason: the commenter is repository.Service, whose AddComment
	// emits task.commented back into Dispatch. On a card still in
	// in_progress/todo/need_revision that comment would start the very
	// developer run this guard just refused to retry; once the card is
	// `blocked`, isDispatchSuspendedTask drops that dispatch and the comment
	// is inert.
	if !g.park(ctx, repositoryID, task, len(runs)) {
		return false
	}
	g.comment(ctx, repositoryID, task, len(runs), open)
	return true
}

// allUnsettledCriteriaFailures reports whether every run in the slice is a
// Failed run whose Summary carries the criteria-sweep marker
// (isUnsettledCriteriaRun) — as opposed to a crash, a max-iteration cutoff, or
// a git failure, which also leave a run Failed but are not this guard's loop.
func allUnsettledCriteriaFailures(runs []domain.TaskAgentRun) bool {
	if len(runs) == 0 {
		return false
	}
	for _, run := range runs {
		if !isUnsettledCriteriaRun(run) {
			return false
		}
	}
	return true
}

// openCriteria reads the task's still-open criteria for the park comment.
// Nil-safe reader: without one the comment names none, which still leaves the
// park itself and its reason on the card.
func (g *CriteriaLoopGuard) openCriteria(ctx context.Context, taskID uuid.UUID) []domain.AcceptanceCriterion {
	if g.criteria == nil {
		return nil
	}
	items, err := g.criteria.ListTaskCriteria(ctx, taskID)
	if err != nil {
		log.Warn().Err(err).Str("task_id", taskID.String()).
			Msg("criteria loop guard: reading acceptance criteria for the park comment failed")
		return nil
	}
	open := make([]domain.AcceptanceCriterion, 0, len(items))
	for _, c := range items {
		if !c.Settled() {
			open = append(open, c)
		}
	}
	return open
}

// comment names what is still open and why the board stopped retrying, the
// same shape ReviewLoopGuard.comment and PipelineBounceGuard.comment use.
func (g *CriteriaLoopGuard) comment(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask, runCount int, open []domain.AcceptanceCriterion) {
	if g.comments == nil {
		return
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("criteria loop: %d run in a row ended with the same acceptance criteria still open and no human input in between; parking for a human decision.\n\n", runCount))
	if len(open) > 0 {
		sb.WriteString("Still open:\n")
		for _, c := range open {
			sb.WriteString("- " + c.Text + "\n")
		}
		sb.WriteString("\n")
	}
	sb.WriteString("Kart `blocked` kolonunda bekliyor: kriterleri tamamlayın ya da cancel_criterion ile gerekçesiyle iptal edin, ardından kartı ilerletin.")
	if _, err := g.comments.AddComment(ctx, repositoryID, task.ID, domain.CreateTaskCommentRequest{
		AuthorType: "system",
		Content:    sb.String(),
	}); err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("criteria loop guard: park comment failed")
	}
}

// park moves the card into `blocked` on the human-decision resource and
// records the move, mirroring ReviewLoopGuard.park exactly — same resource,
// same journal, its own MoveReason so the timeline says which loop stopped it.
func (g *CriteriaLoopGuard) park(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask, runCount int) bool {
	if g.parker == nil || task.Column == domain.TaskColumnBlocked {
		return false
	}
	detail := fmt.Sprintf("criteria loop: %d runs in a row left the same acceptance criteria open — waiting for a human decision", runCount)
	previous, err := g.parker.BlockOnResource(ctx, repositoryID, task.ID, domain.ResourceHumanDecision, detail)
	if err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).
			Msg("criteria loop guard: parking the task failed; leaving the retry in place instead")
		return false
	}
	if g.parks != nil {
		g.parks.Record(ctx, repositoryID, task, previous,
			domain.ResourceHumanDecision, domain.MoveReasonCriteriaLoopParked)
	}
	return true
}
