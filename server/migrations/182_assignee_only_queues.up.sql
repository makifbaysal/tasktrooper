-- todo and need_revision are assignee-only queues now: a card there wakes its
-- assignee and nobody else, and an unassigned one waits for an assignee. The
-- catalog seed used to treat them as single seats, so only the first developer
-- installed (backend) was subscribed and every other developer, and the
-- designer, came up watching no column at all.
--
-- Fresh installs get these rows from the catalog sync. This fills them in for
-- an existing install, only for agents that were left with no subscription at
-- all, so a column set someone chose by hand stays as it is.
INSERT INTO agent_column_subscriptions (agent_id, column_slug)
SELECT DISTINCT ra.agent_id, bc.slug
FROM agent_role_assignments ra
JOIN roles r ON r.id = ra.role_id AND r.key IN ('developer', 'designer')
JOIN board_columns bc ON bc.slug IN ('todo', 'need_revision')
WHERE NOT EXISTS (
    SELECT 1 FROM agent_column_subscriptions s WHERE s.agent_id = ra.agent_id
)
ON CONFLICT DO NOTHING;
