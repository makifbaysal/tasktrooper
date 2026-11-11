-- A CLI run whose agent names no model runs on the provider default (or the
-- CLI's own). The row now records the real model name when it is known, and
-- this flag says it was not chosen per agent, so the usage page can show
-- "<model> (Default model)" instead of an opaque "Default Model".
ALTER TABLE llm_usage ADD COLUMN IF NOT EXISTS model_from_default BOOLEAN NOT NULL DEFAULT false;
