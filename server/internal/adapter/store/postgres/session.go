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
	"github.com/rs/zerolog/log"
)

type SessionStore struct {
	pool *DB

	// hosts describes THIS host's filesystem, so the absolute paths in a
	// session row can be re-anchored on the way out of the store. See
	// localizeSessionPaths.
	hosts hostRoots
}

func NewSessionStore(pool *DB) *SessionStore {
	return &SessionStore{pool: pool}
}

// SetHostRoots tells the store which filesystem it is reading rows on behalf
// of, exactly as RepositoryStore.SetHostRoots does. A setter rather than a
// constructor argument because every caller that does not know (tests, anything
// that never touches a working copy) is correctly served by the zero value:
// with no workspace root nothing is re-anchored and stored paths come back
// verbatim, exactly as before.
func (s *SessionStore) SetHostRoots(workspaceRoot string, allowedRoots []string) *SessionStore {
	s.hosts.set(workspaceRoot, allowedRoots)
	return s
}

// localizeSessionPaths translates a session row's stored paths into ones this
// host can actually use, and is applied to every sessions row leaving this
// store.
//
// workspace_dir is the directory the turn runs in — the repository root, the
// task's checkout (<workspace_root>/task-<id>), or the chat's own scratch dir —
// and project_root is the subtree the indexer and the code tools are scoped to.
// Both are absolute paths belonging to whichever host wrote the row, and one
// database is now served by two of them. Resuming a pod-written chat on
// the Mac reached `os.MkdirAll("/data/workspaces/...")` in
// session.Service.ensureSessionWorkspace and failed the turn with
// `mkdir /data: read-only file system`, the same way the board run did before
// repositories.root_path was translated.
//
// Translating here rather than at each consumer is deliberate: the chat turn,
// the task-bound chat, the index endpoints, the code tools and the delete path
// all reach a directory through a row read from this store.
//
// Only a genuinely foreign path is rewritten (see workspace.HostRootPath), so a
// workspace that exists here — or that this host may still create under its own
// roots — is left alone and no live local workspace is ever traded for an empty
// new one. The stored columns are untouched, which is what keeps the
// translation symmetric: the pod re-anchors a Mac-written path the same way.
func (s *SessionStore) localizeSessionPaths(sess *domain.Session) {
	if s == nil || sess == nil {
		return
	}
	sess.WorkspaceDir = s.localizeSessionPath(sess.ID, "workspace_dir", sess.WorkspaceDir)
	sess.ProjectRoot = s.localizeSessionPath(sess.ID, "project_root", sess.ProjectRoot)
}

func (s *SessionStore) localizeSessionPath(id uuid.UUID, column, stored string) string {
	resolved, reanchored, first := s.hosts.localize(stored)
	if !reanchored {
		return stored
	}
	if first {
		log.Info().
			Str("session", id.String()).
			Str("column", column).
			Str("stored_path", stored).
			Str("host_path", resolved).
			Msg("session path was written by another host; re-anchored to this host's workspace root")
	}
	return resolved
}

// scanSession reads one sessions row in sessionColumns order and re-anchors its
// paths onto this host. Every SELECT/RETURNING in this file goes through it, so
// no read path can hand a consumer another host's directory.
func (s *SessionStore) scanSession(row interface{ Scan(dest ...any) error }) (domain.Session, error) {
	var sess domain.Session
	if err := row.Scan(sessionScanTargets(&sess)...); err != nil {
		return domain.Session{}, err
	}
	s.localizeSessionPaths(&sess)
	return sess, nil
}

// sessionColumns is the read projection every session query uses, and
// sessionScanTargets is the matching scan list. They are a pair on purpose: the
// column list was previously spelled out at five call sites, so adding a column
// meant editing five queries and two scan lists and failing at runtime with a
// scan-arity error if any one of them drifted. This store has no unit tests to
// catch that.
const sessionColumns = `id, title, model, workspace_dir, project_root, repository_id, agent_id, task_id, cli_session_id, created_at, updated_at, expires_at`

func sessionScanTargets(sess *domain.Session) []any {
	return []any{
		&sess.ID, &sess.Title, &sess.Model, &sess.WorkspaceDir, &sess.ProjectRoot,
		&sess.ProjectID, &sess.AgentID, &sess.TaskID, &sess.CLISessionID,
		&sess.CreatedAt, &sess.UpdatedAt, &sess.ExpiresAt,
	}
}

func (s *SessionStore) Create(ctx context.Context, title, model, workspaceDir string, projectID, agentID *uuid.UUID, expiresAt *time.Time) (domain.Session, error) {
	sess, err := s.scanSession(s.pool.QueryRow(ctx, `
		INSERT INTO sessions (title, model, workspace_dir, repository_id, agent_id, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING `+sessionColumns+`
	`, title, model, workspaceDir, projectID, agentID, expiresAt))
	if err != nil {
		return domain.Session{}, fmt.Errorf("create session: %w", err)
	}
	return sess, nil
}

// BindTask makes an existing session the chat about a board task.
//
// A task's chat is opened after the session exists (the opener may also adopt the
// task's clarification thread, which was created by an agent long before), so
// this is an UPDATE rather than an INSERT column.
func (s *SessionStore) BindTask(ctx context.Context, id, taskID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `UPDATE sessions SET task_id = $2, updated_at = now() WHERE id = $1`, id, taskID)
	if err != nil {
		return fmt.Errorf("bind session task: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrSessionNotFound
	}
	return nil
}

// FindByTask returns the chat bound to taskID.
//
// Newest first, so a task that somehow ended up with two bound chats (a bind that
// raced another, a restored backup) reopens the one that was last spoken in
// rather than an abandoned one.
func (s *SessionStore) FindByTask(ctx context.Context, taskID uuid.UUID) (domain.Session, bool, error) {
	sess, err := s.scanSession(s.pool.QueryRow(ctx, `
		SELECT `+sessionColumns+`
		FROM sessions WHERE task_id = $1
		ORDER BY updated_at DESC LIMIT 1
	`, taskID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Session{}, false, nil
	}
	if err != nil {
		return domain.Session{}, false, fmt.Errorf("find session by task: %w", err)
	}
	return sess, true, nil
}

func (s *SessionStore) UpdateWorkspaceDir(ctx context.Context, id uuid.UUID, workspaceDir string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE sessions SET workspace_dir = $2, updated_at = now() WHERE id = $1`, id, workspaceDir)
	if err != nil {
		return fmt.Errorf("update workspace dir: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("session not found")
	}
	return nil
}

func (s *SessionStore) UpdateProjectRoot(ctx context.Context, id uuid.UUID, projectRoot string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE sessions SET project_root = $2, updated_at = now() WHERE id = $1`, id, projectRoot)
	if err != nil {
		return fmt.Errorf("update project root: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("session not found")
	}
	return nil
}

// UpdateCLISessionID records which Claude Code conversation this chat is being
// held in.
//
// updated_at is deliberately NOT touched, unlike every other update on this
// table. This write happens on every turn of a claude_code chat and says
// nothing about the conversation a human would recognise — bumping the
// timestamp would reorder the session list on a bookkeeping detail, and
// FindByTask picks its row by `ORDER BY updated_at DESC`.
//
// A missing row is not an error here: the chat may have been deleted while its
// turn was still finishing, and there is nothing left to record it against.
func (s *SessionStore) UpdateCLISessionID(ctx context.Context, id uuid.UUID, cliSessionID string) error {
	if _, err := s.pool.Exec(ctx, `UPDATE sessions SET cli_session_id = $2 WHERE id = $1`, id, cliSessionID); err != nil {
		return fmt.Errorf("update cli session id: %w", err)
	}
	return nil
}

func (s *SessionStore) ParkPendingTurn(ctx context.Context, sessionID uuid.UUID, req domain.SessionMessageRequest, policy domain.ToolPolicy, resumeAt time.Time) error {
	reqJSON, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal pending session turn request: %w", err)
	}
	policyJSON, err := json.Marshal(policy)
	if err != nil {
		return fmt.Errorf("marshal pending session turn policy: %w", err)
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE sessions
		SET quota_resume_at = $2, quota_pending_request = $3, quota_pending_policy = $4
		WHERE id = $1
	`, sessionID, resumeAt, reqJSON, policyJSON)
	if err != nil {
		return fmt.Errorf("park pending session turn: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrSessionNotFound
	}
	return nil
}

// TakePendingSessionTurn is ParkPendingTurn's other half: the sweeper's claim.
// FOR UPDATE SKIP LOCKED is what keeps two sweeper ticks (or two pods) from
// resuming the same chat twice, the same as TakeQuotaResumable does for board
// tasks.
func (s *SessionStore) TakePendingSessionTurn(ctx context.Context, now time.Time) (domain.PendingSessionTurn, bool, error) {
	var (
		sessionID  uuid.UUID
		reqJSON    []byte
		policyJSON []byte
		resumeAt   time.Time
	)
	err := s.pool.QueryRow(ctx, `
		WITH claimed AS (
			SELECT id FROM sessions
			WHERE quota_resume_at IS NOT NULL AND quota_resume_at <= $1
			ORDER BY quota_resume_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		), cleared AS (
			UPDATE sessions
			SET quota_resume_at = NULL, quota_pending_request = NULL, quota_pending_policy = NULL
			FROM claimed WHERE sessions.id = claimed.id
			RETURNING sessions.id
		)
		SELECT id, quota_pending_request, quota_pending_policy, quota_resume_at
		FROM sessions WHERE id = (SELECT id FROM claimed)
	`, now).Scan(&sessionID, &reqJSON, &policyJSON, &resumeAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PendingSessionTurn{}, false, nil
	}
	if err != nil {
		return domain.PendingSessionTurn{}, false, fmt.Errorf("take pending session turn: %w", err)
	}
	pending := domain.PendingSessionTurn{SessionID: sessionID, ResumeAt: resumeAt}
	if err := json.Unmarshal(reqJSON, &pending.Request); err != nil {
		return domain.PendingSessionTurn{}, false, fmt.Errorf("unmarshal pending session turn request: %w", err)
	}
	if err := json.Unmarshal(policyJSON, &pending.Policy); err != nil {
		return domain.PendingSessionTurn{}, false, fmt.Errorf("unmarshal pending session turn policy: %w", err)
	}
	return pending, true, nil
}

func (s *SessionStore) Get(ctx context.Context, id uuid.UUID) (domain.Session, error) {
	sess, err := s.scanSession(s.pool.QueryRow(ctx, `
		SELECT `+sessionColumns+`
		FROM sessions WHERE id = $1
	`, id))
	if err != nil {
		return domain.Session{}, fmt.Errorf("get session: %w", err)
	}
	return sess, nil
}

func (s *SessionStore) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("session not found")
	}
	return nil
}

func (s *SessionStore) List(ctx context.Context, limit, offset int) ([]domain.Session, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+sessionColumns+`
		FROM sessions
		WHERE repository_id IS NULL AND agent_id IS NULL
		ORDER BY updated_at DESC LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()
	return s.scanSessions(ctx, rows)
}

func (s *SessionStore) ListByProject(ctx context.Context, projectID uuid.UUID, limit, offset int) ([]domain.Session, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+sessionColumns+`
		FROM sessions WHERE repository_id = $1 AND agent_id IS NULL
		ORDER BY updated_at DESC LIMIT $2 OFFSET $3
	`, projectID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list project sessions: %w", err)
	}
	defer rows.Close()
	return s.scanSessions(ctx, rows)
}

func (s *SessionStore) ListByAgent(ctx context.Context, agentID uuid.UUID, limit, offset int) ([]domain.Session, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+sessionColumns+`
		FROM sessions WHERE agent_id = $1
		ORDER BY updated_at DESC LIMIT $2 OFFSET $3
	`, agentID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list agent sessions: %w", err)
	}
	defer rows.Close()
	return s.scanSessions(ctx, rows)
}

func (s *SessionStore) scanSessions(ctx context.Context, rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]domain.Session, error) {
	var sessions []domain.Session
	for rows.Next() {
		sess, err := s.scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, sess)
	}
	if sessions == nil {
		sessions = []domain.Session{}
	}
	return sessions, rows.Err()
}

func (s *SessionStore) ListMessages(ctx context.Context, sessionID uuid.UUID) ([]domain.SessionMessage, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, session_id, role, content, tool_calls, clarification, created_at
		FROM session_messages WHERE session_id = $1 ORDER BY created_at ASC
	`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()

	var messages []domain.SessionMessage
	for rows.Next() {
		var m domain.SessionMessage
		var clarification []byte
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &m.ToolCalls, &clarification, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		if len(clarification) > 0 {
			var req domain.ClarificationRequest
			if err := json.Unmarshal(clarification, &req); err == nil && req.Valid() {
				m.Clarification = &req
			}
		}
		messages = append(messages, m)
	}
	return messages, rows.Err()
}

func (s *SessionStore) AppendMessage(ctx context.Context, sessionID uuid.UUID, role domain.Role, content string, toolCalls []byte, clarification []byte) (domain.SessionMessage, error) {
	var m domain.SessionMessage
	var clarificationOut []byte
	err := s.pool.QueryRow(ctx, `
		INSERT INTO session_messages (session_id, role, content, tool_calls, clarification)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, session_id, role, content, tool_calls, clarification, created_at
	`, sessionID, string(role), content, toolCalls, clarification).Scan(
		&m.ID, &m.SessionID, &m.Role, &m.Content, &m.ToolCalls, &clarificationOut, &m.CreatedAt,
	)
	if err != nil {
		return domain.SessionMessage{}, fmt.Errorf("append message: %w", err)
	}
	if len(clarificationOut) > 0 {
		var req domain.ClarificationRequest
		if err := json.Unmarshal(clarificationOut, &req); err == nil && req.Valid() {
			m.Clarification = &req
		}
	}
	_, _ = s.pool.Exec(ctx, `UPDATE sessions SET updated_at = now() WHERE id = $1`, sessionID)
	return m, nil
}
