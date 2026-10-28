-- Store.Fail: the run ended finally and badly: no_handler, panic, gave_up, or any errs kind
-- that is not retried. Fenced by the lease. Use Query (RETURNING); standalone, on a ctx
-- without the run's cancellation.
-- Args: $1 id uuid, $2 leased_until timestamptz (the fence), $3 last_error text (a token
-- ^[a-z_]{1,32}$: the errs kind in lower case, panic, no_handler, gave_up), $4 now
-- timestamptz (finished_at and updated_at), $5 max_attempts integer (Retry.MaxAttempts).
-- J1 (jobs.md decision 23, F55): when a request was held on the row, the row does not end
-- failed: it becomes queued at next_run_at with next_args, attempts 0, started_at and
-- finished_at NULL, next_* cleared; last_error is still $3 (the failed run's token), so the
-- newer request supersedes the failed one and the failure stays in last_error, log and metric.
-- D52 (a): when the held request was made by the run itself (next_by_run: the handler
-- re-enqueued its own key), the failed run's attempts are kept, not reset to 0, so a
-- self-rescheduling failing job cannot fail forever; and once they have reached $5 the held
-- request is dropped and the row ends failed (the same limit a retried job reaches). A request
-- from outside the run still resets attempts and always wins.
-- F55a: when that request is dropped at the limit, last_error is 'gave_up', not $3, whatever
-- the failed run's own kind (an errs kind, panic or unclassified): the row is given up on, and
-- the runner's log line says reason=gave_up (the run's own outcome and metric are unchanged).
-- Columns, in order: id uuid, status text ('failed', or 'queued' when a held request was
-- applied), next boolean (true when a held request was applied: the runner logs next=true),
-- gave_up boolean (true when a held by-run request was dropped at the limit and last_error is
-- 'gave_up').
-- ZERO rows: the lease was lost, as complete_job.sql.
-- Constraint errors (Invalid, a caller bug): job_last_error_chk.
-- Fails 25006 (db.ErrReadOnly) on a read-only copy.
WITH cur AS (
    SELECT id,
           next_run_at IS NOT NULL
               AND NOT (next_by_run AND attempts >= $5::integer) AS has_next,
           next_by_run AS by_run,
           next_run_at IS NOT NULL
               AND next_by_run AND attempts >= $5::integer AS dropped
    FROM core.job
    WHERE id = $1::uuid
      AND status = 'running'
      AND leased_until = $2::timestamptz
    FOR UPDATE
)
UPDATE core.job AS j
SET status = CASE WHEN cur.has_next THEN 'queued' ELSE 'failed' END,
    run_at = CASE WHEN cur.has_next THEN coalesce(j.next_run_at, j.run_at) ELSE j.run_at END,
    args = CASE WHEN cur.has_next THEN coalesce(j.next_args, j.args) ELSE j.args END,
    attempts = CASE WHEN cur.has_next AND NOT cur.by_run THEN 0 ELSE j.attempts END,
    started_at = CASE WHEN cur.has_next THEN NULL ELSE j.started_at END,
    finished_at = CASE WHEN cur.has_next THEN NULL ELSE $4::timestamptz END,
    next_run_at = NULL,
    next_args = NULL,
    next_by_run = false,
    leased_until = NULL,
    last_error = CASE WHEN cur.dropped THEN 'gave_up' ELSE $3::text END,
    updated_at = $4::timestamptz
FROM cur
WHERE j.id = cur.id
RETURNING j.id, j.status, cur.has_next AS next, cur.dropped AS gave_up
