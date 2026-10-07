package runtime

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type prunableIndexStore struct {
	port.IndexStore
}

func (prunableIndexStore) ListBranchIndexes(context.Context) ([]domain.WorkspaceIndex, error) {
	return nil, nil
}

func (prunableIndexStore) DeleteIndex(context.Context, uuid.UUID) error { return nil }

func TestBranchIndexPruningOnlyOnADatabaseThisProcessStarted(t *testing.T) {
	store := prunableIndexStore{}
	tests := []struct {
		name     string
		store    port.IndexStore
		embedded bool
		prunes   bool
	}{
		{"embedded database", store, true, true},
		{"DATABASE_URL: another host may serve it", store, false, false},
		{"store that cannot prune", struct{ port.IndexStore }{}, true, false},
		{"no index store", nil, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := branchIndexPruning(tt.store, Options{EmbeddedPostgres: tt.embedded})
			assert.Equal(t, tt.prunes, got != nil)
		})
	}
}
