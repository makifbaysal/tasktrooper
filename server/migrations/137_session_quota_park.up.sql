-- A chat turn on the claude_code provider can be stopped by the subscription's
-- usage limit the same way a board task can (migration 101) — but a chat has
-- no board_tasks row to park on. So the park lives on the session itself:
--
--   quota_resume_at        when the limit is expected to lift. NULL on every
--                          session that is not currently parked.
--   quota_pending_request  the SessionMessageRequest that hit the limit,
--                          verbatim, so the sweeper can rerun the exact turn
--                          instead of guessing what the user asked for.
--   quota_pending_policy   the resolved ToolPolicy that request ran with.
--
-- The user's message itself is already in the messages table by the time a
-- park happens (session.Service.SendMessage appends it before running the
-- turn), so the sweeper does not re-append anything — it rebuilds history
-- from the store, the same way any other turn does, and only needs these two
-- JSON blobs to know what to run and with what policy.
ALTER TABLE sessions
    ADD COLUMN IF NOT EXISTS quota_resume_at       TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS quota_pending_request  JSONB,
    ADD COLUMN IF NOT EXISTS quota_pending_policy   JSONB;

-- Partial for the same reason as idx_task_agent_runs_quota_resume: a parked
-- chat is a tiny minority of sessions, and the sweeper's only question is
-- "is any of them due".
CREATE INDEX IF NOT EXISTS idx_sessions_quota_resume
    ON sessions (quota_resume_at)
    WHERE quota_resume_at IS NOT NULL;
