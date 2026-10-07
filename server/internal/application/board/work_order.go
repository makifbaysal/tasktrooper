package board

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type BlockerReader interface {
	ListBlockingSources(ctx context.Context, targetTaskID uuid.UUID) ([]domain.BoardTask, error)
}

type ResourceParker interface {
	BlockOnResource(ctx context.Context, repositoryID, taskID uuid.UUID, resource, detail string) (domain.TaskColumn, error)
}

// The work_order park never moves board_column: a blocks wait is on another card, not on anything outside.
type WorkOrderParker interface {
	MarkWorkOrderWaiting(ctx context.Context, repositoryID, taskID uuid.UUID, detail string) error
}

type WorkOrder struct {
	relations BlockerReader
	tasks     WorkOrderParker
	comments  TaskCommenter
	parked    func()
}

func NewWorkOrder(relations BlockerReader, tasks WorkOrderParker) *WorkOrder {
	return &WorkOrder{relations: relations, tasks: tasks}
}

func (w *WorkOrder) SetCommenter(c TaskCommenter) {
	if w != nil {
		w.comments = c
	}
}

// SetParkedHook runs after every park is written. A blocker that finished
// between the dispatcher's check and the park woke no one (the dependent was
// not parked yet), so the sweeper is asked to look once the park exists.
func (w *WorkOrder) SetParkedHook(fn func()) {
	if w != nil {
		w.parked = fn
	}
}

func (w *WorkOrder) Blockers(ctx context.Context, taskID uuid.UUID) ([]domain.BoardTask, error) {
	if w == nil || w.relations == nil || taskID == uuid.Nil {
		return nil, nil
	}
	return w.relations.ListBlockingSources(ctx, taskID)
}

// Already-parked is a no-op, not an optimisation: the park comment re-enters Dispatch for the same event and would recurse.
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
			Content:    workOrderParkComment(blockers),
		}); err != nil {
			log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("work order: park comment failed")
		}
	}
	log.Info().Str("task_id", task.ID.String()).Str("resource", domain.ResourceWorkOrder).
		Str("detail", detail).Msg("task parked: its work order is not satisfied yet")
	if w.parked != nil {
		w.parked()
	}
	return nil
}

func workOrderParkComment(blockers []domain.BoardTask) string {
	return prompt.Text(workOrderParkPrefixKey) + " " + strings.Join(blockerLabels(blockers), ", ") +
		" " + prompt.Text(workOrderParkSuffixKey)
}

func blockerLabels(blockers []domain.BoardTask) []string {
	out := make([]string, 0, len(blockers))
	for _, b := range blockers {
		out = append(out, fmt.Sprintf("%s [%s]", domain.RelationLabel(b.Key, b.Title, b.ID), b.Column))
	}
	return out
}

// !wfOK fails closed: an unreadable workflow must not let a task slip past its work order.
func workOrderGateApplies(wf domain.Workflow, wfOK bool, input DispatchInput) bool {
	if wfOK && !wf.Has(input.Task.Column, domain.BehaviourBlockOnDependencies) {
		return false
	}
	if input.Payload != nil {
		if resource, _ := input.Payload[domain.EventPayloadResumedResource].(string); resource == domain.ResourceWorkOrder {
			return false
		}
	}
	return true
}
