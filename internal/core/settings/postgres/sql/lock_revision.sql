-- Store.LockRevision: the FIRST statement of every Set and SetTerms write transaction.
-- Use QueryRow, inside the Write transaction. Takes the revision row's lock, held to commit, so
-- writers run one at a time: no two transactions can upsert the same keys in opposite orders
-- (no deadlock, no 40P01), and the revision bump at the end orders snapshots. After this call,
-- re-read core.setting with load_settings.sql inside the transaction when the area Checks
-- need the stored values (they cannot change underneath).
-- Args: none.
-- Columns, in order: revision bigint (the revision before this write). No row
-- (pgx.ErrNoRows) is F26: re-kind to errs.Internal.
SELECT revision
FROM core.setting_revision
WHERE id = 1
FOR UPDATE
