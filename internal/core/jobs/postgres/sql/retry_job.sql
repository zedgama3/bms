-- Store.Retry: an admin puts a Failed row back to Queued: attempts 0, due now, error and
-- finish cleared (started_at and instance are kept as history). Use Query (RETURNING) inside
-- the Write transaction that also records jobs.job.retried (Reason = the returned name).
-- Args: $1 id uuid, $2 now timestamptz (run_at and updated_at), $3 exclusive boolean: true for
-- a scheduled name (Every > 0), which never has two open rows (jobs.md decision 7), so the row is
-- put back only while no row of that name is queued or running.
-- Columns, in order: id uuid, name text. ZERO rows: no such row (NotFound) or the row is not
-- failed (Conflict, code status), or $3 is true and a row of the name is open (Conflict, code
-- running); ask get_job_status.sql which: a failed row means the open-row guard refused.
-- With $3 true this MUST be preceded, in the same Write transaction, by lock_job_name.sql for the
-- row's name (see its header), as for run_now_job.sql: without the lock two concurrent
-- transactions both pass the NOT EXISTS (Q-JOBS8, jobs.md amendment (b)).
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
  AND (NOT $3::boolean OR NOT EXISTS (
      SELECT 1
      FROM core.job AS open_job
      WHERE open_job.name = core.job.name
        AND open_job.status IN ('queued', 'running')
  ))
RETURNING id, name
