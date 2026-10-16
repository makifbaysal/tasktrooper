-- Observability data sources: one row per configured connection of a
-- registered kind (prometheus, ...). config is the kind's non-secret fields;
-- the secret ones are encrypted together as one JSON blob, like
-- cloud_accounts.secret_enc, and NULL when the source has none. secrets_set
-- names the stored secret fields so a read never has to decrypt to say which
-- are set.
CREATE TABLE IF NOT EXISTS data_sources (
    id            UUID        PRIMARY KEY,
    kind          TEXT        NOT NULL,
    name          TEXT        NOT NULL,
    config        JSONB       NOT NULL DEFAULT '{}',
    secret_enc    BYTEA,
    secrets_set   TEXT[]      NOT NULL DEFAULT '{}',
    status        TEXT        NOT NULL DEFAULT 'unverified'
        CHECK (status IN ('ok', 'error', 'unverified')),
    status_detail TEXT        NOT NULL DEFAULT '',
    info          JSONB       NOT NULL DEFAULT '{}',
    verified_at   TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Recipes a person added beside the embedded catalog; body is the recipe as
-- JSON (domain.Recipe), id its recipe id. A row with a catalog recipe's id
-- replaces that recipe.
CREATE TABLE IF NOT EXISTS observability_recipes (
    id         TEXT        PRIMARY KEY,
    body       JSONB       NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
