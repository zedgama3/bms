-- Store.Fail: the run ended finally and badly: no_handler, panic, gave_up, or any errs kind
-- that is not retried. Fenced by the lease. Use Query (RETURNING); standalone, on a ctx
-- without the run's cancellation.
-- Args: $1 id uuid, $2 leased_until timestamptz (the fence), $3 last_error text (a token
-- ^[a-z_]{1,32}$: the errs kind in lower case, panic, no_handler, gave_up), $4 now
-- timestamptz (finished_at and updated_at).
-- Columns, in order: id uuid. ZERO rows: the lease was lost, as complete_job.sql.
-- Constraint errors (Invalid, a caller bug): job_last_error_chk.
-- Fails 25006 (db.ErrReadOnly) on a read-only copy.
UPDATE core.job
SET status = 'failed',
    finished_at = $4::timestamptz,
    leased_until = NULL,
    last_error = $3::text,
    updated_at = $4::timestamptz
WHERE id = $1::uuid
  AND status = 'running'
  AND leased_until = $2::timestamptz
RETURNING id
