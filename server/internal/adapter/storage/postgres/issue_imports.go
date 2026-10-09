package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type IssueImportStore struct {
	pool *DB
}

func NewIssueImportStore(pool *DB) *IssueImportStore {
	return &IssueImportStore{pool: pool}
}

const issueImportColumns = `id, provider, external_key, repository_id, url, title, imported_by, intake_task_id,
	conversion_status, conversion_session_id, conversion_error, closed_at, created_at, updated_at`

func (s *IssueImportStore) Create(ctx context.Context, imp domain.IssueImport) (domain.IssueImport, error) {
	if imp.ID == uuid.Nil {
		imp.ID = uuid.New()
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO issue_imports (id, provider, external_key, repository_id, url, title, imported_by, intake_task_id,
		                           conversion_status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now(), now())
		RETURNING `+issueImportColumns,
		imp.ID, string(imp.Provider), imp.ExternalKey, imp.RepositoryID, imp.URL, imp.Title, imp.ImportedBy,
		imp.IntakeTaskID, string(imp.ConversionStatus))
	out, err := scanIssueImport(row)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.IssueImport{}, fmt.Errorf("create issue import: %w", port.ErrIssueImportExists)
		}
		return domain.IssueImport{}, fmt.Errorf("create issue import: %w", err)
	}
	return out, nil
}

func (s *IssueImportStore) Get(ctx context.Context, id uuid.UUID) (domain.IssueImport, error) {
	return s.getOne(ctx, `WHERE id = $1`, id)
}

func (s *IssueImportStore) GetByProviderKey(ctx context.Context, provider domain.IssueProvider, key string) (domain.IssueImport, error) {
	return s.getOne(ctx, `WHERE provider = $1 AND external_key = $2`, string(provider), key)
}

func (s *IssueImportStore) GetBySession(ctx context.Context, sessionID uuid.UUID) (domain.IssueImport, error) {
	return s.getOne(ctx, `WHERE conversion_session_id = $1`, sessionID)
}

func (s *IssueImportStore) getOne(ctx context.Context, where string, args ...any) (domain.IssueImport, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+issueImportColumns+` FROM issue_imports `+where+` LIMIT 1`, args...)
	out, err := scanIssueImport(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.IssueImport{}, fmt.Errorf("get issue import: %w", port.ErrNotFound)
	}
	if err != nil {
		return domain.IssueImport{}, fmt.Errorf("get issue import: %w", err)
	}
	return out, nil
}

func (s *IssueImportStore) ListByStatus(ctx context.Context, status domain.IssueConversionStatus) ([]domain.IssueImport, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+issueImportColumns+` FROM issue_imports
		WHERE conversion_status = $1 ORDER BY created_at, id
	`, string(status))
	if err != nil {
		return nil, fmt.Errorf("list issue imports: %w", err)
	}
	defer rows.Close()
	out := []domain.IssueImport{}
	for rows.Next() {
		imp, err := scanIssueImport(rows)
		if err != nil {
			return nil, fmt.Errorf("list issue imports: %w", err)
		}
		out = append(out, imp)
	}
	return out, rows.Err()
}

func (s *IssueImportStore) ClaimConversion(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE issue_imports SET conversion_status = 'converting', updated_at = now()
		WHERE id = $1 AND conversion_status = 'pending'
	`, id)
	if err != nil {
		return false, fmt.Errorf("claim issue conversion: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (s *IssueImportStore) ResetConverting(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, `
		UPDATE issue_imports SET conversion_status = 'pending', updated_at = now()
		WHERE conversion_status = 'converting'
	`); err != nil {
		return fmt.Errorf("reset issue conversions: %w", err)
	}
	return nil
}

func (s *IssueImportStore) SetConversion(ctx context.Context, id uuid.UUID, status domain.IssueConversionStatus, sessionID *uuid.UUID, errMsg string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE issue_imports
		SET conversion_status = $2,
		    conversion_session_id = COALESCE($3, conversion_session_id),
		    conversion_error = $4,
		    updated_at = now()
		WHERE id = $1
	`, id, string(status), sessionID, errMsg)
	if err != nil {
		return fmt.Errorf("set issue conversion: %w", err)
	}
	return nil
}

func (s *IssueImportStore) ClearIntakeTask(ctx context.Context, id uuid.UUID) error {
	if _, err := s.pool.Exec(ctx, `UPDATE issue_imports SET intake_task_id = NULL, updated_at = now() WHERE id = $1`, id); err != nil {
		return fmt.Errorf("clear issue intake task: %w", err)
	}
	return nil
}

func (s *IssueImportStore) MarkClosed(ctx context.Context, id uuid.UUID, at time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE issue_imports SET closed_at = $2, updated_at = now() WHERE id = $1 AND closed_at IS NULL
	`, id, at)
	if err != nil {
		return fmt.Errorf("mark issue import closed: %w", err)
	}
	return nil
}

func (s *IssueImportStore) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM issue_imports WHERE id = $1`, id); err != nil {
		return fmt.Errorf("delete issue import: %w", err)
	}
	return nil
}

func scanIssueImport(row issueRowScanner) (domain.IssueImport, error) {
	var out domain.IssueImport
	var provider, status string
	if err := row.Scan(&out.ID, &provider, &out.ExternalKey, &out.RepositoryID, &out.URL, &out.Title, &out.ImportedBy,
		&out.IntakeTaskID, &status, &out.ConversionSessionID, &out.ConversionError, &out.ClosedAt,
		&out.CreatedAt, &out.UpdatedAt); err != nil {
		return domain.IssueImport{}, err
	}
	out.Provider = domain.IssueProvider(provider)
	out.ConversionStatus = domain.IssueConversionStatus(status)
	return out, nil
}
