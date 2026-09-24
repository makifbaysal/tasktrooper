ALTER TABLE system_resources ADD COLUMN IF NOT EXISTS name_locked BOOLEAN NOT NULL DEFAULT false;

-- A merged-away identity key must keep resolving to its merge target, or the
-- next rescan's EnsureResource would resurrect the resource it was merged into.
CREATE TABLE IF NOT EXISTS system_resource_aliases (
    identity_key TEXT        PRIMARY KEY,
    resource_id  UUID        NOT NULL REFERENCES system_resources(id) ON DELETE CASCADE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_system_resource_aliases_resource_id ON system_resource_aliases(resource_id);
