ALTER TABLE task_criterion_checks ADD COLUMN IF NOT EXISTS verified_sha TEXT NOT NULL DEFAULT '';
