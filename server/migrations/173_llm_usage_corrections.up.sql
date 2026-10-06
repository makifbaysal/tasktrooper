-- Two corrections to llm_usage history; both are idempotent.
--
-- 1. opencode reports tokens.input EXCLUDING both cache buckets (its own
--    tokens.total is input + output + reasoning + cache.read + cache.write),
--    and its adapter stored that input as prompt_tokens as-is, breaking the
--    contract that the cache columns are subsets of prompt_tokens — the usage
--    page showed cache hit rates far above 100%. The adapter now folds the
--    cache in, so only rows that still break the contract are old ones, and
--    only those are touched: a row already written by the fixed adapter (or
--    already corrected) has cache <= prompt and is left alone. An old row
--    whose cache happened to stay below its input cannot be told apart and
--    keeps its smaller prompt. Reasoning tokens were dropped and are gone.
UPDATE llm_usage
SET prompt_tokens = prompt_tokens + cache_read_tokens + cache_write_tokens
WHERE kind = 'cli'
  AND provider = 'opencode'
  AND cache_read_tokens + cache_write_tokens > prompt_tokens;

-- 2. 168 backfilled pre-ledger Claude Code runs under the '(unrecorded)'
--    model, but the model each session reported was already stored on its
--    claude_code_session trace step. 168 stamped each row with its run's
--    updated_at and copied its token counters, so a row is named only when
--    that identifies exactly one run and every session step of that run
--    reports the same model. A run that switched models keeps '(unrecorded)':
--    its total cannot be split between them.
WITH matched AS (
    SELECT u.id AS usage_id, (array_agg(r.session_run_id))[1] AS session_run_id
    FROM llm_usage u
    JOIN task_agent_runs r
      ON r.updated_at = u.created_at
     AND r.prompt_tokens = u.prompt_tokens
     AND r.completion_tokens = u.completion_tokens
     AND r.cache_read_tokens = u.cache_read_tokens
     AND r.cache_write_tokens = u.cache_write_tokens
    WHERE u.kind = 'cli' AND u.provider = 'claude_code' AND u.model = '(unrecorded)'
    GROUP BY u.id
    HAVING COUNT(*) = 1
),
named AS (
    SELECT m.usage_id, MIN(COALESCE(st.payload->>'model', '')) AS model
    FROM matched m
    JOIN session_steps st ON st.run_id = m.session_run_id AND st.step_type = 'claude_code_session'
    GROUP BY m.usage_id
    HAVING COUNT(DISTINCT COALESCE(st.payload->>'model', '')) = 1
)
UPDATE llm_usage u
SET model = named.model
FROM named
WHERE u.id = named.usage_id AND named.model <> '';
