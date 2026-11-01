-- Store purge (Service.Purge). Run inside the Write transaction that also records audit.purged.
-- Args: $1 before timestamptz. One row, one column: deleted bigint (scan into int64): the
-- number of entries with at < before that core.audit_purge removed. The guard flag is set
-- inside the function only. Fails 25006 (db.ErrReadOnly) on a read-only copy.
SELECT core.audit_purge($1::timestamptz) AS deleted
