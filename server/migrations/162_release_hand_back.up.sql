-- A hand-back the dispatcher dropped (the agent's run was still alive) is
-- re-sent by the sweeper, but only while no agent has looked at the release
-- since — so the bookkeeping lives on the row, not in process memory that a
-- desktop relaunch would wipe.
ALTER TABLE releases
    ADD COLUMN IF NOT EXISTS hand_back_count   INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS last_hand_back_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS agent_seen_at     TIMESTAMPTZ;
