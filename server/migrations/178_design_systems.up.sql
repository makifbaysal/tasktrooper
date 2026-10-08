-- The designer role, the `design` task type and the design system store.
--
-- A design system has two layers: one base per project (scope = 'project')
-- and an optional layer per repository (scope = 'repository') that adds to or
-- overrides the base. Every proposal is a new version written by a design
-- task; approving that task in analiz_review approves the version and
-- supersedes the one before it.

INSERT INTO roles (key, name, description, required_tools) VALUES
    ('designer', 'Designer', 'Owns the design system and designs screens before they are built.', '{}')
ON CONFLICT (key) DO NOTHING;

INSERT INTO task_types (key, label, key_prefix, position, is_default, is_defect, assignee_role_id, assignee_mode, behaviours, built_in)
SELECT 'design', 'Design',
       CASE WHEN EXISTS (SELECT 1 FROM task_types WHERE key_prefix = 'D') THEN 'DSG' ELSE 'D' END,
       4, false, false, (SELECT id FROM roles WHERE key = 'designer'), 'override',
       '[{"key":"no_workspace_writes"}]', true
WHERE NOT EXISTS (SELECT 1 FROM task_types WHERE key = 'design');

-- The analiz spine (backlog, todo, in_progress, analiz_review, need_revision,
-- blocked, done, released): a design ships documents and a design system
-- proposal, never a diff, and the human approves it in analiz_review.
INSERT INTO workflow_stages (task_type, column_slug, position, on_path, kind, behaviours, instructions) VALUES
('design', 'backlog', COALESCE((SELECT position FROM board_columns WHERE slug = 'backlog'), 0), true, 'intake', '[]', ''),
('design', 'todo', COALESCE((SELECT position FROM board_columns WHERE slug = 'todo'), 1), true, 'queue',
    '[{"key":"auto_enter","params":{"to":"in_progress","assignee_only":"true"}},{"key":"block_on_dependencies"},{"key":"commit_on_finish"}]', ''),
('design', 'in_progress', COALESCE((SELECT position FROM board_columns WHERE slug = 'in_progress'), 2), true, 'work',
    '[{"key":"block_on_dependencies","params":{"refuse_move":"true"}},{"key":"commit_on_finish"},{"key":"advance_on_document","params":{"to":"analiz_review"}}]', ''),
('design', 'analiz_review', COALESCE((SELECT position FROM board_columns WHERE slug = 'analiz_review'), 3), true, 'approval',
    '[{"key":"route_to_subscribers"},{"key":"strip_writers"},{"key":"review_chain_stage","params":{"label":"design review","remedy":"move it to analiz_review and approve the design there"}}]', ''),
('design', 'need_revision', COALESCE((SELECT position FROM board_columns WHERE slug = 'need_revision'), 7), false, 'rework',
    '[{"key":"auto_enter","params":{"to":"in_progress","assignee_only":"true"}},{"key":"commit_on_finish"},{"key":"advance_on_document","params":{"to":"analiz_review"}}]', ''),
('design', 'blocked', COALESCE((SELECT position FROM board_columns WHERE slug = 'blocked'), 10), false, 'parked', '[]', ''),
('design', 'done', COALESCE((SELECT position FROM board_columns WHERE slug = 'done'), 11), true, 'terminal',
    '[{"key":"ensure_pr_on_enter"},{"key":"forward_exit"},{"key":"require_criteria_complete"},{"key":"strip_writers","params":{"allow":"merge_task_pull_request,release_control"}},{"key":"approve_design_system_on_enter"}]', ''),
('design', 'released', COALESCE((SELECT position FROM board_columns WHERE slug = 'released'), 12), true, 'terminal',
    '[{"key":"forward_exit"},{"key":"require_criteria_complete"},{"key":"strip_writers","params":{"allow":"release_control"}}]', '')
ON CONFLICT (task_type, column_slug) DO NOTHING;

INSERT INTO workflow_stage_participants (stage_id, role_id, mode)
SELECT s.id, r.id, 'worker'
FROM workflow_stages s, roles r
WHERE s.task_type = 'design'
  AND s.column_slug IN ('in_progress', 'need_revision', 'done')
  AND r.key = 'designer'
ON CONFLICT (stage_id, role_id) DO NOTHING;

CREATE TABLE IF NOT EXISTS design_systems (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    scope           TEXT        NOT NULL CHECK (scope IN ('project', 'repository')),
    project_id      UUID        REFERENCES projects(id) ON DELETE CASCADE,
    repository_id   UUID        REFERENCES repositories(id) ON DELETE CASCADE,
    version         INT         NOT NULL,
    status          TEXT        NOT NULL CHECK (status IN ('in_review', 'approved', 'superseded')),
    design_md       TEXT        NOT NULL DEFAULT '',
    tokens          JSONB       NOT NULL DEFAULT '{}',
    inventory_md    TEXT        NOT NULL DEFAULT '',
    rationale       TEXT        NOT NULL DEFAULT '',
    source_task_id  UUID        REFERENCES board_tasks(id) ON DELETE SET NULL,
    source_task_key TEXT        NOT NULL DEFAULT '',
    created_by      TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    approved_at     TIMESTAMPTZ,
    CHECK (
        (scope = 'project' AND project_id IS NOT NULL AND repository_id IS NULL) OR
        (scope = 'repository' AND repository_id IS NOT NULL AND project_id IS NULL)
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS design_systems_project_version
    ON design_systems (project_id, version) WHERE scope = 'project';
CREATE UNIQUE INDEX IF NOT EXISTS design_systems_repository_version
    ON design_systems (repository_id, version) WHERE scope = 'repository';
CREATE UNIQUE INDEX IF NOT EXISTS design_systems_project_approved
    ON design_systems (project_id) WHERE scope = 'project' AND status = 'approved';
CREATE UNIQUE INDEX IF NOT EXISTS design_systems_repository_approved
    ON design_systems (repository_id) WHERE scope = 'repository' AND status = 'approved';
CREATE INDEX IF NOT EXISTS design_systems_source_task
    ON design_systems (source_task_id) WHERE source_task_id IS NOT NULL;

-- The design task the UI opened for a target ("create from the project" on
-- the project or repository Design System tab), so a second click while it is
-- still running points at it instead of opening another.
CREATE TABLE IF NOT EXISTS design_system_requests (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    scope         TEXT        NOT NULL CHECK (scope IN ('project', 'repository')),
    project_id    UUID        REFERENCES projects(id) ON DELETE CASCADE,
    repository_id UUID        REFERENCES repositories(id) ON DELETE CASCADE,
    task_id       UUID        NOT NULL REFERENCES board_tasks(id) ON DELETE CASCADE,
    -- A project's design task runs in one of the project's repositories.
    task_repository_id UUID   NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS design_system_requests_project
    ON design_system_requests (project_id, created_at DESC) WHERE project_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS design_system_requests_repository
    ON design_system_requests (repository_id, created_at DESC) WHERE repository_id IS NOT NULL;

-- Which project's base a repository builds on when it belongs to more than
-- one project that has a design system. No row: the only linked project with
-- an approved base, if there is exactly one.
CREATE TABLE IF NOT EXISTS repository_design_settings (
    repository_id UUID        PRIMARY KEY REFERENCES repositories(id) ON DELETE CASCADE,
    project_id    UUID        REFERENCES projects(id) ON DELETE SET NULL,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
