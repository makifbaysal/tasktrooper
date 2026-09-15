package repository

import (
	"context"
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

func TestValidateMoveAllowedLetsAnOpenBlockerIntoTodo(t *testing.T) {
	svc := &Service{relations: &fixedBlockerStore{
		graphRelationStore: newGraphRelations(),
		blockers:           []domain.BoardTask{{ID: uuid.New(), Key: "T-5", Column: domain.TaskColumnTodo}},
	}}

	err := svc.validateMoveAllowed(context.Background(), uuid.New(), domain.TaskColumnTodo)

	require.NoError(t, err)
}

func TestValidateMoveAllowedRefusesInProgressWithAnOpenBlocker(t *testing.T) {
	svc := &Service{relations: &fixedBlockerStore{
		graphRelationStore: newGraphRelations(),
		blockers:           []domain.BoardTask{{ID: uuid.New(), Key: "T-5", Column: domain.TaskColumnTodo}},
	}}

	err := svc.validateMoveAllowed(context.Background(), uuid.New(), domain.TaskColumnInProgress)

	require.Error(t, err)
	require.ErrorContains(t, err, "blocked until these are done")
}

func TestValidateMoveAllowedAllowsBothColumnsWhenBlockerIsDone(t *testing.T) {
	svc := &Service{relations: &fixedBlockerStore{graphRelationStore: newGraphRelations(), blockers: nil}}

	require.NoError(t, svc.validateMoveAllowed(context.Background(), uuid.New(), domain.TaskColumnTodo))
	require.NoError(t, svc.validateMoveAllowed(context.Background(), uuid.New(), domain.TaskColumnInProgress))
}
