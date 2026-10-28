-- Store.Requeue: a retryable failure (Unavailable or Timeout) with attempts left: back to
-- queued at the backoff time. The attempt stays counted. Fenced by the lease. Use Query
-- (RETURNING); standalone, on a ctx without the run's cancellation.
-- Args: $1 id uuid, $2 leased_until timestamptz (the fence), $3 last_error text (a token: the
-- kind in lower case), $4 run_at timestamptz (now + Retry.Delay(attempts + 1, u)), $5 now
-- timestamptz (updated_at).
-- J1 (jobs.md decision 23): when a request was held on the row, it supersedes the retry: the
-- row is queued at next_run_at (NOT $4: no backoff) with next_args and attempts 0,
-- started_at NULL, next_* cleared; last_error is still $3 (the failed run's token).
-- D52 (a): when that request was made by the run itself (next_by_run), attempts are kept
-- instead of reset to 0 (the run failed; its attempt stays counted).
-- Columns, in order: id uuid, status text (always 'queued'), next boolean (true when a held
-- request was applied instead of the backoff: the runner logs next=true). ZERO rows: the
-- lease was lost, as complete_job.sql.
-- Constraint errors (Invalid, a caller bug): job_last_error_chk.
-- Fails 25006 (db.ErrReadOnly) on a read-only copy.
WITH cur AS (
    SELECT id, next_run_at IS NOT NULL AS has_next, next_by_run AS by_run
    FROM core.job
    WHERE id = $1::uuid
      AND status = 'running'
      AND leased_until = $2::timestamptz
    FOR UPDATE
)
UPDATE core.job AS j
SET status = 'queued',
    run_at = coalesce(j.next_run_at, $4::timestamptz),
    args = coalesce(j.next_args, j.args),
    attempts = CASE WHEN cur.has_next AND NOT cur.by_run THEN 0 ELSE j.attempts END,
    started_at = CASE WHEN cur.has_next THEN NULL ELSE j.started_at END,
    next_run_at = NULL,
    next_args = NULL,
    next_by_run = false,
    leased_until = NULL,
    last_error = $3::text,
    updated_at = $5::timestamptz
FROM cur
WHERE j.id = cur.id
RETURNING j.id, j.status, cur.has_next AS next
