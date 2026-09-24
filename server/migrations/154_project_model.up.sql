-- The structured project model: components, their CI checks, the edges
-- between them and system resources, human-written notes, and the scans that
-- produce all of it. Replaces the old repository-profile/dependency/pipeline-
-- slot subsystem (migration 133 already dropped tenancy, so nothing here is
-- tenant-scoped).

CREATE TABLE IF NOT EXISTS project_components (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id  UUID        NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    path           TEXT        NOT NULL,
    name           JSONB       NOT NULL DEFAULT '{}',
    role           JSONB       NOT NULL DEFAULT '{}',
    stack          JSONB       NOT NULL DEFAULT '{}',
    commands       JSONB       NOT NULL DEFAULT '[]',
    mobile         JSONB,
    docs           JSONB       NOT NULL DEFAULT '{}',
    gates          JSONB       NOT NULL DEFAULT '{}',
    status         TEXT        NOT NULL DEFAULT 'active',
    manually_added BOOLEAN     NOT NULL DEFAULT false,
    last_scan_id   UUID,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (repository_id, path)
);

CREATE INDEX IF NOT EXISTS idx_project_components_repository_id ON project_components(repository_id);

CREATE TABLE IF NOT EXISTS component_checks (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id  UUID        NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    component_id   UUID        NOT NULL REFERENCES project_components(id) ON DELETE CASCADE,
    source         TEXT        NOT NULL DEFAULT 'ci',
    workflow       TEXT        NOT NULL DEFAULT '',
    workflow_name  TEXT        NOT NULL DEFAULT '',
    job_key        TEXT        NOT NULL DEFAULT '',
    job_name       TEXT        NOT NULL DEFAULT '',
    purpose        JSONB       NOT NULL DEFAULT '{}',
    environment    TEXT        NOT NULL DEFAULT '',
    triggers       JSONB       NOT NULL DEFAULT '[]',
    path_filters   JSONB       NOT NULL DEFAULT '[]',
    steps          JSONB       NOT NULL DEFAULT '[]',
    local_commands JSONB       NOT NULL DEFAULT '{}',
    gate           JSONB       NOT NULL DEFAULT '{}',
    dispatchable   BOOLEAN     NOT NULL DEFAULT false,
    status         TEXT        NOT NULL DEFAULT 'active',
    missing        BOOLEAN     NOT NULL DEFAULT false,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (component_id, workflow, job_key)
);

CREATE INDEX IF NOT EXISTS idx_component_checks_repository_id ON component_checks(repository_id);

CREATE TABLE IF NOT EXISTS system_resources (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    kind         TEXT        NOT NULL,
    vendor       TEXT        NOT NULL DEFAULT '',
    name         TEXT        NOT NULL DEFAULT '',
    identity_key TEXT        NOT NULL UNIQUE,
    details      JSONB       NOT NULL DEFAULT '{}',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Exactly one of to_component_id/to_resource_id is set once a link resolves;
-- both NULL is a still-unresolved suggestion (see domain.ComponentLink.Resolved).
CREATE TABLE IF NOT EXISTS component_links (
    id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id     UUID        NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    from_component_id UUID        NOT NULL REFERENCES project_components(id) ON DELETE CASCADE,
    to_component_id   UUID        REFERENCES project_components(id) ON DELETE SET NULL,
    to_resource_id    UUID        REFERENCES system_resources(id) ON DELETE SET NULL,
    protocol          TEXT        NOT NULL DEFAULT 'other',
    detail            TEXT        NOT NULL DEFAULT '',
    env_vars          JSONB       NOT NULL DEFAULT '[]',
    evidence          JSONB       NOT NULL DEFAULT '[]',
    confidence        TEXT        NOT NULL DEFAULT 'low',
    reason            TEXT        NOT NULL DEFAULT '',
    hint              TEXT        NOT NULL DEFAULT '',
    status            TEXT        NOT NULL DEFAULT 'suggested',
    source            TEXT        NOT NULL DEFAULT 'scan',
    auto_confirmed    BOOLEAN     NOT NULL DEFAULT false,
    signal_key        TEXT        NOT NULL DEFAULT '',
    missing           BOOLEAN     NOT NULL DEFAULT false,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (to_component_id IS NULL OR to_resource_id IS NULL)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_component_links_from_signal
    ON component_links(from_component_id, signal_key) WHERE signal_key <> '';
CREATE INDEX IF NOT EXISTS idx_component_links_repository_id ON component_links(repository_id);
CREATE INDEX IF NOT EXISTS idx_component_links_to_component_id ON component_links(to_component_id) WHERE to_component_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_component_links_to_resource_id ON component_links(to_resource_id) WHERE to_resource_id IS NOT NULL;

-- The nil UUID stands in for "no component" in the unique index because NULL
-- never equals NULL: two repository-level notes on the same topic would
-- otherwise both satisfy a (repository_id, component_id, topic) uniqueness
-- check.
CREATE TABLE IF NOT EXISTS project_notes (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id UUID        NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    component_id  UUID        REFERENCES project_components(id) ON DELETE CASCADE,
    topic         TEXT        NOT NULL,
    body_md       TEXT        NOT NULL DEFAULT '',
    evidence      JSONB       NOT NULL DEFAULT '[]',
    source_commit TEXT        NOT NULL DEFAULT '',
    stale         BOOLEAN     NOT NULL DEFAULT false,
    author        TEXT        NOT NULL DEFAULT 'agent',
    locked        BOOLEAN     NOT NULL DEFAULT false,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_project_notes_scope_topic
    ON project_notes(repository_id, COALESCE(component_id, '00000000-0000-0000-0000-000000000000'::uuid), topic);

CREATE TABLE IF NOT EXISTS project_scans (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id UUID        NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    trigger       TEXT        NOT NULL,
    status        TEXT        NOT NULL,
    stage         TEXT        NOT NULL DEFAULT '',
    commit_sha    TEXT        NOT NULL DEFAULT '',
    events        JSONB       NOT NULL DEFAULT '[]',
    result        JSONB,
    review_count  INT         NOT NULL DEFAULT 0,
    error         TEXT        NOT NULL DEFAULT '',
    started_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at   TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_project_scans_repository_started ON project_scans(repository_id, started_at DESC);

ALTER TABLE board_tasks ADD COLUMN IF NOT EXISTS component_id UUID REFERENCES project_components(id) ON DELETE SET NULL;
