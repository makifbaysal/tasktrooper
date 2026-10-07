-- One row per test build: a task's (or the default branch's) binary uploaded to
-- TestFlight or to Play internal app sharing for a person to try.
--
-- build_sequence is the counter both stores order builds by. It is recorded
-- here as well as read from the store because a build that failed before its
-- upload still burned its number on this side, and handing the next task the
-- same number would only fail at the store.
CREATE TABLE IF NOT EXISTS store_test_builds (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id  UUID        NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    platform       TEXT        NOT NULL CHECK (platform IN ('ios', 'android')),
    task_id        UUID        REFERENCES board_tasks(id) ON DELETE SET NULL,
    task_key       TEXT        NOT NULL DEFAULT '',
    task_number    INT         NOT NULL DEFAULT 0,
    attempt        INT         NOT NULL DEFAULT 0,
    build_sequence BIGINT      NOT NULL,
    build_number   TEXT        NOT NULL,
    version_name   TEXT        NOT NULL DEFAULT '',
    commit_sha     TEXT        NOT NULL DEFAULT '',
    branch         TEXT        NOT NULL DEFAULT '',
    engine         TEXT        NOT NULL DEFAULT '',
    status         TEXT        NOT NULL,
    failure        TEXT        NOT NULL DEFAULT '',
    store_build_id TEXT        NOT NULL DEFAULT '',
    install_url    TEXT        NOT NULL DEFAULT '',
    artifact_path  TEXT        NOT NULL DEFAULT '',
    test_groups    JSONB       NOT NULL DEFAULT '[]',
    notes          TEXT        NOT NULL DEFAULT '',
    run_url        TEXT        NOT NULL DEFAULT '',
    log_tail       TEXT        NOT NULL DEFAULT '',
    trigger_source TEXT        NOT NULL DEFAULT 'manual',
    created_by     TEXT        NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at    TIMESTAMPTZ,
    UNIQUE (repository_id, platform, build_number)
);

CREATE INDEX IF NOT EXISTS store_test_builds_repo_idx
    ON store_test_builds (repository_id, platform, created_at DESC);
CREATE INDEX IF NOT EXISTS store_test_builds_task_idx
    ON store_test_builds (task_id, platform) WHERE task_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS store_test_builds_unfinished_idx
    ON store_test_builds (status) WHERE status NOT IN ('ready', 'failed', 'dispatched');

-- Which TestFlight groups / Play tracks a new test build is opened to the
-- moment it is ready. No row = the platform default (every TestFlight internal
-- group; no Play track, the internal app sharing link only).
CREATE TABLE IF NOT EXISTS store_test_settings (
    repository_id UUID        NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    platform      TEXT        NOT NULL CHECK (platform IN ('ios', 'android')),
    auto_groups   JSONB       NOT NULL DEFAULT '[]',
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (repository_id, platform)
);

-- Human UAT on the coding types now builds the task for the stores. It only
-- does anything for a repository with a linked store app, and a workflow can
-- drop it like any other behaviour.
UPDATE workflow_stages
SET behaviours = behaviours || '[{"key":"store_test_build_on_enter"}]'::jsonb
WHERE column_slug = 'human_uat'
  AND task_type IN ('task', 'bug', 'technical')
  AND NOT behaviours @> '[{"key":"store_test_build_on_enter"}]';
