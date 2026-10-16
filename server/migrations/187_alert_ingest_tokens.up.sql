-- Alert ingest tokens: what an external alerter (Alertmanager, Grafana, Sentry,
-- Cloud Monitoring) presents in the URL of POST /v1/alerts/ingest/:token
-- instead of the server API key. Only the token's sha256 is kept; the
-- plaintext is shown once, when the token is created. Deleting the repository
-- revokes its tokens.
CREATE TABLE IF NOT EXISTS alert_ingest_tokens (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id UUID        NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    label         TEXT        NOT NULL,
    token_hash    TEXT        NOT NULL UNIQUE,
    default_env   TEXT        NOT NULL DEFAULT 'prod',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at  TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_alert_ingest_tokens_repository ON alert_ingest_tokens(repository_id, created_at DESC);
