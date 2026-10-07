package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type BoardConfigStore struct {
	pool *DB
}

func NewBoardConfigStore(pool *DB) *BoardConfigStore {
	return &BoardConfigStore{pool: pool}
}

func (s *BoardConfigStore) GetSettings(ctx context.Context) (domain.BoardSettings, error) {
	var settings domain.BoardSettings
	err := s.pool.QueryRow(ctx, `SELECT key_prefix FROM board_settings WHERE id = 1`).Scan(&settings.KeyPrefix)
	if err != nil {
		return domain.BoardSettings{}, fmt.Errorf("get board settings: %w", err)
	}
	return settings, nil
}

func (s *BoardConfigStore) UpdateSettings(ctx context.Context, keyPrefix string) (domain.BoardSettings, error) {
	var settings domain.BoardSettings
	err := s.pool.QueryRow(ctx, `
		INSERT INTO board_settings (id, key_prefix) VALUES (1, $1)
		ON CONFLICT (id) DO UPDATE SET key_prefix = EXCLUDED.key_prefix
		RETURNING key_prefix
	`, keyPrefix).Scan(&settings.KeyPrefix)
	if err != nil {
		return domain.BoardSettings{}, fmt.Errorf("update board settings: %w", err)
	}
	return settings, nil
}

func (s *BoardConfigStore) ListColumns(ctx context.Context) ([]domain.BoardColumn, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, slug, label, position, is_backlog
		FROM board_columns ORDER BY position ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list board columns: %w", err)
	}
	defer rows.Close()
	var cols []domain.BoardColumn
	for rows.Next() {
		var c domain.BoardColumn
		if err := rows.Scan(&c.ID, &c.Slug, &c.Label, &c.Position, &c.IsBacklog); err != nil {
			return nil, err
		}
		cols = append(cols, c)
	}
	return cols, rows.Err()
}

func (s *BoardConfigStore) ReplaceColumns(ctx context.Context, columns []domain.BoardColumnInput) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM board_columns`); err != nil {
		return fmt.Errorf("clear board columns: %w", err)
	}
	for _, col := range columns {
		if _, err := tx.Exec(ctx, `
			INSERT INTO board_columns (slug, label, position, is_backlog)
			VALUES ($1, $2, $3, $4)
		`, col.Slug, col.Label, col.Position, col.IsBacklog); err != nil {
			return fmt.Errorf("insert board column: %w", err)
		}
	}
	return tx.Commit(ctx)
}

func (s *BoardConfigStore) ListMembers(ctx context.Context) ([]domain.BoardMember, error) {
	rows, err := s.pool.Query(ctx, `SELECT agent_id FROM board_members`)
	if err != nil {
		return nil, fmt.Errorf("list board members: %w", err)
	}
	defer rows.Close()
	var members []domain.BoardMember
	for rows.Next() {
		var m domain.BoardMember
		if err := rows.Scan(&m.AgentID); err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

func (s *BoardConfigStore) SetMembers(ctx context.Context, agentIDs []uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM board_members`); err != nil {
		return err
	}
	for _, agentID := range agentIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO board_members (agent_id) VALUES ($1)
		`, agentID); err != nil {
			return fmt.Errorf("insert board member: %w", err)
		}
	}
	return tx.Commit(ctx)
}

func (s *BoardConfigStore) ListSubscriptions(ctx context.Context) ([]domain.BoardSubscription, error) {
	rows, err := s.pool.Query(ctx, `SELECT agent_id, column_slug FROM agent_column_subscriptions`)
	if err != nil {
		return nil, fmt.Errorf("list subscriptions: %w", err)
	}
	defer rows.Close()
	var subs []domain.BoardSubscription
	for rows.Next() {
		var sub domain.BoardSubscription
		if err := rows.Scan(&sub.AgentID, &sub.ColumnSlug); err != nil {
			return nil, err
		}
		subs = append(subs, sub)
	}
	return subs, rows.Err()
}

func (s *BoardConfigStore) SetSubscriptions(ctx context.Context, subs []domain.BoardSubscriptionInput) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM agent_column_subscriptions`); err != nil {
		return err
	}
	for _, sub := range subs {
		var filter interface{}
		if len(sub.TaskTypeFilter) > 0 {
			filter = sub.TaskTypeFilter
		}
		for _, slug := range sub.ColumnSlugs {
			if _, err := tx.Exec(ctx, `
				INSERT INTO agent_column_subscriptions (agent_id, column_slug, task_type_filter)
				VALUES ($1, $2, $3)
			`, sub.AgentID, slug, filter); err != nil {
				return fmt.Errorf("insert subscription: %w", err)
			}
		}
	}
	return tx.Commit(ctx)
}

func (s *BoardConfigStore) ListAgentSubscriptions(ctx context.Context, agentID uuid.UUID) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT column_slug FROM agent_column_subscriptions WHERE agent_id = $1 ORDER BY column_slug
	`, agentID)
	if err != nil {
		return nil, fmt.Errorf("list agent subscriptions: %w", err)
	}
	defer rows.Close()
	var slugs []string
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			return nil, err
		}
		slugs = append(slugs, slug)
	}
	return slugs, rows.Err()
}

func (s *BoardConfigStore) SetAgentSubscriptions(ctx context.Context, agentID uuid.UUID, columnSlugs []string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM agent_column_subscriptions WHERE agent_id = $1`, agentID); err != nil {
		return err
	}
	for _, slug := range columnSlugs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO agent_column_subscriptions (agent_id, column_slug) VALUES ($1, $2)
			ON CONFLICT DO NOTHING
		`, agentID, slug); err != nil {
			return fmt.Errorf("insert agent subscription: %w", err)
		}
	}
	return tx.Commit(ctx)
}

// ListAgentSubscriptionsDetailed is ListAgentSubscriptions plus the per-column
// task_type_filter ListAgentSubscriptions drops on the floor.
func (s *BoardConfigStore) ListAgentSubscriptionsDetailed(ctx context.Context, agentID uuid.UUID) ([]domain.AgentColumnSubscription, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT column_slug, task_type_filter FROM agent_column_subscriptions WHERE agent_id = $1 ORDER BY column_slug
	`, agentID)
	if err != nil {
		return nil, fmt.Errorf("list agent subscriptions: %w", err)
	}
	defer rows.Close()
	var out []domain.AgentColumnSubscription
	for rows.Next() {
		var sub domain.AgentColumnSubscription
		if err := rows.Scan(&sub.ColumnSlug, &sub.TaskTypes); err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}

// SetAgentSubscriptionsDetailed is SetAgentSubscriptions with the filter
// preserved — the fix for the bug SetAgentSubscriptions has always had (it
// silently drops task_type_filter on every save, see release-b-plan.md §0).
func (s *BoardConfigStore) SetAgentSubscriptionsDetailed(ctx context.Context, agentID uuid.UUID, subs []domain.AgentColumnSubscription) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM agent_column_subscriptions WHERE agent_id = $1`, agentID); err != nil {
		return err
	}
	for _, sub := range subs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO agent_column_subscriptions (agent_id, column_slug, task_type_filter) VALUES ($1, $2, $3)
			ON CONFLICT DO NOTHING
		`, agentID, sub.ColumnSlug, sub.TaskTypes); err != nil {
			return fmt.Errorf("insert agent subscription: %w", err)
		}
	}
	return tx.Commit(ctx)
}

// ListAgentColumnInstructions reads an agent's per-column prompts, sorted by
// column slug. SetAgentColumnInstruction upserts one; an empty instruction
// deletes the row, so "reset to none" and "not configured" are the same answer
// for dispatch.
func (s *BoardConfigStore) ListAgentColumnInstructions(ctx context.Context, agentID uuid.UUID) ([]domain.AgentColumnInstruction, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT column_slug, instruction, catalog_sha FROM agent_column_instructions WHERE agent_id = $1 ORDER BY column_slug
	`, agentID)
	if err != nil {
		return nil, fmt.Errorf("list agent column instructions: %w", err)
	}
	defer rows.Close()
	var out []domain.AgentColumnInstruction
	for rows.Next() {
		var ins domain.AgentColumnInstruction
		if err := rows.Scan(&ins.ColumnSlug, &ins.Instruction, &ins.CatalogSHA); err != nil {
			return nil, err
		}
		out = append(out, ins)
	}
	return out, rows.Err()
}

// The operator path: clearing catalog_sha makes the row the operator's.
func (s *BoardConfigStore) SetAgentColumnInstruction(ctx context.Context, agentID uuid.UUID, columnSlug, instruction string) error {
	if strings.TrimSpace(instruction) == "" {
		return s.DeleteAgentColumnInstruction(ctx, agentID, columnSlug)
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO agent_column_instructions (agent_id, column_slug, instruction, catalog_sha) VALUES ($1, $2, $3, '')
		ON CONFLICT (agent_id, column_slug) DO UPDATE SET instruction = EXCLUDED.instruction, catalog_sha = ''
	`, agentID, columnSlug, instruction); err != nil {
		return fmt.Errorf("upsert agent column instruction: %w", err)
	}
	return nil
}

func (s *BoardConfigStore) SetCatalogColumnInstruction(ctx context.Context, agentID uuid.UUID, columnSlug, instruction, sha string) error {
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO agent_column_instructions (agent_id, column_slug, instruction, catalog_sha) VALUES ($1, $2, $3, $4)
		ON CONFLICT (agent_id, column_slug) DO UPDATE SET instruction = EXCLUDED.instruction, catalog_sha = EXCLUDED.catalog_sha
	`, agentID, columnSlug, instruction, sha); err != nil {
		return fmt.Errorf("upsert catalog column instruction: %w", err)
	}
	return nil
}

func (s *BoardConfigStore) DeleteAgentColumnInstruction(ctx context.Context, agentID uuid.UUID, columnSlug string) error {
	if _, err := s.pool.Exec(ctx, `
		DELETE FROM agent_column_instructions WHERE agent_id = $1 AND column_slug = $2
	`, agentID, columnSlug); err != nil {
		return fmt.Errorf("delete agent column instruction: %w", err)
	}
	return nil
}

func (s *BoardConfigStore) ListTransitions(ctx context.Context) ([]domain.BoardTransition, error) {
	rows, err := s.pool.Query(ctx, `SELECT from_slug, to_slug FROM board_column_transitions`)
	if err != nil {
		return nil, fmt.Errorf("list transitions: %w", err)
	}
	defer rows.Close()
	var out []domain.BoardTransition
	for rows.Next() {
		var t domain.BoardTransition
		if err := rows.Scan(&t.From, &t.To); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *BoardConfigStore) SetTransitions(ctx context.Context, transitions []domain.BoardTransition) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM board_column_transitions`); err != nil {
		return err
	}
	for _, t := range transitions {
		if t.From == "" || t.To == "" || t.From == t.To {
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO board_column_transitions (from_slug, to_slug) VALUES ($1, $2)
			ON CONFLICT DO NOTHING
		`, t.From, t.To); err != nil {
			return fmt.Errorf("insert transition: %w", err)
		}
	}
	return tx.Commit(ctx)
}

func (s *BoardConfigStore) AgentsForColumn(ctx context.Context, columnSlug string, taskType string) ([]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT agent_id FROM agent_column_subscriptions
		WHERE column_slug = $1
		  AND (task_type_filter IS NULL OR $2 = ANY(task_type_filter))
	`, columnSlug, taskType)
	if err != nil {
		return nil, fmt.Errorf("agents for column: %w", err)
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *BoardConfigStore) ValidateColumnSlug(ctx context.Context, slug string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM board_columns WHERE slug = $1)
	`, slug).Scan(&exists)
	return exists, err
}

type TaskCommentStore struct {
	pool *DB
}

func NewTaskCommentStore(pool *DB) *TaskCommentStore {
	return &TaskCommentStore{pool: pool}
}

// The author name is joined in rather than resolved by the caller: the comments
// endpoint is polled, and naming agents there used to list every agent per poll.
const taskCommentAuthorJoin = `LEFT JOIN agents ag ON c.author_type = 'agent' AND ag.id::text = c.author_id`

func (s *TaskCommentStore) Create(ctx context.Context, comment domain.TaskComment) (domain.TaskComment, error) {
	var created domain.TaskComment
	err := s.pool.QueryRow(ctx, `
		WITH c AS (
			INSERT INTO task_comments (task_id, author_type, author_id, content, actor_user_id)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING id, task_id, author_type, author_id, content, created_at, actor_user_id
		)
		SELECT c.id, c.task_id, c.author_type, c.author_id, c.content, c.created_at, c.actor_user_id, COALESCE(ag.name, '')
		FROM c `+taskCommentAuthorJoin+`
	`, comment.TaskID, comment.AuthorType, comment.AuthorID, comment.Content, comment.ActorUserID).Scan(
		&created.ID, &created.TaskID, &created.AuthorType, &created.AuthorID, &created.Content, &created.CreatedAt, &created.ActorUserID, &created.AuthorName,
	)
	if err != nil {
		return domain.TaskComment{}, fmt.Errorf("create comment: %w", err)
	}
	return created, nil
}

func (s *TaskCommentStore) ListByTask(ctx context.Context, taskID uuid.UUID) ([]domain.TaskComment, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.task_id, c.author_type, c.author_id, c.content, c.created_at, c.actor_user_id, COALESCE(ag.name, '')
		FROM task_comments c `+taskCommentAuthorJoin+`
		WHERE c.task_id = $1 ORDER BY c.created_at ASC
	`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list comments: %w", err)
	}
	defer rows.Close()
	var comments []domain.TaskComment
	for rows.Next() {
		var c domain.TaskComment
		if err := rows.Scan(&c.ID, &c.TaskID, &c.AuthorType, &c.AuthorID, &c.Content, &c.CreatedAt, &c.ActorUserID, &c.AuthorName); err != nil {
			return nil, err
		}
		comments = append(comments, c)
	}
	return comments, rows.Err()
}

type BoardEventStore struct {
	pool *DB
}

func NewBoardEventStore(pool *DB) *BoardEventStore {
	return &BoardEventStore{pool: pool}
}

func (s *BoardEventStore) Create(ctx context.Context, event domain.BoardEvent) (domain.BoardEvent, error) {
	var created domain.BoardEvent
	err := s.pool.QueryRow(ctx, `
		INSERT INTO board_events (repository_id, task_id, event_type, payload, actor_user_id)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, repository_id, task_id, event_type, payload, created_at, actor_user_id
	`, event.RepositoryID, event.TaskID, string(event.EventType), event.Payload, event.ActorUserID).Scan(
		&created.ID, &created.RepositoryID, &created.TaskID, &created.EventType, &created.Payload, &created.CreatedAt, &created.ActorUserID,
	)
	if err != nil {
		return domain.BoardEvent{}, fmt.Errorf("create board event: %w", err)
	}
	return created, nil
}

func (s *BoardEventStore) ListRecent(ctx context.Context, limit int) ([]domain.BoardEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, repository_id, task_id, event_type, payload, created_at, actor_user_id
		FROM board_events ORDER BY created_at DESC LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("list board events: %w", err)
	}
	defer rows.Close()
	var events []domain.BoardEvent
	for rows.Next() {
		var e domain.BoardEvent
		if err := rows.Scan(&e.ID, &e.RepositoryID, &e.TaskID, &e.EventType, &e.Payload, &e.CreatedAt, &e.ActorUserID); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// ListByTask returns one task's board events oldest-first: read top to bottom
// it is the task's history (created → moved → assigned → commented).
func (s *BoardEventStore) ListByTask(ctx context.Context, taskID uuid.UUID, limit int) ([]domain.BoardEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, repository_id, task_id, event_type, payload, created_at, actor_user_id
		FROM board_events WHERE task_id = $1 ORDER BY created_at ASC LIMIT $2
	`, taskID, limit)
	if err != nil {
		return nil, fmt.Errorf("list task board events: %w", err)
	}
	defer rows.Close()
	var events []domain.BoardEvent
	for rows.Next() {
		var e domain.BoardEvent
		if err := rows.Scan(&e.ID, &e.RepositoryID, &e.TaskID, &e.EventType, &e.Payload, &e.CreatedAt, &e.ActorUserID); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// taskAgentRunColumns is the row shape every task_agent_runs read selects and
// every write returns, in the exact order scanTaskAgentRun(s) reads them.
//
// One list rather than the seven verbatim copies it replaces: they only ever
// change together (adding migration 101's cli_session_id/quota_resume_at meant
// editing all seven), and a copy that drifts does not fail to compile — it
// fails at runtime, on one query, with a scan error that names a type rather
// than the query it came from.
const taskAgentRunColumns = `id, task_id, agent_id, board_event_id, session_run_id, status, summary, workspace_path, ` +
	`tool_calls, tool_errors, error_pattern, llm_calls, prompt_tokens, completion_tokens, cache_read_tokens, cache_write_tokens, ` +
	`cli_session_id, quota_resume_at, cli_provider, created_at, updated_at`

type TaskAgentRunStore struct {
	pool *DB
}

func NewTaskAgentRunStore(pool *DB) *TaskAgentRunStore {
	return &TaskAgentRunStore{pool: pool}
}

// Create inserts the run, or — when another caller already queued one for the
// same (task, agent) — returns the run that got there first instead.
//
// The dispatcher's pending check is a read in its own round-trip, so two board
// events for one task+agent (a task.assigned landing right behind a
// task.moved, a webhook arriving alongside a UI action) can both read "nothing
// queued" and both arrive here. Migration 075's partial unique index over
// status = 'pending' is what actually settles that race; ON CONFLICT turns the
// loser's insert into a lookup, so a duplicate dispatch degrades to a no-op
// instead of an error surfacing to whoever moved the card.
//
// DO UPDATE rather than DO NOTHING because only DO UPDATE is guaranteed to
// hand back the conflicting row: DO NOTHING returns nothing, and re-reading
// the row in the same statement would run against a snapshot taken before the
// winner committed, so the loser would see neither its own insert nor the
// winner's. The SET is a deliberate no-op — updated_at is the run's heartbeat
// (ListStale reads it) and a racer must not rewrite the winner's.
func (s *TaskAgentRunStore) Create(ctx context.Context, run domain.TaskAgentRun) (domain.TaskAgentRun, error) {
	var created domain.TaskAgentRun
	err := s.pool.QueryRow(ctx, `
		INSERT INTO task_agent_runs (task_id, agent_id, board_event_id, session_run_id, status, summary, workspace_path)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (task_id, agent_id) WHERE status = 'pending'
		DO UPDATE SET updated_at = task_agent_runs.updated_at
		RETURNING `+taskAgentRunColumns+`
	`, run.TaskID, run.AgentID, run.BoardEventID, run.SessionRunID, run.Status, run.Summary, run.WorkspacePath).Scan(
		&created.ID, &created.TaskID, &created.AgentID, &created.BoardEventID, &created.SessionRunID,
		&created.Status, &created.Summary, &created.WorkspacePath,
		&created.ToolCalls, &created.ToolErrors, &created.ErrorPattern,
		&created.LLMCalls, &created.PromptTokens, &created.CompletionTokens, &created.CacheReadTokens, &created.CacheWriteTokens,
		&created.CLISessionID, &created.QuotaResumeAt, &created.CLIProvider,
		&created.CreatedAt, &created.UpdatedAt,
	)
	if err != nil {
		return domain.TaskAgentRun{}, fmt.Errorf("create task agent run: %w", err)
	}
	return created, nil
}

// Update writes the run back. The status write is guarded: a run a human
// cancelled stays cancelled even though the goroutine unwinding behind the
// cancelled context still tries to stamp its own verdict on the way out. Every
// other column is written normally — whatever work the run did get done before
// it was stopped is still worth recording.
func (s *TaskAgentRunStore) Update(ctx context.Context, run domain.TaskAgentRun) (domain.TaskAgentRun, error) {
	var updated domain.TaskAgentRun
	err := s.pool.QueryRow(ctx, `
		UPDATE task_agent_runs SET
			session_run_id = $2,
			status = CASE WHEN task_agent_runs.status = 'cancelled' THEN 'cancelled' ELSE $3 END,
			summary = $4,
			workspace_path = $5,
			tool_calls = $6,
			tool_errors = $7,
			error_pattern = $8,
			llm_calls = $9,
			prompt_tokens = $10,
			completion_tokens = $11,
			cache_read_tokens = $12,
			cache_write_tokens = $13,
			cli_session_id = $14,
			quota_resume_at = $15,
			cli_provider = $16,
			updated_at = now()
		WHERE id = $1
		RETURNING `+taskAgentRunColumns+`
	`, run.ID, run.SessionRunID, run.Status, run.Summary, run.WorkspacePath, run.ToolCalls, run.ToolErrors, run.ErrorPattern,
		run.LLMCalls, run.PromptTokens, run.CompletionTokens, run.CacheReadTokens, run.CacheWriteTokens,
		run.CLISessionID, run.QuotaResumeAt, run.CLIProvider).Scan(
		&updated.ID, &updated.TaskID, &updated.AgentID, &updated.BoardEventID, &updated.SessionRunID,
		&updated.Status, &updated.Summary, &updated.WorkspacePath,
		&updated.ToolCalls, &updated.ToolErrors, &updated.ErrorPattern,
		&updated.LLMCalls, &updated.PromptTokens, &updated.CompletionTokens, &updated.CacheReadTokens, &updated.CacheWriteTokens,
		&updated.CLISessionID, &updated.QuotaResumeAt, &updated.CLIProvider,
		&updated.CreatedAt, &updated.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.TaskAgentRun{}, fmt.Errorf("update task agent run: %w", domain.ErrTaskAgentRunNotFound)
		}
		return domain.TaskAgentRun{}, fmt.Errorf("update task agent run: %w", err)
	}
	return updated, nil
}

func (s *TaskAgentRunStore) ListByTask(ctx context.Context, taskID uuid.UUID, limit int) ([]domain.TaskAgentRun, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+taskAgentRunColumns+`
		FROM task_agent_runs WHERE task_id = $1 ORDER BY created_at DESC LIMIT $2
	`, taskID, limit)
	if err != nil {
		return nil, fmt.Errorf("list task agent runs: %w", err)
	}
	defer rows.Close()
	return scanTaskAgentRuns(rows)
}

func (s *TaskAgentRunStore) ListRecent(ctx context.Context, limit int) ([]domain.TaskAgentRun, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+taskAgentRunColumns+`
		FROM task_agent_runs ORDER BY created_at DESC LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("list recent agent runs: %w", err)
	}
	defer rows.Close()
	return scanTaskAgentRuns(rows)
}

func (s *TaskAgentRunStore) HasPendingForEvent(ctx context.Context, boardEventID, agentID uuid.UUID) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM task_agent_runs
			WHERE board_event_id = $1 AND agent_id = $2 AND status IN ('pending', 'running')
		)
	`, boardEventID, agentID).Scan(&exists)
	return exists, err
}

func (s *TaskAgentRunStore) HasPendingForTask(ctx context.Context, taskID, agentID uuid.UUID) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM task_agent_runs
			WHERE task_id = $1 AND agent_id = $2 AND status = 'pending'
		)
	`, taskID, agentID).Scan(&exists)
	return exists, err
}

func (s *TaskAgentRunStore) HasLiveForTask(ctx context.Context, taskID, agentID uuid.UUID) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM task_agent_runs
			WHERE task_id = $1 AND agent_id = $2 AND status IN ('pending', 'running')
		)
	`, taskID, agentID).Scan(&exists)
	return exists, err
}

func (s *TaskAgentRunStore) ListStale(ctx context.Context, cutoff time.Time) ([]domain.TaskAgentRun, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+taskAgentRunColumns+`
		FROM task_agent_runs
		WHERE status IN ('pending', 'running') AND updated_at < $1
		ORDER BY updated_at ASC
	`, cutoff)
	if err != nil {
		return nil, fmt.Errorf("list stale task agent runs: %w", err)
	}
	defer rows.Close()
	return scanTaskAgentRuns(rows)
}

func (s *TaskAgentRunStore) GetByID(ctx context.Context, id uuid.UUID) (domain.TaskAgentRun, error) {
	var run domain.TaskAgentRun
	err := s.pool.QueryRow(ctx, `
		SELECT `+taskAgentRunColumns+`
		FROM task_agent_runs WHERE id = $1
	`, id).Scan(
		&run.ID, &run.TaskID, &run.AgentID, &run.BoardEventID, &run.SessionRunID,
		&run.Status, &run.Summary, &run.WorkspacePath,
		&run.ToolCalls, &run.ToolErrors, &run.ErrorPattern,
		&run.LLMCalls, &run.PromptTokens, &run.CompletionTokens, &run.CacheReadTokens, &run.CacheWriteTokens,
		&run.CLISessionID, &run.QuotaResumeAt, &run.CLIProvider,
		&run.CreatedAt, &run.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.TaskAgentRun{}, fmt.Errorf("get task agent run: %w", domain.ErrTaskAgentRunNotFound)
		}
		return domain.TaskAgentRun{}, fmt.Errorf("get task agent run: %w", err)
	}
	return run, nil
}

// CancelIfLive is the one atomic write that decides a cancellation. The status
// filter in the WHERE clause is what makes a double-click harmless and what
// stops a stop-request from resurrecting a run that finished a millisecond
// earlier.
func (s *TaskAgentRunStore) CancelIfLive(ctx context.Context, id uuid.UUID, reason string) (domain.TaskAgentRun, bool, error) {
	var cancelled domain.TaskAgentRun
	err := s.pool.QueryRow(ctx, `
		UPDATE task_agent_runs SET
			status = 'cancelled',
			summary = CASE WHEN $2 = '' THEN summary ELSE $2 END,
			updated_at = now()
		WHERE id = $1 AND status IN ('pending', 'running')
		RETURNING `+taskAgentRunColumns+`
	`, id, reason).Scan(
		&cancelled.ID, &cancelled.TaskID, &cancelled.AgentID, &cancelled.BoardEventID, &cancelled.SessionRunID,
		&cancelled.Status, &cancelled.Summary, &cancelled.WorkspacePath,
		&cancelled.ToolCalls, &cancelled.ToolErrors, &cancelled.ErrorPattern,
		&cancelled.LLMCalls, &cancelled.PromptTokens, &cancelled.CompletionTokens, &cancelled.CacheReadTokens, &cancelled.CacheWriteTokens,
		&cancelled.CLISessionID, &cancelled.QuotaResumeAt, &cancelled.CLIProvider,
		&cancelled.CreatedAt, &cancelled.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.TaskAgentRun{}, false, nil
		}
		return domain.TaskAgentRun{}, false, fmt.Errorf("cancel task agent run: %w", err)
	}
	return cancelled, true, nil
}

// Touch bumps the heartbeat and reads the status back in the same statement.
//
// The read is the point as much as the write. A stop pressed by a human is
// served by whichever replica the load balancer picked, and all that replica
// can reliably do is write 'cancelled' here — the run's context and its cancel
// func live in another process's memory. So the run asks, on the write it was
// making anyway, and stops itself when the row says to. That is why the WHERE
// clause does not filter on status: a cancelled row must still be READABLE
// here, or the run would learn nothing and keep going. The CASE is what keeps
// the bump itself limited to a live run.
func (s *TaskAgentRunStore) Touch(ctx context.Context, id uuid.UUID) (string, error) {
	var status string
	err := s.pool.QueryRow(ctx, `
		UPDATE task_agent_runs SET
			updated_at = CASE WHEN status IN ('pending', 'running') THEN now() ELSE updated_at END
		WHERE id = $1
		RETURNING status
	`, id).Scan(&status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("touch task agent run: %w", domain.ErrTaskAgentRunNotFound)
		}
		return "", fmt.Errorf("touch task agent run: %w", err)
	}
	return status, nil
}

// claimRunSQL is the whole of "may this process execute this run".
//
// Shape borrowed wholesale from TakeBlockedResourceTask, which is how the
// device, quota, work-order and deploy sweepers already keep two pods off one
// item: pick the winner under FOR UPDATE SKIP LOCKED, write the state change in
// the same statement, and let the loser see zero rows.
//
//   - me       the run, but only while it is still pending. SKIP LOCKED rather
//     than a wait: if another replica is inside this statement for the
//     same run, the honest answer is "somebody else has it", not a
//     queue.
//   - live     the genuinely-executing runs. The heartbeat window is what
//     excludes runs whose process was killed.
//   - gate     the two budgets, evaluated together so the caller learns WHICH
//     one refused without a second query.
//   - claimed  the transition. 'pending' -> 'running' is the claim; there is no
//     owner column and no lease to expire, because the heartbeat on
//     the row is already the liveness signal and a claim nobody
//     heartbeats is exactly what the reconciler exists to collect.
const claimRunSQL = `
WITH me AS (
    SELECT r.id, r.task_id
    FROM task_agent_runs r
    WHERE r.id = $1 AND r.status = 'pending'
    FOR UPDATE SKIP LOCKED
), live AS (
    SELECT r.id, r.task_id
    FROM task_agent_runs r
    WHERE r.status = 'running' AND r.updated_at > now() - $2::interval
), gate AS (
    SELECT me.id,
        EXISTS (SELECT 1 FROM live WHERE live.task_id = me.task_id) AS task_busy
    FROM me
), claimed AS (
    UPDATE task_agent_runs r
    SET status = 'running', updated_at = now()
    FROM gate
    WHERE r.id = gate.id
      AND NOT gate.task_busy
    RETURNING r.id
)
SELECT
    EXISTS (SELECT 1 FROM claimed) AS claimed,
    EXISTS (SELECT 1 FROM me) AS pending,
    COALESCE((SELECT task_busy FROM gate), false)
`

// claimLockClass namespaces this store's advisory locks so they cannot collide
// with the migration runner's, which uses the single-argument form.
const claimLockClass int32 = 521202603

// claimLockSQL serialises claims for the length of the claiming transaction
// only.
//
// FOR UPDATE SKIP LOCKED locks the ONE row being claimed, not the task it
// belongs to. Under READ COMMITTED two runs of the same task claimed at once
// would each ask "is another run on this task live?" against a snapshot from
// before the other committed, and both would start. The advisory lock makes
// that check and the write serial, and it releases at commit with no cleanup
// path to get wrong.
//
// The two-argument form keeps it apart from the migration runner's
// single-argument lock.
const claimLockSQL = `SELECT pg_advisory_xact_lock($1, 0)`

func (s *TaskAgentRunStore) ClaimRun(ctx context.Context, claim port.RunClaim) (port.RunClaimResult, error) {
	live := claim.LiveWithin
	if live <= 0 {
		live = time.Minute
	}
	var claimed, pending, taskBusy bool
	err := s.pool.InTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, claimLockSQL, claimLockClass); err != nil {
			return fmt.Errorf("lock for claim: %w", err)
		}
		return tx.QueryRow(ctx, claimRunSQL,
			claim.RunID,
			live.String(),
		).Scan(&claimed, &pending, &taskBusy)
	})
	if err != nil {
		return port.RunClaimResult{}, fmt.Errorf("claim task agent run: %w", err)
	}
	switch {
	case claimed:
		return port.RunClaimResult{Claimed: true}, nil
	case !pending:
		return port.RunClaimResult{Reason: "not_pending"}, nil
	case taskBusy:
		return port.RunClaimResult{Reason: "task_busy"}, nil
	default:
		// The gate passed and the UPDATE still wrote nothing. Not reachable
		// through the statement above, but a reason the caller can log beats a
		// claim that silently reads as taken.
		return port.RunClaimResult{Reason: "not_pending"}, nil
	}
}

// FailIfStale re-checks the heartbeat inside the write.
//
// The reconciler lists stale runs and then acts on each one, and the owner of
// a listed run may heartbeat during that gap — walking the full list takes
// real time, so the gap is seconds, not microseconds. Re-asserting the
// cutoff in the WHERE clause is what makes the list advisory and the write
// authoritative. 'cancelled' is excluded for the same reason Update
// preserves it: a human's verdict outranks a sweeper's.
func (s *TaskAgentRunStore) FailIfStale(ctx context.Context, id uuid.UUID, cutoff time.Time, summary string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE task_agent_runs SET
			status = 'failed',
			summary = CASE WHEN $3 = '' THEN summary ELSE $3 END,
			updated_at = now()
		WHERE id = $1 AND status IN ('pending', 'running') AND updated_at < $2
	`, id, cutoff, summary)
	if err != nil {
		return false, fmt.Errorf("fail stale task agent run: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// HasLiveRunForTask is the cross-process replacement for Runner.IsTaskActive.
// The heartbeat window, not the status alone: a 'running' row whose pod was
// killed must not protect a workspace forever.
func (s *TaskAgentRunStore) HasLiveRunForTask(ctx context.Context, taskID uuid.UUID, liveWithin time.Duration) (bool, error) {
	if liveWithin <= 0 {
		liveWithin = time.Minute
	}
	var exists bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM task_agent_runs
			WHERE task_id = $1 AND status = 'running' AND updated_at > now() - $2::interval
		)
	`, taskID, liveWithin.String()).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("live run for task: %w", err)
	}
	return exists, nil
}

func scanTaskAgentRuns(rows pgx.Rows) ([]domain.TaskAgentRun, error) {
	var runs []domain.TaskAgentRun
	for rows.Next() {
		var r domain.TaskAgentRun
		if err := rows.Scan(
			&r.ID, &r.TaskID, &r.AgentID, &r.BoardEventID, &r.SessionRunID,
			&r.Status, &r.Summary, &r.WorkspacePath,
			&r.ToolCalls, &r.ToolErrors, &r.ErrorPattern,
			&r.LLMCalls, &r.PromptTokens, &r.CompletionTokens, &r.CacheReadTokens, &r.CacheWriteTokens,
			&r.CLISessionID, &r.QuotaResumeAt, &r.CLIProvider,
			&r.CreatedAt, &r.UpdatedAt,
		); err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}
