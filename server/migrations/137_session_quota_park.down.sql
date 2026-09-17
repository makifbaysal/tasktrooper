DROP INDEX IF EXISTS idx_sessions_quota_resume;

ALTER TABLE sessions
    DROP COLUMN IF EXISTS quota_resume_at,
    DROP COLUMN IF EXISTS quota_pending_request,
    DROP COLUMN IF EXISTS quota_pending_policy;
