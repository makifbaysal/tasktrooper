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

type IssueLinkStore struct {
	pool *DB
}

func NewIssueLinkStore(pool *DB) *IssueLinkStore {
	return &IssueLinkStore{pool: pool}
}

const issueLinkColumns = `id, task_id, task_key, imported_by, repository_id, provider, external_key, url, title, last_column, closed_at, created_at`

func (s *IssueLinkStore) Create(ctx context.Context, link domain.IssueLink) (domain.IssueLink, error) {
	if link.ID == uuid.Nil {
		link.ID = uuid.New()
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO issue_links (id, task_id, task_key, imported_by, repository_id, provider, external_key, url, title, last_column, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, now())
		RETURNING `+issueLinkColumns,
		link.ID, link.TaskID, link.TaskKey, link.ImportedBy, link.RepositoryID, string(link.Provider), link.ExternalKey, link.URL, link.Title, link.LastColumn)
	out, err := scanIssueLink(row)
	if err != nil {
		return domain.IssueLink{}, fmt.Errorf("create issue link: %w", err)
	}
	return out, nil
}

func (s *IssueLinkStore) GetByTask(ctx context.Context, taskID uuid.UUID) (domain.IssueLink, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+issueLinkColumns+` FROM issue_links WHERE task_id = $1`, taskID)
	out, err := scanIssueLink(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.IssueLink{}, fmt.Errorf("get issue link by task: %w", port.ErrNotFound)
	}
	if err != nil {
		return domain.IssueLink{}, fmt.Errorf("get issue link by task: %w", err)
	}
	return out, nil
}

func (s *IssueLinkStore) ListByIssue(ctx context.Context, provider domain.IssueProvider, key string) ([]domain.IssueLink, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+issueLinkColumns+` FROM issue_links
		WHERE provider = $1 AND external_key = $2
		ORDER BY created_at, id
	`, string(provider), key)
	if err != nil {
		return nil, fmt.Errorf("list issue links: %w", err)
	}
	defer rows.Close()
	out := []domain.IssueLink{}
	for rows.Next() {
		link, err := scanIssueLink(rows)
		if err != nil {
			return nil, fmt.Errorf("list issue links: %w", err)
		}
		out = append(out, link)
	}
	return out, rows.Err()
}

func (s *IssueLinkStore) ExistingKeys(ctx context.Context, provider domain.IssueProvider, keys []string) (map[string]domain.IssueLink, error) {
	out := make(map[string]domain.IssueLink, len(keys))
	if len(keys) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT ON (external_key) `+issueLinkColumns+`
		FROM issue_links WHERE provider = $1 AND external_key = ANY($2)
		ORDER BY external_key, created_at, id
	`, string(provider), keys)
	if err != nil {
		return nil, fmt.Errorf("existing issue links: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		link, err := scanIssueLink(rows)
		if err != nil {
			return nil, fmt.Errorf("existing issue links: %w", err)
		}
		out[link.ExternalKey] = link
	}
	return out, rows.Err()
}

func (s *IssueLinkStore) UpdateColumn(ctx context.Context, taskID uuid.UUID, column string) error {
	if _, err := s.pool.Exec(ctx, `UPDATE issue_links SET last_column = $2 WHERE task_id = $1`, taskID, column); err != nil {
		return fmt.Errorf("update issue link column: %w", err)
	}
	return nil
}

func (s *IssueLinkStore) MarkIssueClosed(ctx context.Context, provider domain.IssueProvider, key string, at time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE issue_links SET closed_at = $3
		WHERE provider = $1 AND external_key = $2 AND closed_at IS NULL
	`, string(provider), key, at)
	if err != nil {
		return fmt.Errorf("mark issue links closed: %w", err)
	}
	return nil
}

type issueRowScanner interface {
	Scan(dest ...any) error
}

func scanIssueLink(row issueRowScanner) (domain.IssueLink, error) {
	var out domain.IssueLink
	var provider string
	if err := row.Scan(&out.ID, &out.TaskID, &out.TaskKey, &out.ImportedBy, &out.RepositoryID, &provider, &out.ExternalKey,
		&out.URL, &out.Title, &out.LastColumn, &out.ClosedAt, &out.CreatedAt); err != nil {
		return domain.IssueLink{}, err
	}
	out.Provider = domain.IssueProvider(provider)
	return out, nil
}
