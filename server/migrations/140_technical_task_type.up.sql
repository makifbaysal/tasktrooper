ALTER TABLE board_tasks DROP CONSTRAINT board_tasks_task_type_check;
ALTER TABLE board_tasks ADD CONSTRAINT board_tasks_task_type_check
    CHECK (task_type IN ('task', 'analiz', 'bug', 'technical'));

-- Only installs running a restricted transition graph need the new edges; an
-- install with no rows stays fully permissive (see migrations 041/042/056 for
-- the same pattern). Lets a `technical` task leave QA straight for human_uat
-- without an operator having to edit their workflow matrix by hand.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM board_column_transitions) THEN
        INSERT INTO board_column_transitions (from_slug, to_slug) VALUES
            ('ready_for_qa', 'human_uat'),
            ('in_qa', 'human_uat')
        ON CONFLICT DO NOTHING;
    END IF;
END $$;
