-- Code-health findings (anti-patterns, missing or misconfigured logging,
-- secrets, debug settings that ship): one row per (component, rule) a scan
-- reported. Kept across scans so a person's verdict — accepted, or a false
-- positive — survives every rescan, and a task opened for a finding stays
-- linked until the scan stops finding the problem. component_id is NULL for
-- a repository-wide finding no component owns (a root .env in a monorepo).
CREATE TABLE IF NOT EXISTS component_findings (
    id            UUID        PRIMARY KEY,
    repository_id UUID        NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    component_id  UUID        REFERENCES project_components(id) ON DELETE CASCADE,
    rule          TEXT        NOT NULL,
    category      TEXT        NOT NULL,
    severity      TEXT        NOT NULL CHECK (severity IN ('high', 'medium', 'low')),
    occurrences   INTEGER     NOT NULL DEFAULT 0,
    evidence      JSONB       NOT NULL DEFAULT '[]',
    status        TEXT        NOT NULL DEFAULT 'open'
        CHECK (status IN ('open', 'accepted', 'false_positive', 'resolved')),
    status_reason TEXT        NOT NULL DEFAULT '',
    task_id       UUID        REFERENCES board_tasks(id) ON DELETE SET NULL,
    last_scan_id  UUID,
    resolved_at   TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_component_findings_repository ON component_findings(repository_id);
CREATE INDEX IF NOT EXISTS idx_component_findings_task ON component_findings(task_id) WHERE task_id IS NOT NULL;
