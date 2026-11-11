-- How a component is containerised: the "has containers" switch, the image
-- registry, the platforms to build for, the tag pattern and whether agents must
-- run its tooling inside its containers. Human-set only; a scan never writes it.
ALTER TABLE project_components ADD COLUMN IF NOT EXISTS container JSONB NOT NULL DEFAULT '{}';
