-- Per-agent control of the Co-authored-by trailers on its commits. Empty text
-- and false keep the built-in trailers, so every existing agent behaves as before.
ALTER TABLE agents ADD COLUMN IF NOT EXISTS commit_coauthor TEXT NOT NULL DEFAULT '';
ALTER TABLE agents ADD COLUMN IF NOT EXISTS commit_coauthor_disabled BOOLEAN NOT NULL DEFAULT false;
