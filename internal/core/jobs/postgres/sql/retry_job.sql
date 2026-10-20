-- Store.Retry: an admin puts a Failed row back to Queued: attempts 0, due now, error and
-- finish cleared (started_at and instance are kept as history). Use Query (RETURNING) inside
-- the Write transaction that also records jobs.job.retried (Reason = the returned name).
-- Args: $1 id uuid, $2 now timestamptz (run_at and updated_at).
-- Columns, in order: id uuid, name text. ZERO rows: no such row (NotFound) or the row is not
-- failed (Conflict, code status); ask get_job_status.sql which.
-- Note: nothing here checks for another open row of the same name, so a Retry of a failed
-- scheduled slot can run beside a later slot (jobs.md specifies only the status test; the
-- handler is idempotent).
-- Fails 25006 (db.ErrReadOnly) on a read-only copy. Constraint errors: none reachable.
UPDATE core.job
SET status = 'queued',
    attempts = 0,
    run_at = $2::timestamptz,
    finished_at = NULL,
    last_error = NULL,
    updated_at = $2::timestamptz
WHERE id = $1::uuid
  AND status = 'failed'
RETURNING id, name
