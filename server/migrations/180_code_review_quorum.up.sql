-- code_review had exactly one reviewer, and its verdict lived on the column
-- span (task_column_spans.review_verdict, one value per visit). A second,
-- security-only reviewer needs a verdict per reviewer: the card leaves
-- code_review once every subscribed reviewer has decided, and the task shows
-- who approved and who asked for changes.
--
-- A verdict hangs off the span it was given in. A park to blocked interrupts a
-- review without changing the code under it, so the application reads the
-- spans of one review round together rather than only the open one.

CREATE TABLE IF NOT EXISTS task_review_verdicts (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id     UUID NOT NULL REFERENCES board_tasks(id) ON DELETE CASCADE,
    span_id     UUID NOT NULL REFERENCES task_column_spans(id) ON DELETE CASCADE,
    agent_id    UUID REFERENCES agents(id) ON DELETE SET NULL,
    -- Kept so a deleted reviewer's verdict still says whose it was.
    agent_name  TEXT NOT NULL DEFAULT '',
    verdict     TEXT NOT NULL CHECK (verdict IN ('approve', 'reject')),
    decided_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (span_id, agent_id)
);

CREATE INDEX IF NOT EXISTS idx_task_review_verdicts_task
    ON task_review_verdicts(task_id, decided_at);

-- The security-agent itself is created by the catalog sync at boot
-- (catalog/agents/security-agent), which assigns this role and subscribes it
-- to code_review next to the system architect.
INSERT INTO roles (key, name, description, required_tools) VALUES
    ('security', 'Security Reviewer', 'Reviews every pull request in code_review for security issues and blocks the ones that introduce an exploitable vulnerability.', '{}')
ON CONFLICT (key) DO NOTHING;
