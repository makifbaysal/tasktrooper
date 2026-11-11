-- What a reviewer reported, kept per report so a rework loop can be read back
-- as a story. A row is one finding a reviewer raised in one round (kind
-- 'finding'), one place the reviewer wants a person to look (kind
-- 'human_review'), what it could not verify ('not_reviewed') or the aspects it covered ('aspect'), or a QA verdict on one acceptance criterion
-- (task_criterion_checks keeps only the latest verdict per criterion, so the
-- failures of earlier rounds are written here as they happen). Append-only:
-- whether a finding is still open or was fixed is read from the rounds that
-- followed, never stored.
CREATE TABLE IF NOT EXISTS task_review_findings (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id             UUID NOT NULL,
    kind                TEXT NOT NULL DEFAULT 'finding' CHECK (kind IN ('finding', 'human_review', 'not_reviewed', 'aspect')),
    -- The column the task was in when the reviewer reported (code_review, in_qa).
    gate                TEXT NOT NULL DEFAULT '',
    agent_id            UUID,
    agent_name          TEXT NOT NULL DEFAULT '',
    head_sha            TEXT NOT NULL DEFAULT '',
    -- Identity across rounds: the reviewer's own key when it gave one, else
    -- a hash of file, category and normalised message.
    fingerprint         TEXT NOT NULL,
    file                TEXT NOT NULL DEFAULT '',
    line                INT NOT NULL DEFAULT 0,
    severity            TEXT NOT NULL DEFAULT '',
    category            TEXT NOT NULL DEFAULT '',
    message             TEXT NOT NULL DEFAULT '',
    confidence          TEXT NOT NULL DEFAULT '',
    human_review_reason TEXT NOT NULL DEFAULT '',
    question            TEXT NOT NULL DEFAULT '',
    -- not_reviewed items: what the reviewer could not verify, and why.
    scope               TEXT NOT NULL DEFAULT '',
    reason              TEXT NOT NULL DEFAULT '',
    -- open: the reviewer reports it; withdrawn: the reviewer takes it back;
    -- passed: a QA approval of the criterion this fingerprint names.
    status              TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'withdrawn', 'passed')),
    criterion_id        UUID,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_task_review_findings_task
    ON task_review_findings (task_id, created_at);

-- A person's "I looked at this" on one flag. flag_key names the flag, not a
-- row: automatic flags are derived on every read, so there is nothing to hang
-- the state on but the key.
CREATE TABLE IF NOT EXISTS task_review_flag_acks (
    task_id     UUID NOT NULL,
    flag_key    TEXT NOT NULL,
    reviewed_by TEXT NOT NULL DEFAULT '',
    reviewed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (task_id, flag_key)
);

-- Company edition: applies in either order relative to c005
-- (.ai/shared-tenancy.md, "Upstream migrations after c005"), like 201.
DO $$
BEGIN
    IF to_regprocedure('tenancy_apply()') IS NULL THEN
        ALTER TABLE task_review_findings ADD CONSTRAINT task_review_findings_task_id_fkey
            FOREIGN KEY (task_id) REFERENCES board_tasks(id) ON DELETE CASCADE;
        ALTER TABLE task_review_flag_acks ADD CONSTRAINT task_review_flag_acks_task_id_fkey
            FOREIGN KEY (task_id) REFERENCES board_tasks(id) ON DELETE CASCADE;
    ELSE
        PERFORM tenancy_apply();
        ALTER TABLE task_review_findings ADD CONSTRAINT task_review_findings_task_id_fkey
            FOREIGN KEY (tenant_id, task_id) REFERENCES board_tasks(tenant_id, id) ON DELETE CASCADE;
        ALTER TABLE task_review_flag_acks ADD CONSTRAINT task_review_flag_acks_task_id_fkey
            FOREIGN KEY (tenant_id, task_id) REFERENCES board_tasks(tenant_id, id) ON DELETE CASCADE;
    END IF;
END $$;
