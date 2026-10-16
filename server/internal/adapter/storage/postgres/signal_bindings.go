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

// SignalBindingStore persists signal bindings (migration 188).
type SignalBindingStore struct {
	pool *DB
}

func NewSignalBindingStore(pool *DB) *SignalBindingStore {
	return &SignalBindingStore{pool: pool}
}

var _ port.SignalBindingStore = (*SignalBindingStore)(nil)

const signalBindingCols = `id, component_id, environment, signal, data_source_id, recipe_id, params, query, status, source, created_at, updated_at`

func scanSignalBinding(row pgx.Row) (domain.SignalBinding, error) {
	var b domain.SignalBinding
	var paramsJSON []byte
	if err := row.Scan(
		&b.ID, &b.ComponentID, &b.Environment, &b.Signal, &b.DataSourceID, &b.RecipeID, &paramsJSON, &b.Query,
		&b.Status, &b.Source, &b.CreatedAt, &b.UpdatedAt,
	); err != nil {
		return domain.SignalBinding{}, err
	}
	if err := json.Unmarshal(paramsJSON, &b.Params); err != nil {
		return domain.SignalBinding{}, fmt.Errorf("unmarshal signal binding params: %w", err)
	}
	if b.Params == nil {
		b.Params = map[string]string{}
	}
	return b, nil
}

func collectSignalBindings(rows pgx.Rows) ([]domain.SignalBinding, error) {
	defer rows.Close()
	out := []domain.SignalBinding{}
	for rows.Next() {
		b, err := scanSignalBinding(rows)
		if err != nil {
			return nil, fmt.Errorf("scan signal binding: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *SignalBindingStore) ListSignalBindings(ctx context.Context, componentID uuid.UUID, env domain.DeployEnvironment) ([]domain.SignalBinding, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+signalBindingCols+` FROM signal_bindings
		WHERE component_id = $1 AND environment = $2 ORDER BY created_at, id`, componentID, env)
	if err != nil {
		return nil, fmt.Errorf("list signal bindings: %w", err)
	}
	return collectSignalBindings(rows)
}

func (s *SignalBindingStore) ListConfirmedSignalBindings(ctx context.Context, env domain.DeployEnvironment) ([]domain.SignalBinding, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+signalBindingCols+` FROM signal_bindings
		WHERE environment = $1 AND status = 'confirmed' ORDER BY component_id, signal`, env)
	if err != nil {
		return nil, fmt.Errorf("list confirmed signal bindings: %w", err)
	}
	return collectSignalBindings(rows)
}

func (s *SignalBindingStore) GetSignalBinding(ctx context.Context, id uuid.UUID) (domain.SignalBinding, error) {
	b, err := scanSignalBinding(s.pool.QueryRow(ctx, `SELECT `+signalBindingCols+` FROM signal_bindings WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.SignalBinding{}, fmt.Errorf("get signal binding: %w", port.ErrNotFound)
	}
	if err != nil {
		return domain.SignalBinding{}, fmt.Errorf("get signal binding: %w", err)
	}
	return b, nil
}

const upsertConfirmedSignalBindingSQL = `
INSERT INTO signal_bindings (id, component_id, environment, signal, data_source_id, recipe_id, params, query, status, source)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'confirmed',$9)
ON CONFLICT (component_id, environment, signal) WHERE status = 'confirmed' DO UPDATE SET
	data_source_id = EXCLUDED.data_source_id, recipe_id = EXCLUDED.recipe_id, params = EXCLUDED.params,
	query = EXCLUDED.query, source = EXCLUDED.source, updated_at = now()
RETURNING ` + signalBindingCols

func (s *SignalBindingStore) PutConfirmedSignalBinding(ctx context.Context, b domain.SignalBinding) (domain.SignalBinding, error) {
	if b.ID == uuid.Nil {
		b.ID = uuid.New()
	}
	paramsJSON, err := json.Marshal(nonNilMap(b.Params))
	if err != nil {
		return domain.SignalBinding{}, fmt.Errorf("put signal binding: marshal params: %w", err)
	}
	var out domain.SignalBinding
	err = s.pool.InTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM signal_bindings
			WHERE component_id = $1 AND environment = $2 AND signal = $3 AND status = 'suggested'`,
			b.ComponentID, b.Environment, b.Signal); err != nil {
			return err
		}
		var err error
		out, err = scanSignalBinding(tx.QueryRow(ctx, upsertConfirmedSignalBindingSQL,
			b.ID, b.ComponentID, b.Environment, b.Signal, b.DataSourceID, b.RecipeID, paramsJSON, b.Query, b.Source))
		return err
	})
	if err != nil {
		return domain.SignalBinding{}, fmt.Errorf("put signal binding: %w", err)
	}
	return out, nil
}

func (s *SignalBindingStore) ConfirmSignalBinding(ctx context.Context, id uuid.UUID) (domain.SignalBinding, error) {
	var out domain.SignalBinding
	err := s.pool.InTx(ctx, func(tx pgx.Tx) error {
		b, err := scanSignalBinding(tx.QueryRow(ctx, `SELECT `+signalBindingCols+` FROM signal_bindings WHERE id = $1 FOR UPDATE`, id))
		if errors.Is(err, pgx.ErrNoRows) {
			return port.ErrNotFound
		}
		if err != nil {
			return err
		}
		if b.Status == domain.SignalBindingConfirmed {
			out = b
			return nil
		}
		if _, err := tx.Exec(ctx, `DELETE FROM signal_bindings
			WHERE component_id = $1 AND environment = $2 AND signal = $3 AND id <> $4 AND status IN ('confirmed', 'suggested')`,
			b.ComponentID, b.Environment, b.Signal, b.ID); err != nil {
			return err
		}
		out, err = scanSignalBinding(tx.QueryRow(ctx, `UPDATE signal_bindings SET status = 'confirmed', updated_at = now()
			WHERE id = $1 RETURNING `+signalBindingCols, id))
		return err
	})
	if err != nil {
		return domain.SignalBinding{}, fmt.Errorf("confirm signal binding: %w", err)
	}
	return out, nil
}

func (s *SignalBindingStore) SetSignalBindingStatus(ctx context.Context, id uuid.UUID, status domain.SignalBindingStatus) (domain.SignalBinding, error) {
	b, err := scanSignalBinding(s.pool.QueryRow(ctx, `UPDATE signal_bindings SET status = $2, updated_at = now()
		WHERE id = $1 RETURNING `+signalBindingCols, id, status))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.SignalBinding{}, fmt.Errorf("set signal binding status: %w", port.ErrNotFound)
	}
	if err != nil {
		return domain.SignalBinding{}, fmt.Errorf("set signal binding status: %w", err)
	}
	return b, nil
}

const insertSuggestionSQL = `
INSERT INTO signal_bindings (id, component_id, environment, signal, data_source_id, recipe_id, params, query, status, source)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'suggested','detected')
RETURNING ` + signalBindingCols

func (s *SignalBindingStore) ReplaceDetectedSuggestions(ctx context.Context, componentID uuid.UUID, env domain.DeployEnvironment, suggestions []domain.SignalBinding) ([]domain.SignalBinding, error) {
	out := make([]domain.SignalBinding, 0, len(suggestions))
	err := s.pool.InTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM signal_bindings
			WHERE component_id = $1 AND environment = $2 AND status = 'suggested' AND source = 'detected'`,
			componentID, env); err != nil {
			return err
		}
		for _, b := range suggestions {
			if b.ID == uuid.Nil {
				b.ID = uuid.New()
			}
			paramsJSON, err := json.Marshal(nonNilMap(b.Params))
			if err != nil {
				return fmt.Errorf("marshal params: %w", err)
			}
			saved, err := scanSignalBinding(tx.QueryRow(ctx, insertSuggestionSQL,
				b.ID, componentID, env, b.Signal, b.DataSourceID, b.RecipeID, paramsJSON, b.Query))
			if err != nil {
				return err
			}
			out = append(out, saved)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("replace signal suggestions: %w", err)
	}
	return out, nil
}

func (s *SignalBindingStore) DeleteSignalBinding(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM signal_bindings WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete signal binding: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("delete signal binding: %w", port.ErrNotFound)
	}
	return nil
}
