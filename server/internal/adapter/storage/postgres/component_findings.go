package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// FindingStore persists code-health findings (migration 185).
type FindingStore struct {
	pool *DB
}

func NewFindingStore(pool *DB) *FindingStore { return &FindingStore{pool: pool} }

var _ port.FindingStore = (*FindingStore)(nil)

const findingSelect = `
	SELECT f.id, f.repository_id, f.component_id, f.rule, f.category, f.severity, f.occurrences, f.evidence,
		f.status, f.status_reason, f.task_id, f.last_scan_id, f.resolved_at, f.created_at, f.updated_at,
		t.id, (SELECT key_prefix FROM task_types WHERE key = t.task_type) || '-' || t.task_number, t.title, t.board_column
	FROM component_findings f
	LEFT JOIN board_tasks t ON t.id = f.task_id`

const findingOrder = ` ORDER BY f.component_id NULLS FIRST,
	CASE f.severity WHEN 'high' THEN 0 WHEN 'medium' THEN 1 ELSE 2 END, f.rule`

func scanFinding(row pgx.Row) (domain.ComponentFinding, error) {
	var f domain.ComponentFinding
	var evidence []byte
	var taskID *uuid.UUID
	var taskKey, taskTitle, taskColumn *string
	if err := row.Scan(&f.ID, &f.RepositoryID, &f.ComponentID, &f.Rule, &f.Category, &f.Severity, &f.Occurrences, &evidence,
		&f.Status, &f.StatusReason, &f.TaskID, &f.LastScanID, &f.ResolvedAt, &f.CreatedAt, &f.UpdatedAt,
		&taskID, &taskKey, &taskTitle, &taskColumn); err != nil {
		return domain.ComponentFinding{}, err
	}
	if err := json.Unmarshal(evidence, &f.Evidence); err != nil {
		return domain.ComponentFinding{}, fmt.Errorf("unmarshal finding evidence: %w", err)
	}
	if taskID != nil {
		f.Task = &domain.FindingTask{ID: *taskID, Key: deref(taskKey), Title: deref(taskTitle), Column: domain.TaskColumn(deref(taskColumn))}
	}
	return f, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (s *FindingStore) ListFindings(ctx context.Context, repositoryID uuid.UUID) ([]domain.ComponentFinding, error) {
	rows, err := s.pool.Query(ctx, findingSelect+` WHERE f.repository_id = $1`+findingOrder, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("list findings: %w", err)
	}
	defer rows.Close()
	var out []domain.ComponentFinding
	for rows.Next() {
		f, err := scanFinding(rows)
		if err != nil {
			return nil, fmt.Errorf("scan finding: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *FindingStore) GetFinding(ctx context.Context, id uuid.UUID) (domain.ComponentFinding, error) {
	f, err := scanFinding(s.pool.QueryRow(ctx, findingSelect+` WHERE f.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ComponentFinding{}, port.ErrNotFound
	}
	if err != nil {
		return domain.ComponentFinding{}, fmt.Errorf("get finding: %w", err)
	}
	return f, nil
}

const upsertFindingSQL = `
INSERT INTO component_findings
	(id, repository_id, component_id, rule, category, severity, occurrences, evidence, status, status_reason, task_id, last_scan_id, resolved_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
ON CONFLICT (id) DO UPDATE SET
	component_id = EXCLUDED.component_id,
	rule = EXCLUDED.rule,
	category = EXCLUDED.category,
	severity = EXCLUDED.severity,
	occurrences = EXCLUDED.occurrences,
	evidence = EXCLUDED.evidence,
	status = EXCLUDED.status,
	status_reason = EXCLUDED.status_reason,
	task_id = EXCLUDED.task_id,
	last_scan_id = EXCLUDED.last_scan_id,
	resolved_at = EXCLUDED.resolved_at,
	updated_at = now()`

func upsertFinding(ctx context.Context, q pmExecutor, f domain.ComponentFinding) (uuid.UUID, error) {
	if f.ID == uuid.Nil {
		f.ID = uuid.New()
	}
	if f.Status == "" {
		f.Status = domain.FindingOpen
	}
	evidence, err := json.Marshal(nonNilSlice(f.Evidence))
	if err != nil {
		return uuid.Nil, fmt.Errorf("marshal finding evidence: %w", err)
	}
	if _, err := q.Exec(ctx, upsertFindingSQL, f.ID, f.RepositoryID, f.ComponentID, f.Rule, f.Category, f.Severity,
		f.Occurrences, evidence, f.Status, f.StatusReason, f.TaskID, f.LastScanID, f.ResolvedAt); err != nil {
		return uuid.Nil, fmt.Errorf("upsert finding: %w", err)
	}
	return f.ID, nil
}

func (s *FindingStore) SaveFinding(ctx context.Context, f domain.ComponentFinding) (domain.ComponentFinding, error) {
	id, err := upsertFinding(ctx, s.pool, f)
	if err != nil {
		return domain.ComponentFinding{}, err
	}
	return s.GetFinding(ctx, id)
}

func (s *FindingStore) ApplyFindings(ctx context.Context, repositoryID uuid.UUID, save []domain.ComponentFinding, del []uuid.UUID) error {
	return s.pool.InTx(ctx, func(tx pgx.Tx) error {
		for _, f := range save {
			f.RepositoryID = repositoryID
			if _, err := upsertFinding(ctx, tx, f); err != nil {
				return err
			}
		}
		if len(del) > 0 {
			if _, err := tx.Exec(ctx, `DELETE FROM component_findings WHERE repository_id = $1 AND id = ANY($2)`, repositoryID, del); err != nil {
				return fmt.Errorf("delete findings: %w", err)
			}
		}
		return nil
	})
}

func (s *FindingStore) LinkFindingsToTask(ctx context.Context, ids []uuid.UUID, taskID uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	if _, err := s.pool.Exec(ctx, `UPDATE component_findings SET task_id = $1, updated_at = now() WHERE id = ANY($2)`, taskID, ids); err != nil {
		return fmt.Errorf("link findings to task: %w", err)
	}
	return nil
}
