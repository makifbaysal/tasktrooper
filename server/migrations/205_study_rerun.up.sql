-- A study can be run again after the product changed: the new study points at
-- the one it follows (parent_study_id) and carries the person's note of what
-- changed. No foreign key on purpose: it stays valid on a schema that re-keys
-- tables, and a deleted parent just leaves the new study without a "before".
ALTER TABLE audience_studies
    ADD COLUMN IF NOT EXISTS parent_study_id UUID,
    ADD COLUMN IF NOT EXISTS changes         TEXT NOT NULL DEFAULT '';
