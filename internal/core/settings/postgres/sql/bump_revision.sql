-- Store.BumpRevision: the LAST statement of a Set or SetTerms write transaction that changed
-- at least one row (an idempotent call that changed nothing does not bump, settings.md
-- "Concurrency"). Use QueryRow, inside the Write transaction, after lock_revision.sql.
-- Args: none.
-- Columns, in order: revision bigint (the new revision, which the writer installs with its
-- snapshot). No row (pgx.ErrNoRows) is F26: re-kind to errs.Internal.
UPDATE core.setting_revision
SET revision = revision + 1
WHERE id = 1
RETURNING revision
