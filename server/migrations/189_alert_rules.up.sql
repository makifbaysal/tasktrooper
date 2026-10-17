-- Alert rules: a condition on one signal of one component environment (read
-- through its confirmed binding) or on a raw query against a data source,
-- evaluated in the background. The rule row also keeps where its evaluation
-- stands (state, since when, the last value); updated_at moves only when a
-- person edits the rule, never on an evaluation. Deleting the component, or
-- the data source of a raw rule, deletes the rule and its events.
CREATE TABLE IF NOT EXISTS alert_rules (
    id                UUID             PRIMARY KEY DEFAULT gen_random_uuid(),
    name              TEXT             NOT NULL CHECK (name <> ''),
    component_id      UUID             NOT NULL REFERENCES project_components(id) ON DELETE CASCADE,
    environment       TEXT             NOT NULL
        CHECK (environment IN ('production', 'staging', 'preview', 'development')),
    signal            TEXT             NOT NULL DEFAULT '',
    data_source_id    UUID             REFERENCES data_sources(id) ON DELETE CASCADE,
    query             TEXT             NOT NULL DEFAULT '',
    condition         JSONB            NOT NULL,
    severity          TEXT             NOT NULL
        CHECK (severity IN ('critical', 'high', 'medium', 'low')),
    channels          TEXT[]           NOT NULL DEFAULT '{}',
    open_incident     BOOLEAN          NOT NULL DEFAULT false,
    enabled           BOOLEAN          NOT NULL DEFAULT true,
    state             TEXT             NOT NULL DEFAULT 'ok'
        CHECK (state IN ('ok', 'pending', 'firing', 'no_data')),
    state_since       TIMESTAMPTZ      NOT NULL DEFAULT now(),
    last_value        DOUBLE PRECISION,
    last_evaluated_at TIMESTAMPTZ,
    created_at        TIMESTAMPTZ      NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ      NOT NULL DEFAULT now(),
    CHECK (
        (signal <> '' AND query = '' AND data_source_id IS NULL)
        OR (signal = '' AND query <> '' AND data_source_id IS NOT NULL)
    )
);

CREATE INDEX IF NOT EXISTS idx_alert_rules_component_env ON alert_rules (component_id, environment);
CREATE INDEX IF NOT EXISTS idx_alert_rules_due ON alert_rules (last_evaluated_at NULLS FIRST) WHERE enabled;
CREATE INDEX IF NOT EXISTS idx_alert_rules_data_source ON alert_rules (data_source_id) WHERE data_source_id IS NOT NULL;

-- One row per state change of a rule. incident_id is the incident its firing
-- opened or its recovery resolved; it is not a foreign key, so an incident
-- row's lifetime never constrains the rule's history.
CREATE TABLE IF NOT EXISTS alert_events (
    id          UUID             PRIMARY KEY DEFAULT gen_random_uuid(),
    rule_id     UUID             NOT NULL REFERENCES alert_rules(id) ON DELETE CASCADE,
    at          TIMESTAMPTZ      NOT NULL DEFAULT now(),
    from_state  TEXT             NOT NULL,
    to_state    TEXT             NOT NULL,
    value       DOUBLE PRECISION,
    incident_id UUID
);

CREATE INDEX IF NOT EXISTS idx_alert_events_rule_at ON alert_events (rule_id, at DESC);
CREATE INDEX IF NOT EXISTS idx_alert_events_at ON alert_events (at);
