-- Board transition rules shipped as a default instead of "anything goes".
-- Must match domain.DefaultBoardTransitions / RequiredBoardTransitions
-- (default_board_transitions_test.go replays this file against them).
--
-- 1. A board with only the stock columns gets the recommended graph, whatever
--    it had before: earlier hand-drawn graphs routinely missed moves the
--    automation makes (code review approval, QA auto-enter, revision bounces)
--    and so stalled every task in that column.
-- 2. A board with a column of its own keeps its graph; where it is already
--    restricted it gains the required moves (the 041/042/056/140 pattern).
--    Adding a row to an unrestricted source would restrict it, so those are
--    left alone, and a rule-less custom board stays rule-less: a default that
--    left its own column unreachable would be worse than none.
CREATE TEMP TABLE default_board_transitions (from_slug TEXT, to_slug TEXT, required BOOLEAN) ON COMMIT DROP;
INSERT INTO default_board_transitions (from_slug, to_slug, required) VALUES
    ('backlog', 'todo', true),
    ('todo', 'in_progress', true),
    ('in_progress', 'code_review', true),
    ('in_progress', 'analiz_review', true),
    ('in_progress', 'need_revision', true),
    ('in_progress', 'human_uat', true),
    ('analiz_review', 'done', true),
    ('analiz_review', 'need_revision', true),
    ('code_review', 'ready_for_qa', true),
    ('code_review', 'need_revision', true),
    ('ready_for_qa', 'in_qa', true),
    ('ready_for_qa', 'need_revision', true),
    ('in_qa', 'pm_uat', true),
    ('in_qa', 'human_uat', true),
    ('in_qa', 'need_revision', true),
    ('need_revision', 'in_progress', true),
    ('need_revision', 'code_review', true),
    ('need_revision', 'analiz_review', true),
    ('pm_uat', 'human_uat', true),
    ('pm_uat', 'need_revision', true),
    ('human_uat', 'done', true),
    ('human_uat', 'need_revision', true),
    ('done', 'released', true),
    ('done', 'need_revision', true),
    ('released', 'need_revision', true),
    ('todo', 'need_revision', false),
    ('in_progress', 'todo', false),
    ('ready_for_qa', 'pm_uat', false),
    ('ready_for_qa', 'human_uat', false),
    ('todo', 'blocked', false),
    ('in_progress', 'blocked', false),
    ('analiz_review', 'blocked', false),
    ('code_review', 'blocked', false),
    ('ready_for_qa', 'blocked', false),
    ('in_qa', 'blocked', false),
    ('need_revision', 'blocked', false),
    ('pm_uat', 'blocked', false),
    ('human_uat', 'blocked', false),
    ('done', 'blocked', false);

DO $$
DECLARE
    -- No columns yet is an install bootseed has not reached: seed.sql gives
    -- it the same rules along with its board.
    seeded BOOLEAN := EXISTS (SELECT 1 FROM board_columns);
    stock_only BOOLEAN := NOT EXISTS (
        SELECT 1 FROM board_columns
        WHERE slug NOT IN (
            'backlog', 'todo', 'in_progress', 'analiz_review', 'code_review', 'ready_for_qa',
            'in_qa', 'need_revision', 'pm_uat', 'human_uat', 'blocked', 'done', 'released'
        )
    );
BEGIN
    IF NOT seeded THEN
        RETURN;
    END IF;
    IF stock_only THEN
        DELETE FROM board_column_transitions;
        INSERT INTO board_column_transitions (from_slug, to_slug)
        SELECT d.from_slug, d.to_slug
        FROM default_board_transitions d
        WHERE EXISTS (SELECT 1 FROM board_columns c WHERE c.slug = d.from_slug)
          AND EXISTS (SELECT 1 FROM board_columns c WHERE c.slug = d.to_slug)
        ON CONFLICT DO NOTHING;
    ELSE
        INSERT INTO board_column_transitions (from_slug, to_slug)
        SELECT d.from_slug, d.to_slug
        FROM default_board_transitions d
        WHERE d.required
          AND EXISTS (SELECT 1 FROM board_column_transitions t WHERE t.from_slug = d.from_slug)
          AND EXISTS (SELECT 1 FROM board_columns c WHERE c.slug = d.to_slug)
        ON CONFLICT DO NOTHING;
    END IF;
END $$;
