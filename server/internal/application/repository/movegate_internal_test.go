package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// fixedBlockerStore overrides ListBlockingSources to return a fixed list,
// standing in for what ListBlockingSources's done/released SQL filter would
// return for a real blocker in a given state.
type fixedBlockerStore struct {
	*graphRelationStore
	blockers []domain.BoardTask
}

func (f *fixedBlockerStore) ListBlockingSources(context.Context, uuid.UUID) ([]domain.BoardTask, error) {
	return f.blockers, nil
}

// A manual move into todo with an open blocker must be refused exactly like
// in_progress: the dispatch-time park (board.WorkOrder) only catches a task
// that reaches the dispatcher, and a human or an agent dragging the card
// straight into todo bypasses that entirely.
func TestValidateMoveAllowedRefusesTodoWithAnOpenBlocker(t *testing.T) {
	svc := &Service{relations: &fixedBlockerStore{
		graphRelationStore: newGraphRelations(),
		blockers:           []domain.BoardTask{{ID: uuid.New(), Key: "T-5", Title: "API migration", Column: domain.TaskColumnInProgress}},
	}}

	err := svc.validateMoveAllowed(context.Background(), uuid.New(), domain.TaskColumnTodo)

	require.Error(t, err)
	require.ErrorContains(t, err, "blocked until these are done")
	var gateErr *domain.WorkOrderGateError
	require.True(t, errors.As(err, &gateErr))
	require.Equal(t, domain.TaskColumnTodo, gateErr.Target)
	require.Len(t, gateErr.Blockers, 1)
	require.Equal(t, "T-5", gateErr.Blockers[0].Key)
}

func TestValidateMoveAllowedRefusesInProgressWithAnOpenBlocker(t *testing.T) {
	svc := &Service{relations: &fixedBlockerStore{
		graphRelationStore: newGraphRelations(),
		blockers:           []domain.BoardTask{{ID: uuid.New(), Key: "T-5", Column: domain.TaskColumnTodo}},
	}}

	err := svc.validateMoveAllowed(context.Background(), uuid.New(), domain.TaskColumnInProgress)

	require.Error(t, err)
	require.ErrorContains(t, err, "blocked until these are done")
	var gateErr *domain.WorkOrderGateError
	require.True(t, errors.As(err, &gateErr), "the HTTP layer needs a typed error to render task_blocked_by_dependency")
	require.Equal(t, domain.TaskColumnInProgress, gateErr.Target)
}

func TestValidateMoveAllowedAllowsBothColumnsWhenBlockerIsDone(t *testing.T) {
	svc := &Service{relations: &fixedBlockerStore{graphRelationStore: newGraphRelations(), blockers: nil}}

	require.NoError(t, svc.validateMoveAllowed(context.Background(), uuid.New(), domain.TaskColumnTodo))
	require.NoError(t, svc.validateMoveAllowed(context.Background(), uuid.New(), domain.TaskColumnInProgress))
}
