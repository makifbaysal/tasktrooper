-- Which shipped MCP templates this install has already been given.
--
-- The boot seed adds every template that is not listed here and then lists
-- it. A template shipped in a later release therefore reaches installs that
-- already have servers, and a seeded server someone deleted stays deleted.
-- Before this table the catalog was seeded only into an empty mcp_servers, so
-- an install never saw a template added after its first launch.
CREATE TABLE IF NOT EXISTS mcp_template_seeds (
    template_id TEXT PRIMARY KEY,
    seeded_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Every server on file came from that first seed or was added by hand; either
-- way it is not to be seeded again.
INSERT INTO mcp_template_seeds (template_id)
SELECT id FROM mcp_servers
ON CONFLICT DO NOTHING;

-- These seven were in the first release's catalog, so every install that has
-- servers was seeded with them; one that is missing was deleted on purpose.
INSERT INTO mcp_template_seeds (template_id)
SELECT t.id
FROM (VALUES ('filesystem'), ('git'), ('github'), ('postgres'), ('slack'), ('huggingface'), ('browser')) AS t(id)
WHERE EXISTS (SELECT 1 FROM mcp_servers)
ON CONFLICT DO NOTHING;
