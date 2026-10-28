-- Store.Complete: the handler returned nil (also a dropped signals.redeliver). Fenced by
-- the lease. Clears last_error (an earlier retry's token is not the outcome). Use Query
-- (RETURNING); standalone, on a ctx without the run's cancellation (recordTimeout).
-- Args: $1 id uuid, $2 leased_until timestamptz (the fence lease_jobs.sql / heartbeat_job.sql
-- last returned), $3 now timestamptz (finished_at and updated_at).
-- J1 (jobs.md decision 23): when a request was held on the row (next_run_at set by
-- enqueue_job.sql while it ran), the row does not end done: it becomes queued at next_run_at
-- with next_args, attempts 0, started_at, finished_at and last_error NULL, next_* cleared.
-- A success resets attempts to 0 whoever made the held request (D52 a).
-- Columns, in order: id uuid, status text ('done', or 'queued' when a held request was
-- applied), next boolean (true when a held request was applied: the runner logs next=true).
-- ZERO rows: the lease was lost (re-leased by another process or reclaimed): the row is not
-- ours; write nothing more, outcome lost_lease.
-- Fails 25006 (db.ErrReadOnly) on a read-only copy (the lease then lapses on its own).
-- Constraint errors: none reachable.
WITH cur AS (
    SELECT id, next_run_at IS NOT NULL AS has_next
    FROM core.job
    WHERE id = $1::uuid
      AND status = 'running'
      AND leased_until = $2::timestamptz
    FOR UPDATE
)
UPDATE core.job AS j
SET status = CASE WHEN cur.has_next THEN 'queued' ELSE 'done' END,
    run_at = coalesce(j.next_run_at, j.run_at),
    args = coalesce(j.next_args, j.args),
    attempts = CASE WHEN cur.has_next THEN 0 ELSE j.attempts END,
    started_at = CASE WHEN cur.has_next THEN NULL ELSE j.started_at END,
    finished_at = CASE WHEN cur.has_next THEN NULL ELSE $3::timestamptz END,
    next_run_at = NULL,
    next_args = NULL,
    next_by_run = false,
    leased_until = NULL,
    last_error = NULL,
    updated_at = $3::timestamptz
FROM cur
WHERE j.id = cur.id
RETURNING j.id, j.status, cur.has_next AS next
