package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// LegacyModelSource reads repository_dependencies (migration 134), the table
// the project model replaced, so the one-time backfill can carry the human's
// earlier settings forward as overrides. Read-only: nothing here writes to
// it, and it stays in place only so this adapter (and the migration's own
// down file) can still read it.
type LegacyModelSource struct {
	pool *DB
}

func NewLegacyModelSource(pool *DB) *LegacyModelSource {
	return &LegacyModelSource{pool: pool}
}

var _ port.LegacyModelSource = (*LegacyModelSource)(nil)

func (s *LegacyModelSource) ListLegacyDependencies(ctx context.Context, repositoryID uuid.UUID) ([]domain.LegacyDependency, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, repository_id, target_kind, target_repository_id, target_sub_project_path,
			database_label, database_engine, database_env, database_host, database_port, database_name, note
		FROM repository_dependencies
		WHERE repository_id = $1
		ORDER BY created_at`, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("list legacy dependencies: %w", err)
	}
	defer rows.Close()

	var out []domain.LegacyDependency
	for rows.Next() {
		var d domain.LegacyDependency
		if err := rows.Scan(&d.ID, &d.RepositoryID, &d.TargetKind, &d.TargetRepositoryID, &d.TargetSubProjectPath,
			&d.DatabaseLabel, &d.DatabaseEngine, &d.DatabaseEnv, &d.DatabaseHost, &d.DatabasePort, &d.DatabaseName, &d.Note); err != nil {
			return nil, fmt.Errorf("scan legacy dependency: %w", err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list legacy dependencies: %w", err)
	}
	return out, nil
}
