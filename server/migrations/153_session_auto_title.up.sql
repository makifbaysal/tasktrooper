-- Agent chats (agent_id set, task_id null) open with a client-side placeholder
-- title ("chat with X") that never changes. auto_titled marks whether that
-- placeholder has already been replaced by a generated one, so the generator
-- runs at most once per session and a later manual rename (not built yet)
-- would have somewhere to record "don't touch this again".
--
-- Every session that already exists is marked auto_titled=true so this
-- migration cannot rewrite a title a user has been looking at for months —
-- only sessions created after this migration are eligible for generation.
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS auto_titled BOOLEAN NOT NULL DEFAULT false;
UPDATE sessions SET auto_titled = true WHERE NOT auto_titled;
