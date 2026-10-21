-- Store.BumpTombstone (files.Reap): a delete of the object failed; count the attempt and
-- leave the tombstone for the next run. Never reset. The error itself is logged by Reap,
-- not stored. Use Exec.
-- Args: $1 id uuid.
-- Command tag: UPDATE 1, or UPDATE 0 when the tombstone is gone (not an error).
-- Served by file_tombstone_pkey. Fails 25006 (db.ErrReadOnly) on a read-only copy.
-- Constraint errors: none reachable (attempts is an int; 2^31 failures take 245,000 years of
-- hourly runs).
UPDATE core.file_tombstone
SET attempts = attempts + 1
WHERE id = $1::uuid
