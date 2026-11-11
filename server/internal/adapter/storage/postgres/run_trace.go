package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type RunTraceStore struct{ pool *DB }

func NewRunTraceStore(pool *DB) *RunTraceStore { return &RunTraceStore{pool: pool} }

var _ port.RunTraceStore = (*RunTraceStore)(nil)

func (s *RunTraceStore) Begin(ctx context.Context, t domain.RunTrace) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO run_traces (run_id, task_id, agent_id, base_sha, branch, workspace_path, column_before, remote, started_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT DO NOTHING`,
		t.RunID, t.TaskID, t.AgentID, t.BaseSHA, t.Branch, t.WorkspacePath, t.ColumnBefore, t.Remote, t.StartedAt)
	return err
}

func (s *RunTraceStore) Header(ctx context.Context, runID uuid.UUID) (domain.RunTrace, error) {
	var t domain.RunTrace
	var verification []byte
	err := s.pool.QueryRow(ctx, `
		SELECT t.run_id, t.task_id, t.agent_id, t.base_sha, t.head_sha, t.branch, t.workspace_path, t.column_before,
		       t.remote, t.dropped, t.verification, t.started_at, t.finished_at,
		       (SELECT count(*) FROM run_trace_events e WHERE e.run_id = t.run_id)
		FROM run_traces t WHERE t.run_id = $1`, runID).
		Scan(&t.RunID, &t.TaskID, &t.AgentID, &t.BaseSHA, &t.HeadSHA, &t.Branch, &t.WorkspacePath, &t.ColumnBefore,
			&t.Remote, &t.Dropped, &verification, &t.StartedAt, &t.FinishedAt, &t.Events)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, domain.ErrRunTraceNotFound
	}
	if err != nil {
		return t, err
	}
	if len(verification) > 0 {
		var v domain.RunTraceVerification
		if json.Unmarshal(verification, &v) == nil {
			t.Verification = &v
		}
	}
	return t, nil
}

func (s *RunTraceStore) ListByTask(ctx context.Context, taskID uuid.UUID) ([]domain.RunTrace, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT run_id, task_id, agent_id, base_sha, head_sha, branch, column_before, remote, dropped, verification,
		       started_at, finished_at
		FROM run_traces WHERE task_id = $1 ORDER BY started_at`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.RunTrace{}
	for rows.Next() {
		var t domain.RunTrace
		var verification []byte
		if err := rows.Scan(&t.RunID, &t.TaskID, &t.AgentID, &t.BaseSHA, &t.HeadSHA, &t.Branch, &t.ColumnBefore,
			&t.Remote, &t.Dropped, &verification, &t.StartedAt, &t.FinishedAt); err != nil {
			return nil, err
		}
		if len(verification) > 0 {
			var v domain.RunTraceVerification
			if json.Unmarshal(verification, &v) == nil {
				t.Verification = &v
			}
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *RunTraceStore) NextSeq(ctx context.Context, runID uuid.UUID) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT COALESCE(max(seq), 0) + 1 FROM run_trace_events WHERE run_id = $1`, runID).Scan(&n)
	return n, err
}

func (s *RunTraceStore) Append(ctx context.Context, events []domain.RunTraceEvent) error {
	if len(events) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, e := range events {
		detail := e.Detail
		if len(detail) == 0 {
			detail = json.RawMessage(`{}`)
		}
		batch.Queue(`
			INSERT INTO run_trace_events (run_id, seq, task_id, agent_id, at, duration_ms, tool, native_tool, source, category, target, status, error, detail)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
			ON CONFLICT DO NOTHING`,
			e.RunID, e.Seq, e.TaskID, e.AgentID, e.At, e.DurationMS, e.Tool, e.NativeTool, e.Source, e.Category,
			e.Target, e.Status, e.Error, []byte(detail))
	}
	res := s.pool.SendBatch(ctx, batch)
	defer func() { _ = res.Close() }()
	for range events {
		if _, err := res.Exec(); err != nil {
			return err
		}
	}
	return nil
}

func (s *RunTraceStore) Finish(ctx context.Context, runID uuid.UUID, headSHA string, dropped int, v *domain.RunTraceVerification) error {
	var raw []byte
	if v != nil {
		raw, _ = json.Marshal(v)
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE run_traces SET head_sha = $2, dropped = $3, verification = $4, finished_at = now()
		WHERE run_id = $1`, runID, headSHA, dropped, raw)
	return err
}

const traceEventColumns = `run_id, task_id, agent_id, seq, at, duration_ms, tool, native_tool, source, category, target, status, error, detail`

func scanTraceEvents(rows pgx.Rows) ([]domain.RunTraceEvent, error) {
	defer rows.Close()
	out := []domain.RunTraceEvent{}
	for rows.Next() {
		var e domain.RunTraceEvent
		var detail []byte
		if err := rows.Scan(&e.RunID, &e.TaskID, &e.AgentID, &e.Seq, &e.At, &e.DurationMS, &e.Tool, &e.NativeTool,
			&e.Source, &e.Category, &e.Target, &e.Status, &e.Error, &detail); err != nil {
			return nil, err
		}
		e.Detail = detail
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *RunTraceStore) List(ctx context.Context, runID uuid.UUID, f port.RunTraceFilter) ([]domain.RunTraceEvent, error) {
	limit := f.Limit
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+traceEventColumns+` FROM run_trace_events
		WHERE run_id = $1 AND seq > $2 AND ($3 = '' OR category = $3)
		ORDER BY seq LIMIT $4`, runID, f.After, f.Category, limit)
	if err != nil {
		return nil, err
	}
	return scanTraceEvents(rows)
}

func (s *RunTraceStore) Counts(ctx context.Context, runID uuid.UUID) (domain.RunTraceCounts, error) {
	var c domain.RunTraceCounts
	err := s.pool.QueryRow(ctx, `
		SELECT
		  count(*) FILTER (WHERE category = 'read'),
		  count(*) FILTER (WHERE category = 'change'),
		  count(*) FILTER (WHERE category = 'exec'),
		  count(*) FILTER (WHERE category = 'external'),
		  count(*) FILTER (WHERE category = 'board'),
		  count(*) FILTER (WHERE category = 'undo'),
		  count(*) FILTER (WHERE status <> 'ok'),
		  count(*),
		  count(DISTINCT detail->>'path') FILTER (WHERE category = 'read' AND detail->>'path' IS NOT NULL),
		  count(DISTINCT detail->>'path') FILTER (WHERE category = 'change' AND status = 'ok' AND detail->>'path' IS NOT NULL)
		FROM run_trace_events WHERE run_id = $1`, runID).
		Scan(&c.Read, &c.Change, &c.Exec, &c.External, &c.Board, &c.Undo, &c.Failed, &c.Total, &c.ReadFiles, &c.ChangedFiles)
	return c, err
}

func (s *RunTraceStore) Effects(ctx context.Context, runID uuid.UUID) ([]domain.RunTraceEvent, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+traceEventColumns+` FROM run_trace_events
		WHERE run_id = $1 AND detail ? 'effect' ORDER BY seq`, runID)
	if err != nil {
		return nil, err
	}
	return scanTraceEvents(rows)
}

func (s *RunTraceStore) Changes(ctx context.Context, runID uuid.UUID) ([]domain.RunTraceEvent, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+traceEventColumns+` FROM run_trace_events
		WHERE run_id = $1 AND category = 'change' ORDER BY seq`, runID)
	if err != nil {
		return nil, err
	}
	return scanTraceEvents(rows)
}

func (s *RunTraceStore) DeleteBefore(ctx context.Context, cutoff time.Time, limit int) (int, error) {
	if limit <= 0 {
		limit = 500
	}
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM run_traces WHERE run_id IN (
			SELECT run_id FROM run_traces WHERE started_at < $1 ORDER BY started_at LIMIT $2)`, cutoff, limit)
	return int(tag.RowsAffected()), err
}
