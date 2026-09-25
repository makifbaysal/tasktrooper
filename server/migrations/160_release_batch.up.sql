-- Batch releases: a desktop or mobile component's merges collect into a draft
-- a human cuts; the cut release is built by a tag-triggered workflow, a
-- command on this machine, or the store pipeline.
ALTER TABLE releases
    ADD COLUMN IF NOT EXISTS local_run    JSONB,
    ADD COLUMN IF NOT EXISTS store_builds JSONB NOT NULL DEFAULT '[]',
    ADD COLUMN IF NOT EXISTS cut_at       TIMESTAMPTZ;
