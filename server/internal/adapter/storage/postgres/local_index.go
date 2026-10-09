package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// LocalIndexStore is the IndexStore over the executor's own cache database.
// workspace_indexes.repository_id references repositories, and this database
// holds no repository the user registered, so each repository key gets a
// placeholder row the first time it is indexed.
type LocalIndexStore struct {
	*IndexStore
	db *DB
}

var _ port.LocalIndexStore = (*LocalIndexStore)(nil)

func NewLocalIndexStore(db *DB) *LocalIndexStore {
	return &LocalIndexStore{IndexStore: NewIndexStore(db), db: db}
}

// localIndexRootPrefix keeps the placeholder's root_path (UNIQUE, NOT NULL)
// out of the path namespace: nothing reads a checkout from it.
const localIndexRootPrefix = "local-index:"

func (s *LocalIndexStore) EnsureRepository(ctx context.Context, id uuid.UUID, repoKey string) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO repositories (id, name, root_path)
		VALUES ($1, $2, $3)
		ON CONFLICT (id) DO NOTHING
	`, id, repoKey, localIndexRootPrefix+id.String())
	if err != nil {
		return fmt.Errorf("register repository %q: %w", repoKey, err)
	}
	return nil
}
