package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type IncidentStore struct {
	pool *DB
}

func NewIncidentStore(pool *DB) *IncidentStore {
	return &IncidentStore{pool: pool}
}

// remedy_author is NULL on every row written before migration 077 — authorship
// there is genuinely unknown, so it is not backfilled. The domain models that
// as the empty string, which the triage guard reads as "no recorded author".
const incidentCols = `id, repository_id, env, source, fingerprint, title, detail, severity, status,
	payload, remedy, remedy_kind, COALESCE(remedy_author, '') AS remedy_author, confidence, occurrences,
	task_id, first_seen_at, last_seen_at, resolved_at`

// severityRank mirrors domain.IncidentSeverity.Rank() so an escalating
// recurrence (warning → page) raises the incident's severity instead of being
// folded in silently at the old level.
const severityRank = `CASE %s WHEN 'critical' THEN 4 WHEN 'high' THEN 3 WHEN 'medium' THEN 2 WHEN 'low' THEN 1 ELSE 0 END`

func scanIncident(row pgx.Row, extra ...any) (domain.Incident, error) {
	var inc domain.Incident
	var severity, status string
	var payloadJSON []byte
	dest := []any{
		&inc.ID, &inc.RepositoryID, &inc.Env, &inc.Source, &inc.Fingerprint, &inc.Title, &inc.Detail,
		&severity, &status, &payloadJSON, &inc.Remedy, &inc.RemedyKind, &inc.RemedyAuthor,
		&inc.Confidence, &inc.Occurrences, &inc.TaskID, &inc.FirstSeenAt, &inc.LastSeenAt, &inc.ResolvedAt,
	}
	dest = append(dest, extra...)
	if err := row.Scan(dest...); err != nil {
		return domain.Incident{}, err
	}
	inc.Severity = domain.IncidentSeverity(severity)
	inc.Status = domain.IncidentStatus(status)
	if len(payloadJSON) > 0 {
		_ = json.Unmarshal(payloadJSON, &inc.Payload)
	}
	return inc, nil
}

// Upsert folds a recurrence into the live incident with the same fingerprint,
// or opens a new one. The ON CONFLICT target repeats the partial index
// predicate so a resolved incident never absorbs a fresh outage.
func (s *IncidentStore) Upsert(ctx context.Context, in domain.IncidentInput) (domain.Incident, bool, error) {
	payload := in.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return domain.Incident{}, false, fmt.Errorf("marshal incident payload: %w", err)
	}
	var inserted bool
	inc, err := scanIncident(s.pool.QueryRow(ctx, `
		INSERT INTO prod_incidents (repository_id, env, source, fingerprint, title, detail, severity, payload)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (repository_id, env, fingerprint) WHERE status NOT IN ('resolved', 'ignored')
		DO UPDATE SET
			occurrences = prod_incidents.occurrences + 1,
			last_seen_at = now(),
			detail = EXCLUDED.detail,
			payload = EXCLUDED.payload,
			severity = CASE WHEN `+fmt.Sprintf(severityRank, "EXCLUDED.severity")+` > `+
		fmt.Sprintf(severityRank, "prod_incidents.severity")+`
				THEN EXCLUDED.severity ELSE prod_incidents.severity END
		RETURNING `+incidentCols+`, (xmax = 0) AS inserted`,
		in.RepositoryID, in.Env, in.Source, in.Fingerprint, in.Title, in.Detail,
		string(in.Severity), payloadJSON), &inserted)
	if err != nil {
		return domain.Incident{}, false, fmt.Errorf("upsert incident: %w", err)
	}
	return inc, inserted, nil
}

func (s *IncidentStore) Get(ctx context.Context, id uuid.UUID) (domain.Incident, error) {
	inc, err := scanIncident(s.pool.QueryRow(ctx, `SELECT `+incidentCols+` FROM prod_incidents WHERE id = $1`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Incident{}, domain.ErrIncidentNotFound
		}
		return domain.Incident{}, fmt.Errorf("get incident: %w", err)
	}
	events, err := s.ListEvents(ctx, id)
	if err != nil {
		return domain.Incident{}, err
	}
	inc.Events = events
	return inc, nil
}

func (s *IncidentStore) List(ctx context.Context, filter domain.IncidentFilter) ([]domain.Incident, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	statuses := make([]string, 0, len(filter.Statuses))
	for _, st := range filter.Statuses {
		statuses = append(statuses, string(st))
	}
	var repositoryID *uuid.UUID = filter.RepositoryID
	rows, err := s.pool.Query(ctx, `SELECT `+incidentCols+` FROM prod_incidents
		WHERE ($1::uuid IS NULL OR repository_id = $1)
		  AND ($2 = '' OR env = $2)
		  AND (cardinality($3::text[]) = 0 OR status = ANY($3))
		  AND ($5 = '' OR source = $5)
		ORDER BY last_seen_at DESC
		LIMIT $4`, repositoryID, filter.Env, statuses, limit, filter.Source)
	if err != nil {
		return nil, fmt.Errorf("list incidents: %w", err)
	}
	defer rows.Close()
	return collectIncidents(rows)
}

func collectIncidents(rows pgx.Rows) ([]domain.Incident, error) {
	var out []domain.Incident
	for rows.Next() {
		inc, err := scanIncident(rows)
		if err != nil {
			return nil, fmt.Errorf("scan incident: %w", err)
		}
		out = append(out, inc)
	}
	return out, rows.Err()
}

func (s *IncidentStore) FindLive(ctx context.Context, repositoryID uuid.UUID, env, fingerprint string) (domain.Incident, error) {
	inc, err := scanIncident(s.pool.QueryRow(ctx, `SELECT `+incidentCols+` FROM prod_incidents
		WHERE repository_id = $1 AND env = $2 AND fingerprint = $3 AND status NOT IN ('resolved', 'ignored')`,
		repositoryID, env, fingerprint))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Incident{}, domain.ErrIncidentNotFound
		}
		return domain.Incident{}, fmt.Errorf("find live incident: %w", err)
	}
	return inc, nil
}

func (s *IncidentStore) History(ctx context.Context, repositoryID uuid.UUID, fingerprint string, limit int) ([]domain.Incident, error) {
	if limit <= 0 || limit > 50 {
		limit = 5
	}
	rows, err := s.pool.Query(ctx, `SELECT `+incidentCols+` FROM prod_incidents
		WHERE repository_id = $1 AND fingerprint = $2 AND status = 'resolved'
		ORDER BY resolved_at DESC NULLS LAST
		LIMIT $3`, repositoryID, fingerprint, limit)
	if err != nil {
		return nil, fmt.Errorf("incident history: %w", err)
	}
	defer rows.Close()
	return collectIncidents(rows)
}

func (s *IncidentStore) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.IncidentStatus) (domain.Incident, error) {
	inc, err := scanIncident(s.pool.QueryRow(ctx, `
		UPDATE prod_incidents SET
			status = $2,
			resolved_at = CASE WHEN $2 IN ('resolved', 'ignored') THEN now() ELSE NULL END
		WHERE id = $1
		RETURNING `+incidentCols, id, string(status)))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Incident{}, domain.ErrIncidentNotFound
		}
		return domain.Incident{}, fmt.Errorf("update incident status: %w", err)
	}
	return inc, nil
}

// UpdateRemedy replaces the incident's proposal and records who wrote it, so a
// later machine pass can tell its own output apart from a real diagnosis.
func (s *IncidentStore) UpdateRemedy(ctx context.Context, id uuid.UUID, remedy, remedyKind, author string, confidence int) (domain.Incident, error) {
	inc, err := scanIncident(s.pool.QueryRow(ctx, `
		UPDATE prod_incidents SET remedy = $2, remedy_kind = $3, remedy_author = $4, confidence = $5
		WHERE id = $1
		RETURNING `+incidentCols, id, remedy, remedyKind, author, confidence))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Incident{}, domain.ErrIncidentNotFound
		}
		return domain.Incident{}, fmt.Errorf("update incident remedy: %w", err)
	}
	return inc, nil
}

func (s *IncidentStore) AttachTask(ctx context.Context, id, taskID uuid.UUID) (domain.Incident, error) {
	inc, err := scanIncident(s.pool.QueryRow(ctx, `
		UPDATE prod_incidents SET task_id = $2 WHERE id = $1 RETURNING `+incidentCols, id, taskID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Incident{}, domain.ErrIncidentNotFound
		}
		return domain.Incident{}, fmt.Errorf("attach incident task: %w", err)
	}
	return inc, nil
}

func (s *IncidentStore) ByTask(ctx context.Context, taskID uuid.UUID) (domain.Incident, error) {
	inc, err := scanIncident(s.pool.QueryRow(ctx, `SELECT `+incidentCols+`
		FROM prod_incidents WHERE task_id = $1 ORDER BY last_seen_at DESC LIMIT 1`, taskID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Incident{}, domain.ErrIncidentNotFound
		}
		return domain.Incident{}, fmt.Errorf("incident by task: %w", err)
	}
	return inc, nil
}

func (s *IncidentStore) AppendEvent(ctx context.Context, incidentID uuid.UUID, kind, message string) error {
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO prod_incident_events (incident_id, kind, message) VALUES ($1, $2, $3)`,
		incidentID, kind, message); err != nil {
		return fmt.Errorf("append incident event: %w", err)
	}
	return nil
}

func (s *IncidentStore) ListEvents(ctx context.Context, incidentID uuid.UUID) ([]domain.IncidentEvent, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, incident_id, kind, message, created_at
		FROM prod_incident_events WHERE incident_id = $1 ORDER BY created_at`, incidentID)
	if err != nil {
		return nil, fmt.Errorf("list incident events: %w", err)
	}
	defer rows.Close()
	var out []domain.IncidentEvent
	for rows.Next() {
		var e domain.IncidentEvent
		if err := rows.Scan(&e.ID, &e.IncidentID, &e.Kind, &e.Message, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan incident event: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
