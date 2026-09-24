-- Cloud provider accounts and the per-(component, environment) bindings that
-- point at what runs where. Replaces repository_gcloud_resources' single-
-- provider, single-binding-per-sub-project shape with one that spans
-- providers and holds a candidate list until a suggested binding is confirmed.

CREATE TABLE IF NOT EXISTS cloud_accounts (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    provider      TEXT        NOT NULL,
    label         TEXT        NOT NULL DEFAULT '',
    meta          JSONB       NOT NULL DEFAULT '{}',
    secret_enc    BYTEA       NOT NULL,
    status        TEXT        NOT NULL DEFAULT 'unverified',
    status_detail TEXT        NOT NULL DEFAULT '',
    verified_at   TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_cloud_accounts_provider ON cloud_accounts(provider);

-- Provider "" with only url/health_url is a custom environment the platform
-- can probe but not read logs from (see domain.ComponentEnvironment).
CREATE TABLE IF NOT EXISTS component_environments (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id  UUID        NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    component_id   UUID        NOT NULL REFERENCES project_components(id) ON DELETE CASCADE,
    environment    TEXT        NOT NULL,
    provider       TEXT        NOT NULL DEFAULT '',
    account_id     UUID        REFERENCES cloud_accounts(id) ON DELETE SET NULL,
    resource       JSONB,
    url            TEXT        NOT NULL DEFAULT '',
    health_url     TEXT        NOT NULL DEFAULT '',
    status         TEXT        NOT NULL DEFAULT 'suggested',
    source         TEXT        NOT NULL DEFAULT 'scan',
    confidence     TEXT        NOT NULL DEFAULT 'low',
    reason         TEXT        NOT NULL DEFAULT '',
    auto_confirmed BOOLEAN     NOT NULL DEFAULT false,
    candidates     JSONB       NOT NULL DEFAULT '[]',
    signal_key     TEXT        NOT NULL DEFAULT '',
    health         JSONB,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (component_id, environment)
);

CREATE INDEX IF NOT EXISTS idx_component_environments_repository_id ON component_environments(repository_id);
CREATE INDEX IF NOT EXISTS idx_component_environments_account_id ON component_environments(account_id) WHERE account_id IS NOT NULL;
