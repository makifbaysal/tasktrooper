-- The "get this project ready" checklist derives every step's status from live
-- data; this holds only what a person chose (skip a step, hide the card) and
-- the board task a step opened. The card's own hide choice is the row whose
-- step_id is '_card'.
CREATE TABLE IF NOT EXISTS repository_readiness_steps (
    repository_id UUID        NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    step_id       TEXT        NOT NULL,
    skipped       BOOLEAN     NOT NULL DEFAULT FALSE,
    task_id       UUID        REFERENCES board_tasks(id) ON DELETE SET NULL,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (repository_id, step_id)
);
