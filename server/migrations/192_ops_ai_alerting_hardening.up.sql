-- a. AI jobs: when a job took a slot and began its chat turn (NULL while it
--    waits for one), and which process created it, so a restart fails only
--    the jobs a process before it left running. At most one job of a kind
--    runs per repository, component and environment: the unique index makes
--    starting one race-free. No process runs while migrations do, so every
--    job still marked running was interrupted.
ALTER TABLE ai_jobs
    ADD COLUMN IF NOT EXISTS started_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS owner      TEXT;

UPDATE ai_jobs SET status = 'failed', error = 'interrupted by a restart', finished_at = now(), updated_at = now()
WHERE status = 'running';

DROP INDEX IF EXISTS idx_ai_jobs_running;
CREATE UNIQUE INDEX IF NOT EXISTS uq_ai_jobs_running ON ai_jobs
    (kind, repository_id, COALESCE(component_id, '00000000-0000-0000-0000-000000000000'::uuid), environment)
    WHERE status = 'running';

-- b. Error group links: claimed_at is set while the task action opens the
--    group's task and cleared once the task is linked. A claim still open
--    long after (the process died in between) may be claimed again.
ALTER TABLE error_group_links
    ADD COLUMN IF NOT EXISTS claimed_at TIMESTAMPTZ;

-- c. Error definition suggestions: the same match for the same component is
--    suggested once, however many proposals race. Older duplicates are
--    dropped first; they were never reviewed.
DELETE FROM error_definitions d
USING error_definitions keep
WHERE d.status = 'suggested' AND keep.status = 'suggested'
  AND d.repository_id = keep.repository_id
  AND d.component_id IS NOT DISTINCT FROM keep.component_id
  AND d.match = keep.match
  AND (d.created_at, d.id) > (keep.created_at, keep.id);

CREATE UNIQUE INDEX IF NOT EXISTS uq_error_definitions_suggested ON error_definitions
    (repository_id, COALESCE(component_id, '00000000-0000-0000-0000-000000000000'::uuid), md5(match::text))
    WHERE status = 'suggested';

-- d. Alert rules: evaluations in a row whose read failed for a reason that
--    says nothing about the value (the source down or slow), and the last
--    such failure. Past a limit the rule stops holding its state and reads
--    as no data. A successful read zeroes both.
ALTER TABLE alert_rules
    ADD COLUMN IF NOT EXISTS error_count INT  NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS last_error  TEXT NOT NULL DEFAULT '';

-- e. A rollback proposal a person dismissed: when, and the incident it
--    answered. The release engineer may not propose it again until another
--    incident is attributed to the release. Written only by its own
--    statements, like rollback_proposal.
ALTER TABLE releases
    ADD COLUMN IF NOT EXISTS rollback_dismissal JSONB;
