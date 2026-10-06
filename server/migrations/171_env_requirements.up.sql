-- What a component's deploy target must have set before its code may ship
-- there: one row per environment variable NAME. A value is stored only for
-- the 'value' kind, which by definition is not a secret (owner/repo, a
-- branch); every secret goes straight to the provider and never lands here.
-- Declared by the developer agent that introduced the variable
-- (declare_env_vars) or classified by a human on the Deploy tab — a human row
-- wins over a later agent declaration of the same name. component_id NULL
-- applies to every component of the repository.
CREATE TABLE IF NOT EXISTS env_requirements (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    component_id  UUID REFERENCES project_components(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    kind          TEXT NOT NULL CHECK (kind IN ('value', 'generated', 'human_secret', 'human_bcrypt', 'optional')),
    value         TEXT NOT NULL DEFAULT '',
    description   TEXT NOT NULL DEFAULT '',
    source        TEXT NOT NULL DEFAULT 'agent' CHECK (source IN ('agent', 'human')),
    task_id       UUID REFERENCES board_tasks(id) ON DELETE SET NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_env_requirements_name
    ON env_requirements (repository_id, component_id, name) NULLS NOT DISTINCT;
