package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"slices"

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

// Every write to one (component, environment, signal) slot holds that
// slot's transaction-scoped advisory lock: a confirm reads the slot, deletes
// its other rows and promotes one, and two interleaving would trip the
// confirmed-row unique index or delete each other's winner. The key is a hash
// of the slot, the same in every process. Replacing a component
// environment's suggestions takes that environment's own lock first, then
// its slots' in sorted order, before touching a row; nothing else holds more
// than one, so no two writers wait on each other in a cycle.
const signalLockSQL = `SELECT pg_advisory_xact_lock($1)`

func signalLockKey(parts ...string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte("signal_bindings"))
	for _, p := range parts {
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(p))
	}
	return int64(h.Sum64())
}

func lockSignalSlot(ctx context.Context, tx pgx.Tx, componentID uuid.UUID, env domain.DeployEnvironment, signal string) error {
	if _, err := tx.Exec(ctx, signalLockSQL, signalLockKey("slot", componentID.String(), string(env), signal)); err != nil {
		return fmt.Errorf("lock signal slot: %w", err)
	}
	return nil
}

func slotWriteError(op string, err error) error {
	if isUniqueViolation(err) {
		return fmt.Errorf("%s: %w", op, port.ErrSignalBindingConflict)
	}
	return fmt.Errorf("%s: %w", op, err)
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
		if err := lockSignalSlot(ctx, tx, b.ComponentID, b.Environment, b.Signal); err != nil {
			return err
		}
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
		return domain.SignalBinding{}, slotWriteError("put signal binding", err)
	}
	return out, nil
}

func (s *SignalBindingStore) ConfirmSignalBinding(ctx context.Context, id uuid.UUID) (domain.SignalBinding, error) {
	var out domain.SignalBinding
	err := s.pool.InTx(ctx, func(tx pgx.Tx) error {
		var componentID uuid.UUID
		var env domain.DeployEnvironment
		var signal string
		err := tx.QueryRow(ctx, `SELECT component_id, environment, signal FROM signal_bindings WHERE id = $1`, id).
			Scan(&componentID, &env, &signal)
		if errors.Is(err, pgx.ErrNoRows) {
			return port.ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := lockSignalSlot(ctx, tx, componentID, env, signal); err != nil {
			return err
		}
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
		return domain.SignalBinding{}, slotWriteError("confirm signal binding", err)
	}
	return out, nil
}

func (s *SignalBindingStore) DismissSignalBinding(ctx context.Context, id uuid.UUID) (domain.SignalBinding, error) {
	b, err := scanSignalBinding(s.pool.QueryRow(ctx, `UPDATE signal_bindings SET status = 'dismissed', updated_at = now()
		WHERE id = $1 AND status = 'suggested' RETURNING `+signalBindingCols, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return s.GetSignalBinding(ctx, id)
	}
	if err != nil {
		return domain.SignalBinding{}, fmt.Errorf("dismiss signal binding: %w", err)
	}
	return b, nil
}

const insertSuggestionSQL = `
INSERT INTO signal_bindings (id, component_id, environment, signal, data_source_id, recipe_id, params, query, status, source)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'suggested','detected')
RETURNING ` + signalBindingCols

func (s *SignalBindingStore) ReplaceDetectedSuggestions(ctx context.Context, componentID uuid.UUID, env domain.DeployEnvironment, sources []uuid.UUID, suggestions []domain.SignalBinding) ([]domain.SignalBinding, error) {
	answered := make(map[uuid.UUID]bool, len(sources))
	for _, id := range sources {
		answered[id] = true
	}
	var out []domain.SignalBinding
	err := s.pool.InTx(ctx, func(tx pgx.Tx) error {
		out = []domain.SignalBinding{}
		if _, err := tx.Exec(ctx, signalLockSQL, signalLockKey("suggestions", componentID.String(), string(env))); err != nil {
			return fmt.Errorf("lock suggestions: %w", err)
		}
		slots, err := pendingDetectedSlots(ctx, tx, componentID, env)
		if err != nil {
			return err
		}
		for _, b := range suggestions {
			slots = append(slots, b.Signal)
		}
		slices.Sort(slots)
		for _, signal := range slices.Compact(slots) {
			if err := lockSignalSlot(ctx, tx, componentID, env, signal); err != nil {
				return err
			}
		}
		rows, err := tx.Query(ctx, `SELECT `+signalBindingCols+` FROM signal_bindings
			WHERE component_id = $1 AND environment = $2 AND status = 'suggested' AND source = 'detected'
			ORDER BY created_at, id`, componentID, env)
		if err != nil {
			return err
		}
		pending, err := collectSignalBindings(rows)
		if err != nil {
			return err
		}
		placed := make([]bool, len(suggestions))
		var drop []uuid.UUID
		for _, p := range pending {
			i := slices.IndexFunc(suggestions, func(b domain.SignalBinding) bool { return b.Signal == p.Signal && b.SameTarget(p) })
			switch {
			case i >= 0 && !placed[i]:
				placed[i] = true
				out = append(out, p)
			case answered[p.DataSourceID]:
				drop = append(drop, p.ID)
			default:
				out = append(out, p)
			}
		}
		if len(drop) > 0 {
			if _, err := tx.Exec(ctx, `DELETE FROM signal_bindings
				WHERE id = ANY($1) AND status = 'suggested' AND source = 'detected'`, drop); err != nil {
				return err
			}
		}
		for i, b := range suggestions {
			if placed[i] {
				continue
			}
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

func pendingDetectedSlots(ctx context.Context, tx pgx.Tx, componentID uuid.UUID, env domain.DeployEnvironment) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT signal FROM signal_bindings
		WHERE component_id = $1 AND environment = $2 AND status = 'suggested' AND source = 'detected'`, componentID, env)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
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
