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

// AlertRuleStore persists alert rules and their events (migrations 189, 190,
// 192).
type AlertRuleStore struct {
	pool *DB
}

func NewAlertRuleStore(pool *DB) *AlertRuleStore {
	return &AlertRuleStore{pool: pool}
}

var _ port.AlertRuleStore = (*AlertRuleStore)(nil)

const alertRuleCols = `id, name, component_id, environment, signal, data_source_id, query, condition, severity, channels,
	open_incident, enabled, state, state_since, last_value, last_evaluated_at, clear_count, clear_since, no_data_count,
	error_count, last_error, created_by, created_at, updated_at`

func scanAlertRule(row pgx.Row) (domain.AlertRule, error) {
	var r domain.AlertRule
	var conditionJSON []byte
	var channels []string
	if err := row.Scan(
		&r.ID, &r.Name, &r.ComponentID, &r.Environment, &r.Signal, &r.DataSourceID, &r.Query, &conditionJSON, &r.Severity,
		&channels, &r.OpenIncident, &r.Enabled, &r.State, &r.StateSince, &r.LastValue, &r.LastEvaluatedAt,
		&r.ClearCount, &r.ClearSince, &r.NoDataCount, &r.ErrorCount, &r.LastError, &r.CreatedBy, &r.CreatedAt, &r.UpdatedAt,
	); err != nil {
		return domain.AlertRule{}, err
	}
	if err := json.Unmarshal(conditionJSON, &r.Condition); err != nil {
		return domain.AlertRule{}, fmt.Errorf("unmarshal alert rule condition: %w", err)
	}
	r.Channels = make([]domain.NotificationChannelKind, 0, len(channels))
	for _, c := range channels {
		r.Channels = append(r.Channels, domain.NotificationChannelKind(c))
	}
	return r, nil
}

func collectAlertRules(rows pgx.Rows) ([]domain.AlertRule, error) {
	defer rows.Close()
	out := []domain.AlertRule{}
	for rows.Next() {
		r, err := scanAlertRule(rows)
		if err != nil {
			return nil, fmt.Errorf("scan alert rule: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func alertRuleArgs(r domain.AlertRule) ([]byte, []string, error) {
	conditionJSON, err := json.Marshal(r.Condition)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal condition: %w", err)
	}
	channels := make([]string, 0, len(r.Channels))
	for _, c := range r.Channels {
		channels = append(channels, string(c))
	}
	return conditionJSON, channels, nil
}

func (s *AlertRuleStore) ListAlertRules(ctx context.Context, filter domain.AlertRuleFilter) ([]domain.AlertRule, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+alertRuleCols+` FROM alert_rules
		WHERE ($1::uuid IS NULL OR component_id = $1) AND ($2 = '' OR environment = $2)
		ORDER BY lower(name), id`, filter.ComponentID, string(filter.Environment))
	if err != nil {
		return nil, fmt.Errorf("list alert rules: %w", err)
	}
	return collectAlertRules(rows)
}

func (s *AlertRuleStore) GetAlertRule(ctx context.Context, id uuid.UUID) (domain.AlertRule, error) {
	r, err := scanAlertRule(s.pool.QueryRow(ctx, `SELECT `+alertRuleCols+` FROM alert_rules WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AlertRule{}, fmt.Errorf("get alert rule: %w", port.ErrNotFound)
	}
	if err != nil {
		return domain.AlertRule{}, fmt.Errorf("get alert rule: %w", err)
	}
	return r, nil
}

func alertRuleCreator(c domain.AlertRuleCreator) string {
	if c == "" {
		return string(domain.AlertRuleByHuman)
	}
	return string(c)
}

func (s *AlertRuleStore) CreateAlertRule(ctx context.Context, r domain.AlertRule) (domain.AlertRule, error) {
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	conditionJSON, channels, err := alertRuleArgs(r)
	if err != nil {
		return domain.AlertRule{}, fmt.Errorf("create alert rule: %w", err)
	}
	out, err := scanAlertRule(s.pool.QueryRow(ctx, `
		INSERT INTO alert_rules (id, name, component_id, environment, signal, data_source_id, query, condition, severity,
			channels, open_incident, enabled, state, state_since, last_value, last_evaluated_at, clear_count, clear_since, no_data_count,
			created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)
		RETURNING `+alertRuleCols,
		r.ID, r.Name, r.ComponentID, r.Environment, r.Signal, r.DataSourceID, r.Query, conditionJSON, r.Severity,
		channels, r.OpenIncident, r.Enabled, r.State, r.StateSince, r.LastValue, r.LastEvaluatedAt,
		r.ClearCount, r.ClearSince, r.NoDataCount, alertRuleCreator(r.CreatedBy)))
	if err != nil {
		return domain.AlertRule{}, fmt.Errorf("create alert rule: %w", err)
	}
	return out, nil
}

func (s *AlertRuleStore) UpdateAlertRule(ctx context.Context, r domain.AlertRule) (domain.AlertRule, error) {
	conditionJSON, channels, err := alertRuleArgs(r)
	if err != nil {
		return domain.AlertRule{}, fmt.Errorf("update alert rule: %w", err)
	}
	out, err := scanAlertRule(s.pool.QueryRow(ctx, `
		UPDATE alert_rules SET name = $2, component_id = $3, environment = $4, signal = $5, data_source_id = $6,
			query = $7, condition = $8, severity = $9, channels = $10, open_incident = $11, enabled = $12,
			state = $13, state_since = $14, last_value = $15, last_evaluated_at = $16,
			clear_count = $17, clear_since = $18, no_data_count = $19, error_count = $20, last_error = $21, updated_at = now()
		WHERE id = $1
		RETURNING `+alertRuleCols,
		r.ID, r.Name, r.ComponentID, r.Environment, r.Signal, r.DataSourceID, r.Query, conditionJSON, r.Severity,
		channels, r.OpenIncident, r.Enabled, r.State, r.StateSince, r.LastValue, r.LastEvaluatedAt,
		r.ClearCount, r.ClearSince, r.NoDataCount, r.ErrorCount, r.LastError))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AlertRule{}, fmt.Errorf("update alert rule: %w", port.ErrNotFound)
	}
	if err != nil {
		return domain.AlertRule{}, fmt.Errorf("update alert rule: %w", err)
	}
	return out, nil
}

func (s *AlertRuleStore) DeleteAlertRule(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM alert_rules WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete alert rule: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("delete alert rule: %w", port.ErrNotFound)
	}
	return nil
}

func (s *AlertRuleStore) DueAlertRules(ctx context.Context, before time.Time, limit int) ([]domain.AlertRule, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+alertRuleCols+` FROM alert_rules
		WHERE enabled AND (last_evaluated_at IS NULL OR last_evaluated_at <= $1)
		ORDER BY last_evaluated_at NULLS FIRST, id
		LIMIT $2`, before, limit)
	if err != nil {
		return nil, fmt.Errorf("list due alert rules: %w", err)
	}
	return collectAlertRules(rows)
}

const insertAlertEventSQL = `INSERT INTO alert_events (id, rule_id, at, from_state, to_state, value, incident_id)
	VALUES ($1,$2,$3,$4,$5,$6,$7)`

func alertEventArgs(e domain.AlertEvent) []any {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	return []any{e.ID, e.RuleID, e.At, e.FromState, e.ToState, e.Value, e.IncidentID}
}

func (s *AlertRuleStore) RecordAlertEvaluation(ctx context.Context, id uuid.UUID, specVersion time.Time, eval domain.AlertEvaluation, event *domain.AlertEvent) (domain.AlertRule, bool, error) {
	var out domain.AlertRule
	found := false
	err := s.pool.InTx(ctx, func(tx pgx.Tx) error {
		r, err := scanAlertRule(tx.QueryRow(ctx, `
			UPDATE alert_rules SET state = $3, state_since = $4, last_value = $5, last_evaluated_at = $6,
				clear_count = $7, clear_since = $8, no_data_count = $9, error_count = $10, last_error = $11
			WHERE id = $1 AND updated_at = $2
			RETURNING `+alertRuleCols, id, specVersion, eval.State, eval.StateSince, eval.Value, eval.At,
			eval.Streak.ClearCount, eval.Streak.ClearSince, eval.Streak.NoDataCount, eval.Errors.ErrorCount, eval.Errors.LastError))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		out, found = r, true
		if event == nil {
			return nil
		}
		e := *event
		e.RuleID = id
		_, err = tx.Exec(ctx, insertAlertEventSQL, alertEventArgs(e)...)
		return err
	})
	if err != nil {
		return domain.AlertRule{}, false, fmt.Errorf("record alert evaluation: %w", err)
	}
	return out, found, nil
}

func (s *AlertRuleStore) AppendAlertEvent(ctx context.Context, event domain.AlertEvent) error {
	if _, err := s.pool.Exec(ctx, insertAlertEventSQL, alertEventArgs(event)...); err != nil {
		return fmt.Errorf("append alert event: %w", err)
	}
	return nil
}

func (s *AlertRuleStore) ListAlertEvents(ctx context.Context, ruleID uuid.UUID, limit int) ([]domain.AlertEvent, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, rule_id, at, from_state, to_state, value, incident_id FROM alert_events
		WHERE rule_id = $1 ORDER BY at DESC, id LIMIT $2`, ruleID, limit)
	if err != nil {
		return nil, fmt.Errorf("list alert events: %w", err)
	}
	defer rows.Close()
	out := []domain.AlertEvent{}
	for rows.Next() {
		var e domain.AlertEvent
		if err := rows.Scan(&e.ID, &e.RuleID, &e.At, &e.FromState, &e.ToState, &e.Value, &e.IncidentID); err != nil {
			return nil, fmt.Errorf("scan alert event: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *AlertRuleStore) PruneAlertEvents(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM alert_events WHERE at < $1`, before)
	if err != nil {
		return 0, fmt.Errorf("prune alert events: %w", err)
	}
	return tag.RowsAffected(), nil
}
