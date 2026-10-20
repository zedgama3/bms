-- Store.Revision: the poll in Run and the first step of a snapshot load. Read-only; use
-- QueryRow. Plain read, no lock; works on a read-only copy.
-- Args: none.
-- Columns, in order: revision bigint. No row (pgx.ErrNoRows) means the row is missing: the
-- caller re-kinds it to errs.Internal (F26, "run bms migrate").
-- Load order: read the revision BEFORE load_settings.sql and load_terminology.sql. The
-- database runs read committed, so rows read afterwards are at least as new as the revision;
-- the worst case is one extra reload on the next poll, never a missed change.
SELECT revision
FROM core.setting_revision
WHERE id = 1
