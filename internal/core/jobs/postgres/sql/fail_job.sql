-- Store.Fail: the run ended finally and badly: no_handler, panic, gave_up, or any errs kind
-- that is not retried. Fenced by the lease. Use Query (RETURNING); standalone, on a ctx
-- without the run's cancellation.
-- Args: $1 id uuid, $2 leased_until timestamptz (the fence), $3 last_error text (a token
-- ^[a-z_]{1,32}$: the errs kind in lower case, panic, no_handler, gave_up), $4 now
-- timestamptz (finished_at and updated_at).
-- J1 (jobs.md decision 23, F55): when a request was held on the row, the row does not end
-- failed: it becomes queued at next_run_at with next_args, attempts 0, started_at and
-- finished_at NULL, next_* cleared; last_error is still $3 (the failed run's token), so the
-- newer request supersedes the failed one and the failure stays in last_error, log and metric.
-- Columns, in order: id uuid, status text ('failed', or 'queued' when a held request was
-- applied), next boolean (true when a held request was applied: the runner logs next=true).
-- ZERO rows: the lease was lost, as complete_job.sql.
-- Constraint errors (Invalid, a caller bug): job_last_error_chk.
-- Fails 25006 (db.ErrReadOnly) on a read-only copy.
WITH cur AS (
    SELECT id, next_run_at IS NOT NULL AS has_next
    FROM core.job
    WHERE id = $1::uuid
      AND status = 'running'
      AND leased_until = $2::timestamptz
    FOR UPDATE
)
UPDATE core.job AS j
SET status = CASE WHEN cur.has_next THEN 'queued' ELSE 'failed' END,
    run_at = coalesce(j.next_run_at, j.run_at),
    args = coalesce(j.next_args, j.args),
    attempts = CASE WHEN cur.has_next THEN 0 ELSE j.attempts END,
    started_at = CASE WHEN cur.has_next THEN NULL ELSE j.started_at END,
    finished_at = CASE WHEN cur.has_next THEN NULL ELSE $4::timestamptz END,
    next_run_at = NULL,
    next_args = NULL,
    leased_until = NULL,
    last_error = $3::text,
    updated_at = $4::timestamptz
FROM cur
WHERE j.id = cur.id
RETURNING j.id, j.status, cur.has_next AS next
