-- Issue sync: GitHub and Jira issues imported as board tasks, with write-back.
--
-- issue_links has one row per task that came from an issue. An issue the
-- product manager split into several tasks has several rows, so the old
-- one-link-per-issue unique key is dropped where an earlier schema created it.
-- issue_imports holds the issue itself, one row per (provider, external_key):
-- it outlives the task the import opened first, which the conversion replaces,
-- and it is what keeps an issue from being imported twice.
CREATE TABLE IF NOT EXISTS issue_links (
    id             UUID PRIMARY KEY,
    task_id        UUID NOT NULL UNIQUE REFERENCES board_tasks(id) ON DELETE CASCADE,
    task_key       TEXT NOT NULL DEFAULT '',
    imported_by    TEXT NOT NULL DEFAULT '',
    repository_id  UUID NOT NULL,
    provider       TEXT NOT NULL CHECK (provider IN ('github', 'jira')),
    external_key   TEXT NOT NULL,
    url            TEXT NOT NULL,
    title          TEXT NOT NULL DEFAULT '',
    last_column    TEXT NOT NULL DEFAULT '',
    closed_at      TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE issue_links DROP CONSTRAINT IF EXISTS issue_links_provider_external_key_key;

CREATE INDEX IF NOT EXISTS idx_issue_links_repository_id ON issue_links(repository_id);
CREATE INDEX IF NOT EXISTS idx_issue_links_issue ON issue_links(provider, external_key);

CREATE TABLE IF NOT EXISTS issue_imports (
    id                     UUID PRIMARY KEY,
    provider               TEXT NOT NULL CHECK (provider IN ('github', 'jira')),
    external_key           TEXT NOT NULL,
    repository_id          UUID NOT NULL,
    url                    TEXT NOT NULL,
    title                  TEXT NOT NULL DEFAULT '',
    imported_by            TEXT NOT NULL DEFAULT '',
    intake_task_id         UUID REFERENCES board_tasks(id) ON DELETE SET NULL,
    conversion_status      TEXT NOT NULL DEFAULT ''
        CHECK (conversion_status IN ('', 'pending', 'converting', 'converted', 'needs_input', 'failed', 'skipped')),
    conversion_session_id  UUID REFERENCES sessions(id) ON DELETE SET NULL,
    conversion_error       TEXT NOT NULL DEFAULT '',
    closed_at              TIMESTAMPTZ,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, external_key)
);

CREATE INDEX IF NOT EXISTS idx_issue_imports_session ON issue_imports(conversion_session_id)
    WHERE conversion_session_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_issue_imports_open_conversion ON issue_imports(conversion_status)
    WHERE conversion_status IN ('pending', 'converting');

-- Links written before issue_imports existed each stand for one import.
INSERT INTO issue_imports (id, provider, external_key, repository_id, url, title, imported_by,
                           intake_task_id, closed_at, created_at, updated_at)
SELECT DISTINCT ON (provider, external_key)
       gen_random_uuid(), provider, external_key, repository_id, url, title, imported_by,
       task_id, closed_at, created_at, created_at
FROM issue_links
ORDER BY provider, external_key, created_at
ON CONFLICT (provider, external_key) DO NOTHING;
