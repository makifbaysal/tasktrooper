-- A board run's real trace: every tool call, written when the call finished
-- (never summarised by a model), plus the commit the run started from and what
-- git said the run changed once it was over. Append-only; it goes with the run
-- (and so with the task) and is swept after 90 days.
CREATE TABLE IF NOT EXISTS run_traces (
    run_id         UUID PRIMARY KEY,
    task_id        UUID NOT NULL,
    agent_id       UUID NOT NULL,
    base_sha       TEXT NOT NULL DEFAULT '',
    head_sha       TEXT NOT NULL DEFAULT '',
    branch         TEXT NOT NULL DEFAULT '',
    workspace_path TEXT NOT NULL DEFAULT '',
    column_before  TEXT NOT NULL DEFAULT '',
    remote         BOOLEAN NOT NULL DEFAULT false,
    dropped        INT NOT NULL DEFAULT 0,
    verification   JSONB,
    started_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at    TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_run_traces_task ON run_traces (task_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_run_traces_started ON run_traces (started_at);

CREATE TABLE IF NOT EXISTS run_trace_events (
    run_id      UUID NOT NULL REFERENCES run_traces(run_id) ON DELETE CASCADE,
    seq         INT NOT NULL,
    task_id     UUID NOT NULL,
    agent_id    UUID NOT NULL,
    at          TIMESTAMPTZ NOT NULL,
    duration_ms BIGINT NOT NULL DEFAULT 0,
    tool        TEXT NOT NULL,
    native_tool TEXT NOT NULL DEFAULT '',
    source      TEXT NOT NULL DEFAULT 'loop',
    category    TEXT NOT NULL,
    target      TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL,
    error       TEXT NOT NULL DEFAULT '',
    detail      JSONB NOT NULL DEFAULT '{}'::jsonb,
    PRIMARY KEY (run_id, seq)
);

CREATE INDEX IF NOT EXISTS idx_run_trace_events_category ON run_trace_events (run_id, category, seq);

-- Company edition: this migration applies in either order relative to c005
-- (.ai/shared-tenancy.md, "Upstream migrations after c005"). On a fresh
-- database it runs before c005, which scopes everything; on one that already
-- has c005 the referenced tables are keyed by (tenant_id, id), so the keys name
-- tenant_id. The key between the two new tables keeps its inline form:
-- tenancy_apply() re-keys it with the tables.
DO $$
BEGIN
    IF to_regprocedure('tenancy_apply()') IS NULL THEN
        ALTER TABLE run_traces ADD CONSTRAINT run_traces_run_id_fkey
            FOREIGN KEY (run_id) REFERENCES task_agent_runs(id) ON DELETE CASCADE;
        ALTER TABLE run_traces ADD CONSTRAINT run_traces_task_id_fkey
            FOREIGN KEY (task_id) REFERENCES board_tasks(id) ON DELETE CASCADE;
    ELSE
        PERFORM tenancy_apply();
        ALTER TABLE run_traces ADD CONSTRAINT run_traces_run_id_fkey
            FOREIGN KEY (tenant_id, run_id) REFERENCES task_agent_runs(tenant_id, id) ON DELETE CASCADE;
        ALTER TABLE run_traces ADD CONSTRAINT run_traces_task_id_fkey
            FOREIGN KEY (tenant_id, task_id) REFERENCES board_tasks(tenant_id, id) ON DELETE CASCADE;
    END IF;
END $$;
