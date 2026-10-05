-- The board task the "no CI workflows" notice opened to author this repo's
-- GitHub Actions workflows. Remembered so the notice can say the task is
-- already open instead of opening a duplicate on every click; a deleted task,
-- or one that reached done/released, frees it again. '' = none recorded.
ALTER TABLE repositories
    ADD COLUMN IF NOT EXISTS ci_setup_task_id TEXT NOT NULL DEFAULT '';
