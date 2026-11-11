-- The PM UAT step is gone. QA's pass now hands a task straight to human_uat,
-- which is where the stage's own exit always led.
--
-- Every statement is idempotent and none assumes the step was ever on: an
-- install that had switched the (short-lived) PM UAT setting off, a board that
-- already lost the column, and a second run all end in the same state.
--
-- History stays as written: board_events, session_actions, score events and
-- closed task_column_spans keep their pm_uat text, which only describes what
-- happened.

-- 1. Tasks sitting in pm_uat (or parked from it) move to human_uat. A queued
--    run for the PM on such a task is cancelled, not failed: nothing went wrong
--    and failed runs feed the reconciler's retry counting. The open span for the
--    column is renamed rather than reopened, so the time already spent counts and
--    the human gate keeps its "no agent" shape.
WITH moved AS (
    UPDATE board_tasks
    SET board_column = 'human_uat', updated_at = now()
    WHERE board_column = 'pm_uat'
    RETURNING id
), spans AS (
    UPDATE task_column_spans
    SET board_column = 'human_uat', agent_id = NULL
    WHERE board_column = 'pm_uat' AND left_at IS NULL
    RETURNING id
)
UPDATE task_agent_runs
SET status = 'cancelled',
    summary = CASE WHEN summary = '' THEN 'the PM UAT step was removed; the task moved to human_uat' ELSE summary END,
    updated_at = now()
WHERE status = 'pending' AND task_id IN (SELECT id FROM moved);

UPDATE board_tasks SET blocked_origin_column = 'human_uat' WHERE blocked_origin_column = 'pm_uat';

-- 2. Workflow stages. human_uat inherits what pm_uat did on entry (the PR must
--    exist by the time a task reaches it), the QA sweeps and any other stage
--    parameter that named pm_uat now name human_uat, and the behaviours that
--    only the step used go from every stage.
UPDATE workflow_stages h
SET behaviours = h.behaviours || '[{"key":"ensure_pr_on_enter"}]'::jsonb
WHERE h.column_slug = 'human_uat'
  AND NOT (h.behaviours @> '[{"key":"ensure_pr_on_enter"}]'::jsonb)
  AND EXISTS (
      SELECT 1 FROM workflow_stages p
      WHERE p.task_type = h.task_type
        AND p.column_slug = 'pm_uat'
        AND p.behaviours @> '[{"key":"ensure_pr_on_enter"}]'::jsonb
  );

UPDATE workflow_stages
SET behaviours = replace(behaviours::text, '"pm_uat"', '"human_uat"')::jsonb
WHERE column_slug <> 'pm_uat' AND behaviours::text LIKE '%"pm_uat"%';

UPDATE workflow_stages
SET instructions = replace(instructions, 'pm_uat', 'human_uat')
WHERE column_slug <> 'pm_uat' AND instructions LIKE '%pm_uat%';

UPDATE workflow_stages ws
SET behaviours = COALESCE((
        SELECT jsonb_agg(b ORDER BY ord)
        FROM jsonb_array_elements(ws.behaviours) WITH ORDINALITY AS e(b, ord)
        WHERE b->>'key' <> 'require_product_check'
          AND NOT (b->>'key' = 'criterion_verdict' AND b->'params'->>'channel' = 'pm')
    ), '[]'::jsonb)
WHERE EXISTS (
    SELECT 1 FROM jsonb_array_elements(ws.behaviours) b
    WHERE b->>'key' = 'require_product_check'
       OR (b->>'key' = 'criterion_verdict' AND b->'params'->>'channel' = 'pm')
);

DELETE FROM workflow_stages WHERE column_slug = 'pm_uat';

-- 3. Transition rules. A hand-drawn graph that let a column reach pm_uat keeps
--    that column's way forward by gaining the edge to human_uat; every edge
--    naming pm_uat then goes.
DO $$
DECLARE
    r record;
BEGIN
    FOR r IN SELECT ctid FROM board_column_transitions WHERE to_slug = 'pm_uat' AND from_slug <> 'human_uat' LOOP
        BEGIN
            UPDATE board_column_transitions SET to_slug = 'human_uat' WHERE ctid = r.ctid;
        EXCEPTION WHEN unique_violation THEN
            NULL;
        END;
    END LOOP;
END $$;

DELETE FROM board_column_transitions WHERE from_slug = 'pm_uat' OR to_slug = 'pm_uat';

-- 4. The column and everything keyed by its slug. Subscriptions and column
--    prompts name the slug without a foreign key (ReplaceColumns deletes and
--    reinserts the columns), so they are removed explicitly.
DELETE FROM board_columns WHERE slug = 'pm_uat';
DELETE FROM agent_column_subscriptions WHERE column_slug = 'pm_uat';
DELETE FROM agent_column_instructions WHERE column_slug = 'pm_uat';

UPDATE agent_templates
SET subscriptions = COALESCE((
        SELECT jsonb_agg(s) FROM jsonb_array_elements(subscriptions) s WHERE s <> '"pm_uat"'::jsonb
    ), '[]'::jsonb)
WHERE subscriptions @> '["pm_uat"]'::jsonb;

-- 5. The PM's "Clean UAT time" KPI measured time in the column; its results go
--    with it (ON DELETE CASCADE), and a template stops carrying it.
DELETE FROM agent_kpis WHERE metric_key = 'clean_time_pm_uat';

UPDATE agent_templates
SET kpis = COALESCE((
        SELECT jsonb_agg(k) FROM jsonb_array_elements(kpis) k WHERE k->>'metric_key' IS DISTINCT FROM 'clean_time_pm_uat'
    ), '[]'::jsonb)
WHERE EXISTS (SELECT 1 FROM jsonb_array_elements(kpis) k WHERE k->>'metric_key' = 'clean_time_pm_uat');

-- 6. The on/off setting that existed for a short while.
DELETE FROM app_settings WHERE key = 'pm_uat_enabled';
