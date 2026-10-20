-- Store.Requeue: a retryable failure (Unavailable or Timeout) with attempts left: back to
-- queued at the backoff time. The attempt stays counted. Fenced by the lease. Use Query
-- (RETURNING); standalone, on a ctx without the run's cancellation.
-- Args: $1 id uuid, $2 leased_until timestamptz (the fence), $3 last_error text (a token: the
-- kind in lower case), $4 run_at timestamptz (now + Retry.Delay(attempts + 1, u)), $5 now
-- timestamptz (updated_at).
-- Columns, in order: id uuid. ZERO rows: the lease was lost, as complete_job.sql.
-- Constraint errors (Invalid, a caller bug): job_last_error_chk.
-- Fails 25006 (db.ErrReadOnly) on a read-only copy.
UPDATE core.job
SET status = 'queued',
    run_at = $4::timestamptz,
    leased_until = NULL,
    last_error = $3::text,
    updated_at = $5::timestamptz
WHERE id = $1::uuid
  AND status = 'running'
  AND leased_until = $2::timestamptz
RETURNING id
