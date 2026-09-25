-- Releases and the release engineer.
--
-- A merge used to be followed by guesswork: whether anything deployed was read
-- off which workflows and deploy targets happened to exist, a green deploy job
-- was the whole of "released", and QA — whose rules forbid touching production
-- — was the agent asked to watch it. From here on a component states how it
-- ships (project_components.delivery), every merge opens a release that is
-- deployed, soaked and judged, and a dedicated release-engineer agent owns
-- everything that happens in `done` and `released`.

-- a. The delivery profile, a Fact like every other component field.
ALTER TABLE project_components
    ADD COLUMN IF NOT EXISTS delivery JSONB NOT NULL DEFAULT '{}';

-- b. Releases and the tasks each one carries.
CREATE TABLE IF NOT EXISTS releases (
    id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id     UUID        NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    component_id      UUID        REFERENCES project_components(id) ON DELETE SET NULL,
    version           TEXT        NOT NULL DEFAULT '',
    mode              TEXT        NOT NULL CHECK (mode IN ('on_merge','dispatch','batch','none')),
    executor          TEXT        NOT NULL DEFAULT '',
    status            TEXT        NOT NULL CHECK (status IN ('draft','pending','deploying','verifying','awaiting_verdict','rolling_back','released','rolled_back','failed','superseded')),
    commit_sha        TEXT        NOT NULL DEFAULT '',
    tag               TEXT        NOT NULL DEFAULT '',
    notes             TEXT        NOT NULL DEFAULT '',
    profile           JSONB       NOT NULL DEFAULT '{}',
    deploy            JSONB,
    checks            JSONB       NOT NULL DEFAULT '{}',
    verdict           TEXT        NOT NULL DEFAULT '',
    rollback          JSONB,
    failure_reason    TEXT        NOT NULL DEFAULT '',
    card_task_id      UUID        REFERENCES board_tasks(id) ON DELETE SET NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    deploy_started_at TIMESTAMPTZ,
    deployed_at       TIMESTAMPTZ,
    verify_until      TIMESTAMPTZ,
    finished_at       TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_releases_repository ON releases(repository_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_releases_component ON releases(component_id, created_at DESC);
-- The sweeper's working set: only releases something still has to advance.
CREATE INDEX IF NOT EXISTS idx_releases_watched ON releases(status)
    WHERE status IN ('deploying','verifying','rolling_back');
-- At most one draft per component: a batch collects into exactly one.
CREATE UNIQUE INDEX IF NOT EXISTS idx_releases_one_draft ON releases(component_id)
    WHERE status = 'draft';

CREATE TABLE IF NOT EXISTS release_tasks (
    release_id UUID        NOT NULL REFERENCES releases(id) ON DELETE CASCADE,
    task_id    UUID        NOT NULL REFERENCES board_tasks(id) ON DELETE CASCADE,
    added_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (release_id, task_id)
);

CREATE INDEX IF NOT EXISTS idx_release_tasks_task ON release_tasks(task_id);

-- c. The role. The release-engineer agent itself is created by the catalog
--    sync at boot (catalog/agents/release-engineer), which assigns this role
--    and subscribes it to done/released.
INSERT INTO roles (key, name, description, required_tools) VALUES
    ('release', 'Release Engineer', 'Merges signed-off work, ships it, verifies production after the deploy and rolls back what breaks.', '{}')
ON CONFLICT (key) DO NOTHING;

-- d. QA leaves done/released. Its merge/watch/rollback duties move to the
--    release engineer; QA's work ends with its in_qa verdict.
DELETE FROM agent_column_subscriptions s
USING agents a
WHERE a.id = s.agent_id
  AND a.name = 'qa-agent'
  AND s.column_slug IN ('done', 'released');

UPDATE workflow_stage_participants p
SET role_id = (SELECT id FROM roles WHERE key = 'release')
FROM workflow_stages ws
WHERE ws.id = p.stage_id
  AND ws.task_type IN ('task', 'bug', 'technical')
  AND ws.column_slug IN ('done', 'released')
  AND p.role_id = (SELECT id FROM roles WHERE key = 'qa');

-- e. `released` never commits or builds: the only run there is the release
--    engineer's health-window wake, and a commit after the merge re-pushed the
--    deleted task branch. Writers are stripped; release control stays.
UPDATE workflow_stages
SET behaviours = COALESCE(
    (SELECT jsonb_agg(elem) FROM jsonb_array_elements(behaviours) elem
     WHERE elem->>'key' NOT IN ('commit_on_finish', 'build_verify', 'strip_writers'))
    , '[]'::jsonb
) || '[{"key":"strip_writers","params":{"allow":"release_control"}}]'::jsonb
WHERE column_slug = 'released';
