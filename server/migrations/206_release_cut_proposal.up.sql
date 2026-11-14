-- A cut the release engineer proposed for a draft batch release because a
-- person asked for one in a chat: a version and notes, nothing shipped. A
-- human reviews it and cuts, or dismisses it. Written only by its own
-- statement, never by the general release update, which clears it as soon as
-- the release leaves draft.
ALTER TABLE releases
    ADD COLUMN IF NOT EXISTS cut_proposal JSONB;
