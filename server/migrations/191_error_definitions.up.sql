-- a. Error definitions: what a recurring error group means and what to do
--    about it. One definition matches a group when every field set in
--    `match` holds (AND); matching happens on read, so no group row is
--    stored. component_id NULL covers every component of the repository.
--    firing_since is the alert/incident action's own state: set while the
--    matched groups' count in the threshold window is at or above the
--    threshold, cleared when it drops below.
CREATE TABLE IF NOT EXISTS error_definitions (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id   UUID        NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    component_id    UUID        REFERENCES project_components(id) ON DELETE CASCADE,
    name            TEXT        NOT NULL CHECK (name <> ''),
    match           JSONB       NOT NULL,
    category        TEXT        NOT NULL
        CHECK (category IN ('bug', 'expected', 'dependency', 'infrastructure', 'user_error')),
    severity        TEXT        NOT NULL
        CHECK (severity IN ('critical', 'high', 'medium', 'low')),
    action          TEXT        NOT NULL
        CHECK (action IN ('none', 'ignore', 'alert', 'incident', 'task')),
    alert_threshold JSONB,
    source          TEXT        NOT NULL CHECK (source IN ('human', 'ai', 'code')),
    status          TEXT        NOT NULL CHECK (status IN ('active', 'suggested', 'dismissed')),
    rationale       TEXT        NOT NULL DEFAULT '',
    evidence        JSONB       NOT NULL DEFAULT '[]',
    task_id         UUID        REFERENCES board_tasks(id) ON DELETE SET NULL,
    firing_since    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_error_definitions_repository ON error_definitions (repository_id, status);
CREATE INDEX IF NOT EXISTS idx_error_definitions_component ON error_definitions (component_id)
    WHERE component_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_error_definitions_acting ON error_definitions (repository_id)
    WHERE status = 'active' AND action IN ('alert', 'incident', 'task');

-- b. The side effects a definition must take only once per error group: the
--    task action's board task. A row is claimed before the task is opened,
--    so two sweeps never open it twice.
CREATE TABLE IF NOT EXISTS error_group_links (
    definition_id    UUID        NOT NULL REFERENCES error_definitions(id) ON DELETE CASCADE,
    fingerprint      TEXT        NOT NULL,
    component_id     UUID,
    environment_id   UUID,
    data_source_id   UUID,
    task_id          UUID        REFERENCES board_tasks(id) ON DELETE SET NULL,
    first_matched_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (definition_id, fingerprint)
);

-- c. AI jobs: one chat turn with the release engineer that proposes error
--    definitions, signal bindings or alert rules. created lists what its
--    tools stored, in order. session_id has no foreign key: a chat may be
--    deleted while its job's outcome stays readable.
CREATE TABLE IF NOT EXISTS ai_jobs (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    kind           TEXT        NOT NULL CHECK (kind IN ('define_errors', 'error_catalog', 'map_signals')),
    repository_id  UUID        NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    component_id   UUID        REFERENCES project_components(id) ON DELETE CASCADE,
    environment    TEXT        NOT NULL
        CHECK (environment IN ('production', 'staging', 'preview', 'development')),
    status         TEXT        NOT NULL CHECK (status IN ('running', 'done', 'failed')),
    session_id     UUID,
    result_summary TEXT        NOT NULL DEFAULT '',
    error          TEXT        NOT NULL DEFAULT '',
    created        JSONB       NOT NULL DEFAULT '[]',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at    TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_ai_jobs_session ON ai_jobs (session_id) WHERE session_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_ai_jobs_running ON ai_jobs (kind, repository_id) WHERE status = 'running';

-- d. Who wrote an alert rule: a person, or the release engineer's
--    propose_alert_rule (always a disabled draft a person enables).
ALTER TABLE alert_rules
    ADD COLUMN IF NOT EXISTS created_by TEXT NOT NULL DEFAULT 'human'
        CHECK (created_by IN ('human', 'ai'));
