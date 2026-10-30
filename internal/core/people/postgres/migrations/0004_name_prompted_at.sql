-- people 0004: the first-sign-in prompt is shown once per session and the server records it
-- (people.md Amendment P-W1, decisions 34 to 36, D58). Adds core.person.name_prompted_at:
-- when the person was last asked for a playa name. The prompt is due when playa_name is ''
-- and the column is NULL or before the session's issue time; Me writes
-- GREATEST(now, issued) when it prompts, so each session is prompted at most once.
--
-- Safe on a live database with data: a nullable column with no default is a catalog change
-- (no table rewrite), every existing row gets NULL (never prompted), and nothing is
-- backfilled.
ALTER TABLE core.person ADD COLUMN name_prompted_at timestamptz NULL;
COMMENT ON COLUMN core.person.name_prompted_at IS 'sensitivity: internal';
