-- Store.Release: give the row back WITHOUT counting the attempt: shutdown or a lost-lease
-- cancel (last_error 'canceled'), or a run that met db.ErrReadOnly (last_error NULL). Back to
-- queued, run_at unchanged, attempts - 1. Also the way to give back rows a tick leased but
-- never started. Fenced by the lease. Use Query (RETURNING); standalone, on a ctx without the
-- run's cancellation.
-- Args: $1 id uuid, $2 leased_until timestamptz (the fence), $3 last_error text (a token, or
-- NULL to leave the current one), $4 now timestamptz (updated_at).
-- J1 (jobs.md decision 23, F56): when a request was held on the row, it replaces the old
-- schedule: run_at = next_run_at, args = next_args, attempts 0 (not attempts - 1),
-- started_at NULL, next_* cleared; last_error is still coalesce($3, last_error).
-- Columns, in order: id uuid, status text (always 'queued'), next boolean (true when a held
-- request was applied: the runner logs next=true). ZERO rows: the lease was lost, as
-- complete_job.sql (the lease then expires on its own).
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
SET status = 'queued',
    run_at = coalesce(j.next_run_at, j.run_at),
    args = coalesce(j.next_args, j.args),
    attempts = CASE WHEN cur.has_next THEN 0 ELSE greatest(j.attempts - 1, 0) END,
    started_at = CASE WHEN cur.has_next THEN NULL ELSE j.started_at END,
    next_run_at = NULL,
    next_args = NULL,
    leased_until = NULL,
    last_error = coalesce($3::text, j.last_error),
    updated_at = $4::timestamptz
FROM cur
WHERE j.id = cur.id
RETURNING j.id, j.status, cur.has_next AS next
