package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// ErrorDefinitionStore persists error definitions and their error group
// links (migration 191).
type ErrorDefinitionStore struct {
	pool *DB
}

func NewErrorDefinitionStore(pool *DB) *ErrorDefinitionStore {
	return &ErrorDefinitionStore{pool: pool}
}

var _ port.ErrorDefinitionStore = (*ErrorDefinitionStore)(nil)

const errorDefinitionCols = `id, repository_id, component_id, name, match, category, severity, action, alert_threshold,
	source, status, rationale, evidence, task_id, firing_since, created_at, updated_at`

func scanErrorDefinition(row pgx.Row) (domain.ErrorDefinition, error) {
	var d domain.ErrorDefinition
	var matchJSON, thresholdJSON, evidenceJSON []byte
	if err := row.Scan(
		&d.ID, &d.RepositoryID, &d.ComponentID, &d.Name, &matchJSON, &d.Category, &d.Severity, &d.Action, &thresholdJSON,
		&d.Source, &d.Status, &d.Rationale, &evidenceJSON, &d.TaskID, &d.FiringSince, &d.CreatedAt, &d.UpdatedAt,
	); err != nil {
		return domain.ErrorDefinition{}, err
	}
	if err := json.Unmarshal(matchJSON, &d.Match); err != nil {
		return domain.ErrorDefinition{}, fmt.Errorf("unmarshal error definition match: %w", err)
	}
	if len(thresholdJSON) > 0 && string(thresholdJSON) != "null" {
		var t domain.ErrorAlertThreshold
		if err := json.Unmarshal(thresholdJSON, &t); err != nil {
			return domain.ErrorDefinition{}, fmt.Errorf("unmarshal error definition threshold: %w", err)
		}
		d.AlertThreshold = &t
	}
	if err := json.Unmarshal(evidenceJSON, &d.Evidence); err != nil {
		return domain.ErrorDefinition{}, fmt.Errorf("unmarshal error definition evidence: %w", err)
	}
	if d.Evidence == nil {
		d.Evidence = []string{}
	}
	return d, nil
}

func errorDefinitionArgs(d domain.ErrorDefinition) (match, threshold, evidence []byte, err error) {
	if match, err = json.Marshal(d.Match); err != nil {
		return nil, nil, nil, err
	}
	if d.AlertThreshold != nil {
		if threshold, err = json.Marshal(d.AlertThreshold); err != nil {
			return nil, nil, nil, err
		}
	}
	ev := d.Evidence
	if ev == nil {
		ev = []string{}
	}
	if evidence, err = json.Marshal(ev); err != nil {
		return nil, nil, nil, err
	}
	return match, threshold, evidence, nil
}

func (s *ErrorDefinitionStore) ListErrorDefinitions(ctx context.Context, f domain.ErrorDefinitionFilter) ([]domain.ErrorDefinition, error) {
	statuses := make([]string, 0, len(f.Statuses))
	for _, st := range f.Statuses {
		statuses = append(statuses, string(st))
	}
	actions := make([]string, 0, len(f.Actions))
	for _, a := range f.Actions {
		actions = append(actions, string(a))
	}
	rows, err := s.pool.Query(ctx, `SELECT `+errorDefinitionCols+` FROM error_definitions
		WHERE (cardinality($1::uuid[]) = 0 OR repository_id = ANY($1))
		  AND ($2::uuid IS NULL OR component_id IS NULL OR component_id = $2)
		  AND (cardinality($3::text[]) = 0 OR status = ANY($3))
		  AND (cardinality($4::text[]) = 0 OR action = ANY($4))
		ORDER BY created_at DESC, id`, errorDefIDs(f.RepositoryIDs), f.ComponentID, statuses, actions)
	if err != nil {
		return nil, fmt.Errorf("list error definitions: %w", err)
	}
	defer rows.Close()
	out := []domain.ErrorDefinition{}
	for rows.Next() {
		d, err := scanErrorDefinition(rows)
		if err != nil {
			return nil, fmt.Errorf("scan error definition: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func errorDefIDs(ids []uuid.UUID) []uuid.UUID {
	if ids == nil {
		return []uuid.UUID{}
	}
	return ids
}

func (s *ErrorDefinitionStore) GetErrorDefinition(ctx context.Context, id uuid.UUID) (domain.ErrorDefinition, error) {
	d, err := scanErrorDefinition(s.pool.QueryRow(ctx, `SELECT `+errorDefinitionCols+` FROM error_definitions WHERE id = $1`, id))
	return d, errorDefStoreErr("get error definition", err)
}

func errorDefStoreErr(op string, err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("%s: %w", op, port.ErrNotFound)
	default:
		return fmt.Errorf("%s: %w", op, err)
	}
}

func (s *ErrorDefinitionStore) CreateErrorDefinition(ctx context.Context, d domain.ErrorDefinition) (domain.ErrorDefinition, error) {
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	match, threshold, evidence, err := errorDefinitionArgs(d)
	if err != nil {
		return domain.ErrorDefinition{}, fmt.Errorf("create error definition: %w", err)
	}
	out, err := scanErrorDefinition(s.pool.QueryRow(ctx, `
		INSERT INTO error_definitions (id, repository_id, component_id, name, match, category, severity, action,
			alert_threshold, source, status, rationale, evidence)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		RETURNING `+errorDefinitionCols,
		d.ID, d.RepositoryID, d.ComponentID, d.Name, match, d.Category, d.Severity, d.Action,
		threshold, d.Source, d.Status, d.Rationale, evidence))
	if isUniqueViolation(err) {
		return domain.ErrorDefinition{}, fmt.Errorf("create error definition: %w", port.ErrErrorDefinitionExists)
	}
	if err != nil {
		return domain.ErrorDefinition{}, fmt.Errorf("create error definition: %w", err)
	}
	return out, nil
}

func (s *ErrorDefinitionStore) UpdateErrorDefinition(ctx context.Context, d domain.ErrorDefinition) (domain.ErrorDefinition, error) {
	match, threshold, evidence, err := errorDefinitionArgs(d)
	if err != nil {
		return domain.ErrorDefinition{}, fmt.Errorf("update error definition: %w", err)
	}
	out, err := scanErrorDefinition(s.pool.QueryRow(ctx, `
		UPDATE error_definitions SET component_id = $2, name = $3, match = $4, category = $5, severity = $6,
			action = $7, alert_threshold = $8, rationale = $9, evidence = $10,
			firing_since = CASE WHEN action = $7 THEN firing_since END, updated_at = now()
		WHERE id = $1
		RETURNING `+errorDefinitionCols,
		d.ID, d.ComponentID, d.Name, match, d.Category, d.Severity, d.Action, threshold, d.Rationale, evidence))
	return out, errorDefStoreErr("update error definition", err)
}

func (s *ErrorDefinitionStore) SetErrorDefinitionStatus(ctx context.Context, id uuid.UUID, status domain.ErrorDefinitionStatus) (domain.ErrorDefinition, error) {
	out, err := scanErrorDefinition(s.pool.QueryRow(ctx, `
		UPDATE error_definitions SET status = $2,
			firing_since = CASE WHEN $2 = 'active' THEN firing_since END, updated_at = now()
		WHERE id = $1
		RETURNING `+errorDefinitionCols, id, string(status)))
	return out, errorDefStoreErr("set error definition status", err)
}

func (s *ErrorDefinitionStore) SetErrorDefinitionFiring(ctx context.Context, id uuid.UUID, since *time.Time) error {
	if _, err := s.pool.Exec(ctx, `UPDATE error_definitions SET firing_since = $2 WHERE id = $1`, id, since); err != nil {
		return fmt.Errorf("set error definition firing: %w", err)
	}
	return nil
}

func (s *ErrorDefinitionStore) SetErrorDefinitionTask(ctx context.Context, id, taskID uuid.UUID) error {
	if _, err := s.pool.Exec(ctx, `UPDATE error_definitions SET task_id = COALESCE(task_id, $2) WHERE id = $1`, id, taskID); err != nil {
		return fmt.Errorf("set error definition task: %w", err)
	}
	return nil
}

func (s *ErrorDefinitionStore) DeleteErrorDefinition(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM error_definitions WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete error definition: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("delete error definition: %w", port.ErrNotFound)
	}
	return nil
}

func (s *ErrorDefinitionStore) ClaimErrorGroupLink(ctx context.Context, l domain.ErrorGroupLink, staleBefore time.Time) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO error_group_links (definition_id, fingerprint, component_id, environment_id, data_source_id, claimed_at)
		VALUES ($1,$2,$3,$4,$5, now())
		ON CONFLICT (definition_id, fingerprint) DO UPDATE SET
			component_id = EXCLUDED.component_id, environment_id = EXCLUDED.environment_id,
			data_source_id = EXCLUDED.data_source_id, claimed_at = now()
		WHERE error_group_links.task_id IS NULL AND error_group_links.claimed_at < $6`,
		l.DefinitionID, l.Fingerprint, l.ComponentID, l.EnvironmentID, l.DataSourceID, staleBefore)
	if err != nil {
		return false, fmt.Errorf("claim error group link: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (s *ErrorDefinitionStore) SetErrorGroupLinkTask(ctx context.Context, definitionID uuid.UUID, fingerprint string, taskID uuid.UUID) error {
	if _, err := s.pool.Exec(ctx, `UPDATE error_group_links SET task_id = $3, claimed_at = NULL WHERE definition_id = $1 AND fingerprint = $2`,
		definitionID, fingerprint, taskID); err != nil {
		return fmt.Errorf("set error group link task: %w", err)
	}
	return nil
}

func (s *ErrorDefinitionStore) ReleaseErrorGroupLink(ctx context.Context, definitionID uuid.UUID, fingerprint string) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM error_group_links WHERE definition_id = $1 AND fingerprint = $2 AND task_id IS NULL`,
		definitionID, fingerprint); err != nil {
		return fmt.Errorf("release error group link: %w", err)
	}
	return nil
}
