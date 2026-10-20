-- Store.Complete: the handler returned nil (also a dropped signals.redeliver). Fenced by
-- the lease. Clears last_error (an earlier retry's token is not the outcome). Use Query
-- (RETURNING); standalone, on a ctx without the run's cancellation (recordTimeout).
-- Args: $1 id uuid, $2 leased_until timestamptz (the fence lease_jobs.sql / heartbeat_job.sql
-- last returned), $3 now timestamptz (finished_at and updated_at).
-- Columns, in order: id uuid. ZERO rows: the lease was lost (re-leased by another process or
-- reclaimed): the row is not ours; write nothing more, outcome lost_lease.
-- Fails 25006 (db.ErrReadOnly) on a read-only copy (the lease then lapses on its own).
-- Constraint errors: none reachable.
UPDATE core.job
SET status = 'done',
    finished_at = $3::timestamptz,
    leased_until = NULL,
    last_error = NULL,
    updated_at = $3::timestamptz
WHERE id = $1::uuid
  AND status = 'running'
  AND leased_until = $2::timestamptz
RETURNING id
