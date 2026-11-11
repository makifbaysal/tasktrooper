-- Remote access: phones paired to this computer through a tunnel. A device
-- holds a long-lived bearer token whose SHA-256 is all that is stored here;
-- a pairing is the short-lived, single-use code that is exchanged for one.
CREATE TABLE IF NOT EXISTS remote_devices (
    id           UUID PRIMARY KEY,
    name         TEXT NOT NULL,
    platform     TEXT NOT NULL DEFAULT '',
    role         TEXT NOT NULL CHECK (role IN ('operator', 'viewer')),
    token_hash   TEXT NOT NULL UNIQUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ,
    expires_at   TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS remote_pairings (
    id              UUID PRIMARY KEY,
    code_hash       TEXT NOT NULL UNIQUE,
    role            TEXT NOT NULL CHECK (role IN ('operator', 'viewer')),
    expires_in_days INT NOT NULL DEFAULT 0,
    device_label    TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at      TIMESTAMPTZ NOT NULL,
    used_at         TIMESTAMPTZ,
    cancelled_at    TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS remote_audit (
    id          BIGSERIAL PRIMARY KEY,
    at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    device_id   UUID,
    device_name TEXT NOT NULL DEFAULT '',
    method      TEXT NOT NULL DEFAULT '',
    route       TEXT NOT NULL DEFAULT '',
    status      INT NOT NULL DEFAULT 0,
    kind        TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS remote_audit_at_idx ON remote_audit (at DESC);
CREATE INDEX IF NOT EXISTS remote_audit_device_idx ON remote_audit (device_id, at DESC);

CREATE TABLE IF NOT EXISTS remote_settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
