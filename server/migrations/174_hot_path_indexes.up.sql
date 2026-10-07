-- Indexes for reads the UI polls every few seconds.
--
-- task_agent_runs: the activity feed reads the newest runs across every task
-- (ORDER BY created_at DESC LIMIT n); every existing index leads with another
-- column, so that was a full sort of the table on each poll.
--
-- session_steps / session_messages: run activity and chat history read one
-- run's or session's rows in time order. The composite indexes serve both the
-- filter and the ORDER BY, and make the single-column indexes they replace
-- redundant (a leading-column prefix of the new one).
--
-- Not CONCURRENTLY: migrations run inside a transaction.

CREATE INDEX IF NOT EXISTS idx_task_agent_runs_created ON task_agent_runs (created_at DESC);

CREATE INDEX IF NOT EXISTS idx_session_steps_run_created ON session_steps (run_id, created_at, id);
DROP INDEX IF EXISTS idx_session_steps_run_id;

CREATE INDEX IF NOT EXISTS idx_session_messages_session_created ON session_messages (session_id, created_at);
DROP INDEX IF EXISTS idx_session_messages_session_id;
