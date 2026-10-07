-- cli_provider names the agent CLI (claude_code, cursor, …) that owns a run's
-- cli_session_id. A session id is only meaningful to the CLI that issued it,
-- and an agent can be moved to another provider between two runs of a task, so
-- a revision or a quota-park resume checks this before replaying `--resume`.
--
-- '' on every row written before it was recorded: those sessions are still
-- resumed after a quota park (that is all migration 101 ever stored), but never
-- continued by a revision run.
ALTER TABLE task_agent_runs
    ADD COLUMN IF NOT EXISTS cli_provider TEXT NOT NULL DEFAULT '';
