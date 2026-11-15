ALTER TABLE repositories
    DROP COLUMN IF EXISTS automation_task_id,
    DROP COLUMN IF EXISTS automation_tests;
