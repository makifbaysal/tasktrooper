-- The board a new install starts with.
--
-- This is what migrations 010/018/041/042/049/051/054/056/093 used to INSERT
-- at migration time. It runs once per install, gated by
-- install_state.board_seeded_at, in the same transaction that sets it.
--
-- ON CONFLICT DO NOTHING throughout, so a row that already exists is kept
-- rather than failing the seed.

-- The default column template. Order matters only through `position`; the
-- slugs are what board_tasks.board_column is validated against and what agent
-- subscriptions name, so they are the part that must not drift.
INSERT INTO board_columns (slug, label, position, is_backlog) VALUES
    ('backlog',       'Backlog',        0, true),
    ('todo',          'Todo',           1, false),
    ('in_progress',   'In Progress',    2, false),
    ('analiz_review', 'Analiz Review',  3, false),
    ('code_review',   'Code Review',    4, false),
    ('ready_for_qa',  'Ready for QA',   5, false),
    ('in_qa',         'In QA',          6, false),
    ('need_revision', 'Need Revision',  7, false),
    ('pm_uat',        'PM UAT',         8, false),
    ('human_uat',     'Human UAT',      9, false),
    ('blocked',       'Blocked',       10, false),
    ('done',          'Done',          11, false),
    ('released',      'Released',      12, false)
ON CONFLICT DO NOTHING;

-- The recommended transition rules (domain.DefaultBoardTransitions; the same
-- set migration 172 gives a rule-less install). Only onto an empty table: a
-- graph someone already drew is theirs.
INSERT INTO board_column_transitions (from_slug, to_slug)
SELECT v.from_slug, v.to_slug FROM (VALUES
    ('backlog', 'todo'),
    ('todo', 'in_progress'),
    ('in_progress', 'code_review'),
    ('in_progress', 'analiz_review'),
    ('in_progress', 'need_revision'),
    ('in_progress', 'human_uat'),
    ('analiz_review', 'done'),
    ('analiz_review', 'need_revision'),
    ('code_review', 'ready_for_qa'),
    ('code_review', 'need_revision'),
    ('ready_for_qa', 'in_qa'),
    ('ready_for_qa', 'need_revision'),
    ('in_qa', 'pm_uat'),
    ('in_qa', 'human_uat'),
    ('in_qa', 'need_revision'),
    ('need_revision', 'in_progress'),
    ('need_revision', 'code_review'),
    ('need_revision', 'analiz_review'),
    ('pm_uat', 'human_uat'),
    ('pm_uat', 'need_revision'),
    ('human_uat', 'done'),
    ('human_uat', 'need_revision'),
    ('done', 'released'),
    ('done', 'need_revision'),
    ('released', 'need_revision'),
    ('todo', 'need_revision'),
    ('in_progress', 'todo'),
    ('ready_for_qa', 'pm_uat'),
    ('ready_for_qa', 'human_uat'),
    ('todo', 'blocked'),
    ('in_progress', 'blocked'),
    ('analiz_review', 'blocked'),
    ('code_review', 'blocked'),
    ('ready_for_qa', 'blocked'),
    ('in_qa', 'blocked'),
    ('need_revision', 'blocked'),
    ('pm_uat', 'blocked'),
    ('human_uat', 'blocked'),
    ('done', 'blocked')
) AS v(from_slug, to_slug)
WHERE NOT EXISTS (SELECT 1 FROM board_column_transitions)
ON CONFLICT DO NOTHING;

-- One counter per task type. They exist up front so the first task of each
-- type takes number 1 rather than racing to create its own row.
INSERT INTO board_task_counters (task_type, last_number) VALUES
    ('task', 0), ('bug', 0), ('analiz', 0)
ON CONFLICT DO NOTHING;

-- The single-row tables, CHECK (id = 1).
INSERT INTO board_settings (id) VALUES (1) ON CONFLICT DO NOTHING;
INSERT INTO billing_plan (id) VALUES (1) ON CONFLICT DO NOTHING;

-- Settings that need a value before anything can be configured. workspace_root
-- is a path on this host; the host-root translation in the stores makes a
-- foreign value harmless (see .ai/workspace.md).
INSERT INTO app_settings (key, value) VALUES
    ('workspace_root',      './data/workspaces'),
    ('default_language',    'tr'),
    ('active_llm_provider', 'local')
ON CONFLICT DO NOTHING;

-- The provider catalog, all unconfigured: the row is the offer, `configured`
-- is whether a key has been supplied.
INSERT INTO llm_provider_configs (provider_type, base_url, default_model) VALUES
    ('local',     'http://127.0.0.1:1234/v1',                                  ''),
    ('openai',    'https://api.openai.com/v1',                                 'gpt-4o'),
    ('anthropic', 'https://api.anthropic.com/v1',                              'claude-sonnet-4-6'),
    ('gemini',    'https://generativelanguage.googleapis.com/v1beta/openai/',  'gemini-2.0-flash'),
    ('groq',      'https://api.groq.com/openai/v1',                            'llama-3.3-70b-versatile')
ON CONFLICT DO NOTHING;

-- Token prices. The admin billing routes can edit them; these are the defaults.
INSERT INTO model_prices (model, usd_per_1m_prompt, usd_per_1m_completion, usd_per_1m_cache_read, usd_per_1m_cache_write) VALUES
    ('claude-opus-5',     5,  25, 0.5,  6.25),
    ('claude-sonnet-5',   3,  15, 0.3,  3.75),
    ('claude-fable-5',   10,  50, 1.0, 12.50),
    ('claude-opus-4-8',   5,  25, 0.5,  6.25),
    ('claude-opus-4-7',   5,  25, 0.5,  6.25),
    ('claude-opus-4-6',   5,  25, 0.5,  6.25),
    ('claude-sonnet-4-6', 3,  15, 0.3,  3.75),
    ('claude-haiku-4-5',  1,   5, 0.1,  1.25)
ON CONFLICT DO NOTHING;
