-- Store.DeleteTombstone: the object is confirmed gone; remove its tombstone. Called by
-- AfterCommit of Delete / DeleteByRecord (outside any transaction) and by Reap. Idempotent.
-- Use Exec (or QueryRow on the count below).
-- Args: $1 id uuid.
-- Command tag: DELETE 1, or DELETE 0 when it was already removed (not an error).
-- Served by file_tombstone_pkey. Fails 25006 (db.ErrReadOnly) on a read-only copy.
-- Constraint errors: none reachable.
DELETE FROM core.file_tombstone
WHERE id = $1::uuid
