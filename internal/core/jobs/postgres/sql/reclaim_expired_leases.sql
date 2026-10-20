-- Store.ReclaimExpired: optional sweep, run at the start of a tick (before lease_jobs.sql)
-- on the writer. Takes up to $3 Running rows whose lease has lapsed (a crashed or hung
-- process that never wrote an outcome) and settles them without waiting for a worker to
-- lease them again:
--   * attempts < $2: back to queued, run_at unchanged (so due now), leased_until cleared,
--     last_error = 'lost_lease'. The attempt stays counted.
--   * attempts >= $2: failed, last_error = 'gave_up', finished_at = now. This stops a poison
--     row (a handler that kills its process every time) from being leased forever;
--     lease_jobs.sql alone would re-lease it indefinitely, because attempts are only checked
--     when a handler returns.
-- lease_jobs.sql (with its $5 = the same max_attempts) leaves a lapsed row with
-- attempts >= max alone, so the sweep is the only way such a row ends: call this at the start of
-- every writer pass, before lease_jobs.sql, in the same Write transaction or an earlier one. A
-- lapsed row with attempts < max is re-leased by lease_jobs.sql by itself, so the sweep's limit
-- ($3) leaving rows behind is harmless for them. Use Query inside a Write transaction.
-- Args: $1 now timestamptz, $2 max_attempts integer (Retry.MaxAttempts), $3 limit integer
-- (MaxBatch).
-- Columns, in order, sorted by id: id uuid, name text, status text ('queued' or 'failed'),
-- attempts integer. Zero rows is the normal case.
-- Served by job_lease_idx.
-- Fails 25006 (db.ErrReadOnly) on a read-only copy.
-- Constraint errors: none reachable.
WITH lapsed AS (
    SELECT id
    FROM core.job
    WHERE run_at <= $1::timestamptz
      AND status = 'running'
      AND leased_until < $1::timestamptz
    ORDER BY run_at, id
    LIMIT $3::integer
    FOR UPDATE SKIP LOCKED
)
UPDATE core.job AS j
SET status = CASE WHEN j.attempts >= $2::integer THEN 'failed' ELSE 'queued' END,
    finished_at = CASE WHEN j.attempts >= $2::integer THEN $1::timestamptz END,
    last_error = CASE WHEN j.attempts >= $2::integer THEN 'gave_up' ELSE 'lost_lease' END,
    leased_until = NULL,
    updated_at = $1::timestamptz
FROM lapsed
WHERE j.id = lapsed.id
RETURNING j.id, j.name, j.status, j.attempts
