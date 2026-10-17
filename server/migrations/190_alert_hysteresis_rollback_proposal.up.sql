-- a. Alert rule hysteresis. A firing rule resolves only once its condition
--    has read false for at least two evaluations in a row and for its own
--    for_seconds (clear_count, clear_since), and moves to no_data only after
--    a few evaluations in a row read nothing (no_data_count). All three count
--    only while the rule is firing and are zero otherwise.
ALTER TABLE alert_rules
    ADD COLUMN IF NOT EXISTS clear_count   INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS clear_since   TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS no_data_count INT NOT NULL DEFAULT 0;

-- b. A rollback the release engineer proposed while the release's profile
--    had auto_rollback off: nothing ran, a human approves or dismisses it.
--    Written only by its own statement, never by the general release update,
--    so a stale copy of the release cannot clear or resurrect it; the update
--    clears it when the release rolls back or is superseded.
ALTER TABLE releases
    ADD COLUMN IF NOT EXISTS rollback_proposal JSONB;
