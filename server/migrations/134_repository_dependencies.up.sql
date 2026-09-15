CREATE TABLE IF NOT EXISTS repository_dependencies (
    id                      UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id           UUID        NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    target_kind             TEXT        NOT NULL,
    target_repository_id    UUID        REFERENCES repositories(id) ON DELETE CASCADE,
    target_sub_project_path TEXT        NOT NULL DEFAULT '',
    database_label          TEXT        NOT NULL DEFAULT '',
    database_engine         TEXT        NOT NULL DEFAULT '',
    database_env            TEXT        NOT NULL DEFAULT '',
    database_host           TEXT        NOT NULL DEFAULT '',
    database_port           INT         NOT NULL DEFAULT 0,
    database_name           TEXT        NOT NULL DEFAULT '',
    database_username       TEXT        NOT NULL DEFAULT '',
    database_secret_enc     BYTEA,
    note                    TEXT        NOT NULL DEFAULT '',
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_repository_dependencies_repository_id ON repository_dependencies(repository_id);
CREATE INDEX IF NOT EXISTS idx_repository_dependencies_target_repository_id ON repository_dependencies(target_repository_id) WHERE target_repository_id IS NOT NULL;
