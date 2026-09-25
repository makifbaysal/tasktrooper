-- A task's before_deploy steps (a migration, a secret, a manual switch) are
-- performed by a human; the release engineer may not ship the task until a
-- human confirms them.
ALTER TABLE board_tasks
    ADD COLUMN IF NOT EXISTS before_deploy_confirmed_at TIMESTAMPTZ;
