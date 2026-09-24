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

// EnvironmentStore persists ComponentEnvironment (migration 155): the binding
// of one component's environment to where it runs.
type EnvironmentStore struct {
	pool *DB
}

func NewEnvironmentStore(pool *DB) *EnvironmentStore {
	return &EnvironmentStore{pool: pool}
}

var _ port.EnvironmentStore = (*EnvironmentStore)(nil)

const environmentCols = `id, repository_id, component_id, environment, provider, account_id, resource, url, health_url, status, source, confidence, reason, auto_confirmed, candidates, signal_key, health, created_at, updated_at`

const environmentColsPrefixed = `ce.id, ce.repository_id, ce.component_id, ce.environment, ce.provider, ce.account_id, ce.resource, ce.url, ce.health_url, ce.status, ce.source, ce.confidence, ce.reason, ce.auto_confirmed, ce.candidates, ce.signal_key, ce.health, ce.created_at, ce.updated_at`

// environmentOrderCase ranks the four deploy environments in the order the
// Deploy & Runtime tab presents them; anything else (a custom environment
// name) sorts after all of them rather than colliding with one.
const environmentOrderCase = `CASE ce.environment
	WHEN 'production' THEN 0
	WHEN 'staging' THEN 1
	WHEN 'preview' THEN 2
	WHEN 'development' THEN 3
	ELSE 4
END`

func scanEnvironment(row pgx.Row) (domain.ComponentEnvironment, error) {
	var e domain.ComponentEnvironment
	var resourceJSON, candidatesJSON, healthJSON []byte
	if err := row.Scan(
		&e.ID, &e.RepositoryID, &e.ComponentID, &e.Environment, &e.Provider, &e.AccountID, &resourceJSON,
		&e.URL, &e.HealthURL, &e.Status, &e.Source, &e.Confidence, &e.Reason, &e.AutoConfirmed,
		&candidatesJSON, &e.SignalKey, &healthJSON, &e.CreatedAt, &e.UpdatedAt,
	); err != nil {
		return domain.ComponentEnvironment{}, err
	}
	if resourceJSON != nil {
		var resource domain.CloudResourceRef
		if err := json.Unmarshal(resourceJSON, &resource); err != nil {
			return domain.ComponentEnvironment{}, fmt.Errorf("unmarshal environment resource: %w", err)
		}
		e.Resource = &resource
	}
	if err := json.Unmarshal(candidatesJSON, &e.Candidates); err != nil {
		return domain.ComponentEnvironment{}, fmt.Errorf("unmarshal environment candidates: %w", err)
	}
	if healthJSON != nil {
		var health domain.EnvironmentHealth
		if err := json.Unmarshal(healthJSON, &health); err != nil {
			return domain.ComponentEnvironment{}, fmt.Errorf("unmarshal environment health: %w", err)
		}
		e.Health = &health
	}
	return e, nil
}

const upsertEnvironmentSQL = `
INSERT INTO component_environments
	(id, repository_id, component_id, environment, provider, account_id, resource, url, health_url, status, source, confidence, reason, auto_confirmed, candidates, signal_key, health)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
ON CONFLICT (component_id, environment) DO UPDATE SET
	repository_id = EXCLUDED.repository_id,
	provider = EXCLUDED.provider,
	account_id = EXCLUDED.account_id,
	resource = EXCLUDED.resource,
	url = EXCLUDED.url,
	health_url = EXCLUDED.health_url,
	status = EXCLUDED.status,
	source = EXCLUDED.source,
	confidence = EXCLUDED.confidence,
	reason = EXCLUDED.reason,
	auto_confirmed = EXCLUDED.auto_confirmed,
	candidates = EXCLUDED.candidates,
	signal_key = EXCLUDED.signal_key,
	health = EXCLUDED.health,
	updated_at = now()
RETURNING ` + environmentCols

// id and created_at are deliberately absent from the SET list above: the
// conflict target is (component_id, environment), not id, so an upsert that
// resolves an existing row keeps that row's original id and created_at even
// when the caller minted a fresh id for what it thought was an insert.
func (s *EnvironmentStore) SaveEnvironment(ctx context.Context, e domain.ComponentEnvironment) (domain.ComponentEnvironment, error) {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	status := e.Status
	if status == "" {
		status = domain.LinkSuggested
	}
	source := e.Source
	if source == "" {
		source = domain.LinkSourceScan
	}
	confidence := e.Confidence
	if confidence == "" {
		confidence = domain.ConfidenceLow
	}
	var resourceJSON []byte
	if e.Resource != nil {
		var err error
		resourceJSON, err = json.Marshal(e.Resource)
		if err != nil {
			return domain.ComponentEnvironment{}, fmt.Errorf("marshal environment resource: %w", err)
		}
	}
	candidatesJSON, err := json.Marshal(nonNilSlice(e.Candidates))
	if err != nil {
		return domain.ComponentEnvironment{}, fmt.Errorf("marshal environment candidates: %w", err)
	}
	var healthJSON []byte
	if e.Health != nil {
		healthJSON, err = json.Marshal(e.Health)
		if err != nil {
			return domain.ComponentEnvironment{}, fmt.Errorf("marshal environment health: %w", err)
		}
	}
	row := s.pool.QueryRow(ctx, upsertEnvironmentSQL,
		e.ID, e.RepositoryID, e.ComponentID, e.Environment, e.Provider, e.AccountID, resourceJSON,
		e.URL, e.HealthURL, status, source, confidence, e.Reason, e.AutoConfirmed, candidatesJSON, e.SignalKey, healthJSON)
	out, err := scanEnvironment(row)
	if err != nil {
		return domain.ComponentEnvironment{}, fmt.Errorf("save environment: %w", err)
	}
	return out, nil
}

func (s *EnvironmentStore) ListEnvironments(ctx context.Context, repositoryID uuid.UUID) ([]domain.ComponentEnvironment, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+environmentColsPrefixed+`
		FROM component_environments ce
		JOIN project_components pc ON pc.id = ce.component_id
		WHERE ce.repository_id = $1
		ORDER BY pc.path, `+environmentOrderCase, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("list environments: %w", err)
	}
	defer rows.Close()
	var out []domain.ComponentEnvironment
	for rows.Next() {
		e, err := scanEnvironment(rows)
		if err != nil {
			return nil, fmt.Errorf("scan environment: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *EnvironmentStore) ListAllEnvironments(ctx context.Context) ([]domain.ComponentEnvironment, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+environmentColsPrefixed+`
		FROM component_environments ce
		JOIN project_components pc ON pc.id = ce.component_id
		ORDER BY ce.repository_id, pc.path, `+environmentOrderCase)
	if err != nil {
		return nil, fmt.Errorf("list all environments: %w", err)
	}
	defer rows.Close()
	var out []domain.ComponentEnvironment
	for rows.Next() {
		e, err := scanEnvironment(rows)
		if err != nil {
			return nil, fmt.Errorf("scan environment: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *EnvironmentStore) GetEnvironment(ctx context.Context, id uuid.UUID) (domain.ComponentEnvironment, error) {
	e, err := scanEnvironment(s.pool.QueryRow(ctx, `SELECT `+environmentCols+`
		FROM component_environments WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ComponentEnvironment{}, fmt.Errorf("get environment: %w", port.ErrNotFound)
	}
	if err != nil {
		return domain.ComponentEnvironment{}, fmt.Errorf("get environment: %w", err)
	}
	return e, nil
}

func (s *EnvironmentStore) DeleteEnvironment(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM component_environments WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete environment: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("delete environment: %w", port.ErrNotFound)
	}
	return nil
}
