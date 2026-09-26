-- Reference docs (coding/test standards, architecture, local run) are now
-- per-component: a single-purpose repo's docs belong to its root component
-- ("."), and a monorepo's root, if it has no component of its own, needs
-- none. The component's own value wins on any key both sides set.
UPDATE project_components c
SET docs = r.docs || c.docs, updated_at = now()
FROM repositories r
WHERE c.repository_id = r.id AND c.path = '.' AND r.docs <> '{}'::jsonb;
