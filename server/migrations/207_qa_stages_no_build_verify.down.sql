UPDATE workflow_stages
SET behaviours = behaviours || '[{"key":"build_verify"}]'::jsonb
WHERE column_slug IN ('ready_for_qa', 'in_qa')
  AND NOT behaviours @> '[{"key":"build_verify"}]';
