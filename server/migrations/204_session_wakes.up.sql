-- A chat agent that started background work (an audience build, a study) and
-- ended its turn with "I'll tell you when it is done" has nothing to bring it
-- back. A wake is that promise made durable: the SessionWakeSweeper polls the
-- watched thing and, once it ended (or expires_at passed), runs a new turn in
-- the session with the outcome. Deleting the row is both the user's "stop
-- waiting" and the sweeper's claim.
CREATE TABLE IF NOT EXISTS session_wakes (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID        NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    kind       TEXT        NOT NULL,
    ref_id     UUID        NOT NULL,
    label      TEXT        NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    UNIQUE (session_id, kind, ref_id)
);
