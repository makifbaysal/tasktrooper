-- Study kinds: a study is one of four types (user_interviews is the original
-- purchase-intent interview, so every existing row keeps its meaning).
-- inputs is what the kind needs besides the goal: the visitor's intent and the
-- page (landing_page_test), the candidate lines (message_testing) or the two
-- pages (ab_test). script is the interview script (grouped questions) written
-- before the study runs; script_state: none (no script, legacy), generating,
-- awaiting_approval (written, not yet approved by the person), approved,
-- failed. script_version counts every change of the script (written, edited,
-- uploaded, replaced); an approval records the version it approved, who gave
-- it and when, and a study only runs on a script whose current version is the
-- approved one. script_calls counts the model calls spent writing it.
ALTER TABLE audience_studies
    ADD COLUMN IF NOT EXISTS kind         TEXT    NOT NULL DEFAULT 'user_interviews'
        CHECK (kind IN ('user_interviews', 'landing_page_test', 'message_testing', 'ab_test')),
    ADD COLUMN IF NOT EXISTS inputs       JSONB   NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS script       JSONB,
    ADD COLUMN IF NOT EXISTS script_state TEXT    NOT NULL DEFAULT 'none'
        CHECK (script_state IN ('none', 'generating', 'awaiting_approval', 'approved', 'failed')),
    ADD COLUMN IF NOT EXISTS script_calls INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS script_version INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS script_approved_version INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS script_approved_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS script_approved_by TEXT NOT NULL DEFAULT '';

-- A study whose interviews are over waits for the report to be asked for.
ALTER TABLE audience_studies DROP CONSTRAINT IF EXISTS audience_studies_status_check;
ALTER TABLE audience_studies ADD CONSTRAINT audience_studies_status_check
    CHECK (status IN ('draft', 'running', 'interviewed', 'synthesizing', 'done', 'failed', 'cancelled'));
