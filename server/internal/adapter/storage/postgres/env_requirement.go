package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type EnvRequirementStore struct {
	pool *DB
}

var _ port.EnvRequirementStore = (*EnvRequirementStore)(nil)

func NewEnvRequirementStore(pool *DB) *EnvRequirementStore {
	return &EnvRequirementStore{pool: pool}
}

const envRequirementColumns = `id, repository_id, component_id, name, kind, value, description, source, task_id, created_at, updated_at`

func scanEnvRequirement(row pgx.Row) (domain.EnvRequirement, error) {
	var r domain.EnvRequirement
	var kind string
	err := row.Scan(&r.ID, &r.RepositoryID, &r.ComponentID, &r.Name, &kind, &r.Value, &r.Description, &r.Source, &r.TaskID, &r.CreatedAt, &r.UpdatedAt)
	r.Kind = domain.EnvVarKind(kind)
	return r, err
}

func (s *EnvRequirementStore) ListEnvRequirements(ctx context.Context, repositoryID uuid.UUID) ([]domain.EnvRequirement, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+envRequirementColumns+`
		FROM env_requirements
		WHERE repository_id = $1
		ORDER BY name, component_id NULLS FIRST
	`, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("list env requirements: %w", err)
	}
	defer rows.Close()
	var out []domain.EnvRequirement
	for rows.Next() {
		r, err := scanEnvRequirement(rows)
		if err != nil {
			return nil, fmt.Errorf("scan env requirement: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// UpsertEnvRequirement's conflict update is skipped for an agent write over a
// human row: what a person decided about a variable (that it is optional, or
// that it is a value) is not the agent's to take back. The final SELECT reads
// whichever row stands.
func (s *EnvRequirementStore) UpsertEnvRequirement(ctx context.Context, r domain.EnvRequirement) (domain.EnvRequirement, error) {
	source := r.Source
	if source == "" {
		source = domain.EnvSourceAgent
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO env_requirements (repository_id, component_id, name, kind, value, description, source, task_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (repository_id, component_id, name) DO UPDATE SET
			kind        = EXCLUDED.kind,
			value       = EXCLUDED.value,
			description = CASE WHEN EXCLUDED.description <> '' THEN EXCLUDED.description ELSE env_requirements.description END,
			source      = EXCLUDED.source,
			task_id     = COALESCE(EXCLUDED.task_id, env_requirements.task_id),
			updated_at  = now()
		WHERE env_requirements.source <> 'human' OR EXCLUDED.source = 'human'
	`, r.RepositoryID, r.ComponentID, r.Name, string(r.Kind), r.Value, r.Description, source, r.TaskID); err != nil {
		return domain.EnvRequirement{}, fmt.Errorf("upsert env requirement: %w", err)
	}
	row := s.pool.QueryRow(ctx, `
		SELECT `+envRequirementColumns+`
		FROM env_requirements
		WHERE repository_id = $1 AND component_id IS NOT DISTINCT FROM $2 AND name = $3
	`, r.RepositoryID, r.ComponentID, r.Name)
	stored, err := scanEnvRequirement(row)
	if err != nil {
		return domain.EnvRequirement{}, fmt.Errorf("read env requirement: %w", err)
	}
	return stored, nil
}
