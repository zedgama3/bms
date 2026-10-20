-- Store.Release: give the row back WITHOUT counting the attempt: shutdown or a lost-lease
-- cancel (last_error 'canceled'), or a run that met db.ErrReadOnly (last_error NULL). Back to
-- queued, run_at unchanged, attempts - 1. Also the way to give back rows a tick leased but
-- never started. Fenced by the lease. Use Query (RETURNING); standalone, on a ctx without the
-- run's cancellation.
-- Args: $1 id uuid, $2 leased_until timestamptz (the fence), $3 last_error text (a token, or
-- NULL to leave the current one), $4 now timestamptz (updated_at).
-- Columns, in order: id uuid. ZERO rows: the lease was lost, as complete_job.sql (the lease
-- then expires on its own).
-- Constraint errors (Invalid, a caller bug): job_last_error_chk.
-- Fails 25006 (db.ErrReadOnly) on a read-only copy.
UPDATE core.job
SET status = 'queued',
    attempts = greatest(attempts - 1, 0),
    leased_until = NULL,
    last_error = coalesce($3::text, last_error),
    updated_at = $4::timestamptz
WHERE id = $1::uuid
  AND status = 'running'
  AND leased_until = $2::timestamptz
RETURNING id
