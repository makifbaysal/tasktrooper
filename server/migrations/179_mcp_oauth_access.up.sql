-- Per-server agent access for MCP servers, and OAuth sign-in for remote ones.
--
-- access = 'all'    every agent whose tool policy does not name its MCP
--                   servers gets this server's tools (the only behaviour
--                   before this migration, so every existing row keeps it);
-- access = 'listed' only agents whose allow_mcp_servers names the server.
-- A row inserted without a value is 'listed': a new server reaches no agent
-- until someone grants it.

ALTER TABLE mcp_servers ADD COLUMN IF NOT EXISTS access TEXT NOT NULL DEFAULT 'all';
ALTER TABLE mcp_servers ALTER COLUMN access SET DEFAULT 'listed';

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'mcp_servers_access_check') THEN
        ALTER TABLE mcp_servers ADD CONSTRAINT mcp_servers_access_check CHECK (access IN ('all', 'listed'));
    END IF;
END $$;

-- One OAuth sign-in per HTTP MCP server. client_secret, access_token and
-- refresh_token are ciphertext from the same cipher as mcp_server_secrets;
-- everything else is metadata the server list and the refresh need in clear.
CREATE TABLE IF NOT EXISTS mcp_server_oauth (
    server_id TEXT PRIMARY KEY REFERENCES mcp_servers(id) ON DELETE CASCADE,
    metadata JSONB NOT NULL DEFAULT '{}',
    client_id TEXT NOT NULL DEFAULT '',
    client_secret BYTEA,
    client_auth_method TEXT NOT NULL DEFAULT '',
    client_redirect_uri TEXT NOT NULL DEFAULT '',
    client_dynamic BOOLEAN NOT NULL DEFAULT false,
    access_token BYTEA,
    refresh_token BYTEA,
    token_type TEXT NOT NULL DEFAULT '',
    scope TEXT NOT NULL DEFAULT '',
    expires_at TIMESTAMPTZ,
    expired BOOLEAN NOT NULL DEFAULT false,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
