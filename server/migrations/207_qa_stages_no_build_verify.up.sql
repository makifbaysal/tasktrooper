-- QA tests the running product; it does not build it. build_verify on
-- ready_for_qa/in_qa re-ran the build/vet/test gate after every QA round and
-- handed failures back to QA as "fix these errors" — to an agent with no write
-- tools, after code review had already passed that same gate. The rounds went
-- to compiling instead of executing the case matrix.
UPDATE workflow_stages
SET behaviours = COALESCE(
    (SELECT jsonb_agg(elem) FROM jsonb_array_elements(behaviours) elem
     WHERE elem->>'key' <> 'build_verify'),
    '[]'::jsonb
)
WHERE column_slug IN ('ready_for_qa', 'in_qa')
  AND behaviours @> '[{"key":"build_verify"}]';
