-- Signal bindings: where one signal of one component environment is read — a
-- data source plus a recipe and its params, or a raw query (which wins when
-- set). At most one confirmed binding per (component, environment, signal);
-- suggestions may be several, and a dismissed row stays so the detector does
-- not offer it again. Deleting the component or the data source deletes its
-- bindings.
CREATE TABLE IF NOT EXISTS signal_bindings (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    component_id   UUID        NOT NULL REFERENCES project_components(id) ON DELETE CASCADE,
    environment    TEXT        NOT NULL
        CHECK (environment IN ('production', 'staging', 'preview', 'development')),
    signal         TEXT        NOT NULL,
    data_source_id UUID        NOT NULL REFERENCES data_sources(id) ON DELETE CASCADE,
    recipe_id      TEXT        NOT NULL DEFAULT '',
    params         JSONB       NOT NULL DEFAULT '{}',
    query          TEXT        NOT NULL DEFAULT '',
    status         TEXT        NOT NULL
        CHECK (status IN ('confirmed', 'suggested', 'dismissed')),
    source         TEXT        NOT NULL
        CHECK (source IN ('detected', 'human', 'ai')),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (recipe_id <> '' OR query <> '')
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_signal_bindings_confirmed
    ON signal_bindings (component_id, environment, signal) WHERE status = 'confirmed';
CREATE INDEX IF NOT EXISTS idx_signal_bindings_component_env ON signal_bindings (component_id, environment);
CREATE INDEX IF NOT EXISTS idx_signal_bindings_confirmed_env ON signal_bindings (environment) WHERE status = 'confirmed';
CREATE INDEX IF NOT EXISTS idx_signal_bindings_data_source ON signal_bindings (data_source_id);
