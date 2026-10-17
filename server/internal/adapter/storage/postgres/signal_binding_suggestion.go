package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func (s *SignalBindingStore) AddSignalBindingSuggestion(ctx context.Context, b domain.SignalBinding) (domain.SignalBinding, error) {
	if b.ID == uuid.Nil {
		b.ID = uuid.New()
	}
	if b.Params == nil {
		b.Params = map[string]string{}
	}
	paramsJSON, err := json.Marshal(b.Params)
	if err != nil {
		return domain.SignalBinding{}, fmt.Errorf("add signal binding suggestion: %w", err)
	}
	var out domain.SignalBinding
	err = s.pool.InTx(ctx, func(tx pgx.Tx) error {
		if err := lockSignalSlot(ctx, tx, b.ComponentID, b.Environment, b.Signal); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT `+signalBindingCols+` FROM signal_bindings
			WHERE component_id = $1 AND environment = $2 AND signal = $3`, b.ComponentID, b.Environment, b.Signal)
		if err != nil {
			return fmt.Errorf("read signal slot: %w", err)
		}
		existing, err := collectSignalBindings(rows)
		if err != nil {
			return err
		}
		for _, e := range existing {
			if e.SameTarget(b) {
				out = e
				return nil
			}
		}
		out, err = scanSignalBinding(tx.QueryRow(ctx, `
			INSERT INTO signal_bindings (id, component_id, environment, signal, data_source_id, recipe_id, params, query, status, source)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'suggested',$9)
			RETURNING `+signalBindingCols,
			b.ID, b.ComponentID, b.Environment, b.Signal, b.DataSourceID, b.RecipeID, paramsJSON, b.Query, b.Source))
		return err
	})
	if err != nil {
		return domain.SignalBinding{}, fmt.Errorf("add signal binding suggestion: %w", err)
	}
	return out, nil
}
