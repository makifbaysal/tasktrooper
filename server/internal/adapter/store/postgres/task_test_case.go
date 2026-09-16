package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type TaskTestCaseStore struct {
	pool *DB
}

func NewTaskTestCaseStore(pool *DB) *TaskTestCaseStore {
	return &TaskTestCaseStore{pool: pool}
}

const testCaseColumns = `id, task_id, criterion_id, title, category, status, expected, actual, evidence, notes, position, created_at, updated_at, scored_at`

func (s *TaskTestCaseStore) ListByTask(ctx context.Context, taskID uuid.UUID) ([]domain.TaskTestCase, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+testCaseColumns+`
		FROM task_test_cases WHERE task_id = $1 ORDER BY position ASC, created_at ASC
	`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []domain.TaskTestCase
	for rows.Next() {
		c, err := scanTestCase(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, c)
	}
	return items, rows.Err()
}

// UpsertForTask writes a batch matched by title. Text fields fall back to what
// is already stored when the caller sends them empty: a round that records a
// case's plan and later records only its result must not erase the expectation
// it wrote in the first call.
func (s *TaskTestCaseStore) UpsertForTask(ctx context.Context, taskID uuid.UUID, items []domain.TaskTestCaseInput) ([]domain.TaskTestCase, error) {
	if len(items) == 0 {
		return s.ListByTask(ctx, taskID)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var next int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(position), 0) FROM task_test_cases WHERE task_id = $1`, taskID).Scan(&next); err != nil {
		return nil, err
	}
	for _, item := range items {
		pos := item.Position
		if pos == 0 {
			next++
			pos = next
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO task_test_cases (task_id, criterion_id, title, category, status, expected, actual, evidence, notes, position)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT (task_id, title) DO UPDATE SET
				criterion_id = COALESCE(EXCLUDED.criterion_id, task_test_cases.criterion_id),
				category     = EXCLUDED.category,
				status       = EXCLUDED.status,
				expected     = COALESCE(NULLIF(EXCLUDED.expected, ''), task_test_cases.expected),
				actual       = COALESCE(NULLIF(EXCLUDED.actual, ''), task_test_cases.actual),
				evidence     = COALESCE(NULLIF(EXCLUDED.evidence, ''), task_test_cases.evidence),
				notes        = COALESCE(NULLIF(EXCLUDED.notes, ''), task_test_cases.notes),
				updated_at   = now()
		`, taskID, item.CriterionID, item.Title, item.Category, item.Status,
			item.Expected, item.Actual, item.Evidence, item.Notes, pos); err != nil {
			return nil, fmt.Errorf("upsert test case %q: %w", item.Title, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.ListByTask(ctx, taskID)
}

func (s *TaskTestCaseStore) ReplaceForTask(ctx context.Context, taskID uuid.UUID, items []domain.TaskTestCaseInput) ([]domain.TaskTestCase, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM task_test_cases WHERE task_id = $1`, taskID); err != nil {
		return nil, err
	}
	for i, item := range items {
		pos := item.Position
		if pos == 0 {
			pos = i + 1
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO task_test_cases (task_id, criterion_id, title, category, status, expected, actual, evidence, notes, position)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		`, taskID, item.CriterionID, item.Title, item.Category, item.Status,
			item.Expected, item.Actual, item.Evidence, item.Notes, pos); err != nil {
			return nil, fmt.Errorf("insert test case %q: %w", item.Title, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.ListByTask(ctx, taskID)
}

func (s *TaskTestCaseStore) Get(ctx context.Context, id uuid.UUID) (domain.TaskTestCase, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+testCaseColumns+` FROM task_test_cases WHERE id = $1`, id)
	return scanTestCase(row)
}

func (s *TaskTestCaseStore) Update(ctx context.Context, id uuid.UUID, item domain.TaskTestCaseInput) (domain.TaskTestCase, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE task_test_cases SET
			criterion_id = COALESCE($2, criterion_id),
			title    = COALESCE(NULLIF($3, ''), title),
			category = $4,
			status   = $5,
			expected = COALESCE(NULLIF($6, ''), expected),
			actual   = COALESCE(NULLIF($7, ''), actual),
			evidence = COALESCE(NULLIF($8, ''), evidence),
			notes    = COALESCE(NULLIF($9, ''), notes),
			updated_at = now()
		WHERE id = $1
		RETURNING `+testCaseColumns, id, item.CriterionID, item.Title, item.Category, item.Status,
		item.Expected, item.Actual, item.Evidence, item.Notes)
	return scanTestCase(row)
}

func (s *TaskTestCaseStore) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM task_test_cases WHERE id = $1`, id)
	return err
}

// testCaseRowScanner covers both pgx.Row and pgx.Rows so one scan function
// serves the single-row reads and the list.
type testCaseRowScanner interface {
	Scan(dest ...any) error
}

func scanTestCase(row testCaseRowScanner) (domain.TaskTestCase, error) {
	var c domain.TaskTestCase
	if err := row.Scan(&c.ID, &c.TaskID, &c.CriterionID, &c.Title, &c.Category, &c.Status,
		&c.Expected, &c.Actual, &c.Evidence, &c.Notes, &c.Position, &c.CreatedAt, &c.UpdatedAt, &c.ScoredAt); err != nil {
		return domain.TaskTestCase{}, err
	}
	return c, nil
}

// MarkScored stamps the given cases as already turned into a performance
// score event, so a later round or forward exit never counts them again.
func (s *TaskTestCaseStore) MarkScored(ctx context.Context, ids []uuid.UUID, at time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := s.pool.Exec(ctx, `UPDATE task_test_cases SET scored_at = $2 WHERE id = ANY($1)`, ids, at)
	return err
}
