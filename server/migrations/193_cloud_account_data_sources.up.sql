-- The data sources made for a connected cloud account on its own (Vercel ->
-- vercel, Google Cloud -> gcp_monitoring, AWS -> aws_cloudwatch): one row per
-- account and kind, so a source is created once and never again. A row whose
-- data_source_id is NULL records that its source was deleted by hand, which
-- stops it coming back. Disconnecting the account deletes its rows.
CREATE TABLE IF NOT EXISTS cloud_account_data_sources (
    cloud_account_id UUID        NOT NULL REFERENCES cloud_accounts(id) ON DELETE CASCADE,
    kind             TEXT        NOT NULL,
    data_source_id   UUID        REFERENCES data_sources(id) ON DELETE SET NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (cloud_account_id, kind)
);
