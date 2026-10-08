package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type DesignSystemStore struct {
	pool *DB
}

func NewDesignSystemStore(pool *DB) *DesignSystemStore {
	return &DesignSystemStore{pool: pool}
}

var _ port.DesignSystemStore = (*DesignSystemStore)(nil)

const designSystemCols = `id, scope, project_id, repository_id, version, status, design_md, tokens, inventory_md,
	rationale, source_task_id, source_task_key, created_by, created_at, updated_at, approved_at,
	(SELECT t.repository_id FROM board_tasks t WHERE t.id = design_systems.source_task_id)`

func scanDesignSystem(row pgx.Row) (domain.DesignSystem, error) {
	var d domain.DesignSystem
	var tokens []byte
	if err := row.Scan(
		&d.ID, &d.Scope, &d.ProjectID, &d.RepositoryID, &d.Version, &d.Status, &d.DesignMD, &tokens, &d.InventoryMD,
		&d.Rationale, &d.SourceTaskID, &d.SourceTaskKey, &d.CreatedBy, &d.CreatedAt, &d.UpdatedAt, &d.ApprovedAt,
		&d.SourceTaskRepositoryID,
	); err != nil {
		return domain.DesignSystem{}, err
	}
	d.Tokens = tokens
	if len(d.Tokens) == 0 {
		d.Tokens = []byte(`{}`)
	}
	return d, nil
}

func tokensParam(raw []byte) []byte {
	if len(raw) == 0 {
		return []byte(`{}`)
	}
	return raw
}

func (s *DesignSystemStore) Create(ctx context.Context, d domain.DesignSystem) (domain.DesignSystem, error) {
	out, err := scanDesignSystem(s.pool.QueryRow(ctx, `
		INSERT INTO design_systems (
			scope, project_id, repository_id, version, status, design_md, tokens, inventory_md,
			rationale, source_task_id, source_task_key, created_by
		)
		VALUES (
			$1, $2, $3,
			COALESCE((SELECT MAX(version) FROM design_systems
				WHERE scope = $1 AND (($1 = 'project' AND project_id = $2) OR ($1 = 'repository' AND repository_id = $3))), 0) + 1,
			$4, $5, $6, $7, $8, $9, $10, $11
		)
		RETURNING `+designSystemCols,
		d.Scope, d.ProjectID, d.RepositoryID, d.Status, d.DesignMD, tokensParam(d.Tokens), d.InventoryMD,
		d.Rationale, d.SourceTaskID, d.SourceTaskKey, d.CreatedBy,
	))
	if err != nil {
		return domain.DesignSystem{}, fmt.Errorf("create design system: %w", err)
	}
	return out, nil
}

func (s *DesignSystemStore) UpdateContent(ctx context.Context, d domain.DesignSystem) (domain.DesignSystem, error) {
	out, err := scanDesignSystem(s.pool.QueryRow(ctx, `
		UPDATE design_systems SET
			design_md = $2, tokens = $3, inventory_md = $4, rationale = $5, updated_at = now()
		WHERE id = $1
		RETURNING `+designSystemCols,
		d.ID, d.DesignMD, tokensParam(d.Tokens), d.InventoryMD, d.Rationale,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.DesignSystem{}, fmt.Errorf("update design system: %w", domain.ErrDesignSystemNotFound)
	}
	if err != nil {
		return domain.DesignSystem{}, fmt.Errorf("update design system: %w", err)
	}
	return out, nil
}

func (s *DesignSystemStore) Get(ctx context.Context, id uuid.UUID) (domain.DesignSystem, error) {
	out, err := scanDesignSystem(s.pool.QueryRow(ctx, `SELECT `+designSystemCols+` FROM design_systems WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.DesignSystem{}, fmt.Errorf("get design system: %w", domain.ErrDesignSystemNotFound)
	}
	if err != nil {
		return domain.DesignSystem{}, fmt.Errorf("get design system: %w", err)
	}
	return out, nil
}

func (s *DesignSystemStore) ListForProject(ctx context.Context, projectID uuid.UUID) ([]domain.DesignSystem, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+designSystemCols+` FROM design_systems
		WHERE scope = 'project' AND project_id = $1 ORDER BY version DESC`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list project design systems: %w", err)
	}
	return collectDesignSystems(rows)
}

func (s *DesignSystemStore) ListForRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.DesignSystem, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+designSystemCols+` FROM design_systems
		WHERE scope = 'repository' AND repository_id = $1 ORDER BY version DESC`, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("list repository design systems: %w", err)
	}
	return collectDesignSystems(rows)
}

func (s *DesignSystemStore) ListBySourceTask(ctx context.Context, taskID uuid.UUID) ([]domain.DesignSystem, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+designSystemCols+` FROM design_systems
		WHERE source_task_id = $1 ORDER BY created_at`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list design systems by task: %w", err)
	}
	return collectDesignSystems(rows)
}

func collectDesignSystems(rows pgx.Rows) ([]domain.DesignSystem, error) {
	defer rows.Close()
	out := []domain.DesignSystem{}
	for rows.Next() {
		d, err := scanDesignSystem(rows)
		if err != nil {
			return nil, fmt.Errorf("scan design system: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *DesignSystemStore) Approve(ctx context.Context, id uuid.UUID) (domain.DesignSystem, error) {
	var out domain.DesignSystem
	err := s.pool.InTx(ctx, func(tx pgx.Tx) error {
		target, err := scanDesignSystem(tx.QueryRow(ctx, `SELECT `+designSystemCols+` FROM design_systems WHERE id = $1 FOR UPDATE`, id))
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrDesignSystemNotFound
		}
		if err != nil {
			return err
		}
		if target.Status == domain.DesignSystemApproved {
			out = target
			return nil
		}
		if _, err := tx.Exec(ctx, `
			UPDATE design_systems SET status = 'superseded', updated_at = now()
			WHERE status = 'approved' AND scope = $1
			  AND (($1 = 'project' AND project_id = $2) OR ($1 = 'repository' AND repository_id = $3))`,
			target.Scope, target.ProjectID, target.RepositoryID); err != nil {
			return err
		}
		out, err = scanDesignSystem(tx.QueryRow(ctx, `
			UPDATE design_systems SET status = 'approved', approved_at = now(), updated_at = now()
			WHERE id = $1
			RETURNING `+designSystemCols, id))
		return err
	})
	if err != nil {
		return domain.DesignSystem{}, fmt.Errorf("approve design system: %w", err)
	}
	return out, nil
}

const designSystemRequestCols = `id, scope, project_id, repository_id, task_id, task_repository_id, created_at`

func scanDesignSystemRequest(row pgx.Row) (domain.DesignSystemRequest, error) {
	var r domain.DesignSystemRequest
	err := row.Scan(&r.ID, &r.Scope, &r.ProjectID, &r.RepositoryID, &r.TaskID, &r.TaskRepositoryID, &r.CreatedAt)
	return r, err
}

func (s *DesignSystemStore) CreateRequest(ctx context.Context, req domain.DesignSystemRequest) (domain.DesignSystemRequest, error) {
	out, err := scanDesignSystemRequest(s.pool.QueryRow(ctx, `
		INSERT INTO design_system_requests (scope, project_id, repository_id, task_id, task_repository_id)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING `+designSystemRequestCols,
		req.Scope, req.ProjectID, req.RepositoryID, req.TaskID, req.TaskRepositoryID))
	if err != nil {
		return domain.DesignSystemRequest{}, fmt.Errorf("create design system request: %w", err)
	}
	return out, nil
}

func (s *DesignSystemStore) LatestRequestForProject(ctx context.Context, projectID uuid.UUID) (*domain.DesignSystemRequest, error) {
	return s.latestRequest(ctx, `SELECT `+designSystemRequestCols+` FROM design_system_requests
		WHERE scope = 'project' AND project_id = $1 ORDER BY created_at DESC LIMIT 1`, projectID)
}

func (s *DesignSystemStore) LatestRequestForRepository(ctx context.Context, repositoryID uuid.UUID) (*domain.DesignSystemRequest, error) {
	return s.latestRequest(ctx, `SELECT `+designSystemRequestCols+` FROM design_system_requests
		WHERE scope = 'repository' AND repository_id = $1 ORDER BY created_at DESC LIMIT 1`, repositoryID)
}

func (s *DesignSystemStore) latestRequest(ctx context.Context, query string, id uuid.UUID) (*domain.DesignSystemRequest, error) {
	r, err := scanDesignSystemRequest(s.pool.QueryRow(ctx, query, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("latest design system request: %w", err)
	}
	return &r, nil
}

func (s *DesignSystemStore) RepositoryBaseProject(ctx context.Context, repositoryID uuid.UUID) (*uuid.UUID, error) {
	var projectID *uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT project_id FROM repository_design_settings WHERE repository_id = $1`, repositoryID).Scan(&projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get repository design settings: %w", err)
	}
	return projectID, nil
}

func (s *DesignSystemStore) SetRepositoryBaseProject(ctx context.Context, repositoryID uuid.UUID, projectID *uuid.UUID) error {
	if projectID == nil {
		if _, err := s.pool.Exec(ctx, `DELETE FROM repository_design_settings WHERE repository_id = $1`, repositoryID); err != nil {
			return fmt.Errorf("clear repository design settings: %w", err)
		}
		return nil
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO repository_design_settings (repository_id, project_id)
		VALUES ($1, $2)
		ON CONFLICT (repository_id) DO UPDATE SET project_id = EXCLUDED.project_id, updated_at = now()
	`, repositoryID, projectID); err != nil {
		return fmt.Errorf("set repository design settings: %w", err)
	}
	return nil
}
