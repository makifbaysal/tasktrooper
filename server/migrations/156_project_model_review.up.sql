-- Review flags for rows a later scan added on its own, and the resolved
-- host/port of an unresolved link target so it can be matched later against
-- environments bound after the scan (see projectmodel.Relink).

ALTER TABLE project_components ADD COLUMN IF NOT EXISTS needs_review BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE component_checks ADD COLUMN IF NOT EXISTS needs_review BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE component_links ADD COLUMN IF NOT EXISTS target_host TEXT NOT NULL DEFAULT '';
ALTER TABLE component_links ADD COLUMN IF NOT EXISTS target_port INT NOT NULL DEFAULT 0;
