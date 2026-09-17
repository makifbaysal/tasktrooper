package board

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// BlockerReader answers "which tasks must still finish before this one may be
// worked on". It is port.TaskRelationStore narrowed to the one method the
// dispatcher needs, so the board package does not take a dependency on relation
// writing to read an order.
type BlockerReader interface {
	ListBlockingSources(ctx context.Context, targetTaskID uuid.UUID) ([]domain.BoardTask, error)
}

// ResourceParker parks a task on a named resource. port.BoardTaskStore,
// narrowed. Used by the loop guards, which park on domain.ResourceHumanDecision
// — a resource whose park DOES move board_column, unlike work_order's.
type ResourceParker interface {
	BlockOnResource(ctx context.Context, repositoryID, taskID uuid.UUID, resource, detail string) (domain.TaskColumn, error)
}

// WorkOrderParker marks a task waiting on the work_order resource.
// port.BoardTaskStore, narrowed to the one write Park needs — the one that
// does NOT move board_column, because a `blocks` wait is a wait on another
// card on the same board, not on anything outside it.
type WorkOrderParker interface {
	MarkWorkOrderWaiting(ctx context.Context, repositoryID, taskID uuid.UUID, detail string) error
}

// WorkOrder is the enforcement of `blocks`.
//
// The relation has existed since migration 022 and, until now, one thing read
// it: repository.Service.validateMoveAllowed refused a MOVE into todo or
// in_progress. That caught a human dragging a card and nothing else — a task
// CREATED straight into todo with an open blocker, a reconciler sweep of a task
// that never started, a sweeper handing a task back, an assignment event: every
// one of those went to the dispatcher and started a run on work that was
// explicitly ordered to wait.
//
// So the gate moved to where runs are actually started. It is a park rather
// than a refusal because a refusal is invisible: the card would sit in `todo`
// looking exactly like an unstarted one, with nobody able to say why no agent
// picked it up. Parking states it — on the card's badge and in a comment
// naming every blocker, without moving the card out of todo/in_progress — and
// gives the release a mechanism (WorkOrderSweeper) instead of leaving it to
// whoever next touches the task.
type WorkOrder struct {
	relations BlockerReader
	tasks     WorkOrderParker
	comments  TaskCommenter
}

func NewWorkOrder(relations BlockerReader, tasks WorkOrderParker) *WorkOrder {
	return &WorkOrder{relations: relations, tasks: tasks}
}

// SetCommenter attaches the store used to explain a park on the card. Nil-safe:
// without it the park still happens and still shows its reason in the card's
// blocked badge.
func (w *WorkOrder) SetCommenter(c TaskCommenter) {
	if w != nil {
		w.comments = c
	}
}

// Blockers returns the unfinished tasks standing in front of this one, or nil
// when it is free to start.
//
// A read failure is reported, not swallowed. The dispatcher treats it as "do
// not start this run": an unknown order is the case the gate exists for, and
// starting the work would be the one outcome that cannot be undone by the next
// sweep.
func (w *WorkOrder) Blockers(ctx context.Context, taskID uuid.UUID) ([]domain.BoardTask, error) {
	if w == nil || w.relations == nil || taskID == uuid.Nil {
		return nil, nil
	}
	return w.relations.ListBlockingSources(ctx, taskID)
}

// Park marks the task as waiting on its blockers, in place — the task's
// column does not change — and says on the card what it is waiting for.
//
// Already-parked is a no-op, not just an optimisation: the comment it posts
// re-enters Dispatch for the same task.commented event (Service.AddComment
// calls s.emit synchronously), and since the column never moves out of
// {todo, in_progress} the work-order gate is true again on that re-entrant
// call. Without this guard that recurses until the stack overflows — the old
// BlockOnResource path was safe only because it moved the column out of the
// gated set before the comment fired, and nothing replaces that here except
// this check.
func (w *WorkOrder) Park(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask, blockers []domain.BoardTask) error {
	if w == nil || w.tasks == nil {
		return nil
	}
	if task.BlockedResource == domain.ResourceWorkOrder {
		return nil
	}
	detail := "waiting for " + strings.Join(blockerLabels(blockers), ", ") + " to finish"
	if err := w.tasks.MarkWorkOrderWaiting(ctx, repositoryID, task.ID, detail); err != nil {
		return err
	}
	if w.comments != nil {
		if _, err := w.comments.AddComment(ctx, repositoryID, task.ID, domain.CreateTaskCommentRequest{
			AuthorType: "system",
			Content: "Work order: this task is parked until " + strings.Join(blockerLabels(blockers), ", ") +
				" reach done or released. It is picked up automatically when they do — nothing to do here.",
		}); err != nil {
			log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("work order: park comment failed")
		}
	}
	log.Info().Str("task_id", task.ID.String()).Str("resource", domain.ResourceWorkOrder).
		Str("detail", detail).Msg("task parked: its work order is not satisfied yet")
	return nil
}

// blockerLabels renders the blockers the way both the card and the comment name
// them: "T-12 (API migration) [in_progress]".
func blockerLabels(blockers []domain.BoardTask) []string {
	out := make([]string, 0, len(blockers))
	for _, b := range blockers {
		out = append(out, fmt.Sprintf("%s [%s]", domain.RelationLabel(b.Key, b.Title, b.ID), b.Column))
	}
	return out
}

// workOrderGateApplies reports whether this dispatch is one the work order has
// anything to say about.
//
// Only the two columns where work STARTS. A blocks relation is a statement
// about who writes code first; once a task has reached code_review its code is
// written, and parking it there would strand a finished change behind a
// dependency the change no longer has. Both are gated here even though
// repository.Service.validateMoveAllowed only refuses a manual move into
// in_progress: a manual move into todo is allowed (queueing, not starting) and
// this is what actually keeps an agent from picking the work up early once it
// gets there — parked in place, blockers named on the card, released by
// WorkOrderSweeper the moment they land.
//
// The resume payload check is what stops the sweeper's own hand-back from being
// re-parked before its dispatch reaches an agent: the sweeper only releases a
// task whose blockers have landed, and re-reading the graph in the same instant
// would at best confirm that and at worst re-park it on a stale read.
func workOrderGateApplies(input DispatchInput) bool {
	switch input.Task.Column {
	case domain.TaskColumnTodo, domain.TaskColumnInProgress:
	default:
		return false
	}
	if input.Payload != nil {
		if resource, _ := input.Payload[domain.EventPayloadResumedResource].(string); resource == domain.ResourceWorkOrder {
			return false
		}
	}
	return true
}
