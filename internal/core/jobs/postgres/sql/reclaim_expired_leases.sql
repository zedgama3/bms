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
--   * J1 (jobs.md decision 23, F56), both arms: a row with a held request (next_run_at set)
--     takes neither of the above: it is queued at next_run_at with next_args, attempts 0,
--     started_at and finished_at NULL, next_* cleared, last_error = 'lost_lease' (whatever its
--     attempts: the newer request supersedes the lapsed run, so a poison row is not given up
--     on while a request waits; the request's own runs then count from 0).
--   * D52 (a): a held request made by the run itself (next_by_run) keeps the row's attempts
--     (the lapsed run failed) and, once attempts >= $2, is dropped: the row takes the failed
--     arm above, so a self-rescheduling poison row is given up on. A request from outside the
--     run behaves as the previous bullet says.
-- lease_jobs.sql (with its $5 = the same max_attempts) leaves a lapsed row with
-- attempts >= max alone, so the sweep is the only way such a row ends: call this at the start of
-- every writer pass, before lease_jobs.sql, in the same Write transaction or an earlier one. A
-- lapsed row with attempts < max is re-leased by lease_jobs.sql by itself, so the sweep's limit
-- ($3) leaving rows behind is harmless for them. Use Query inside a Write transaction.
-- Args: $1 now timestamptz, $2 max_attempts integer (Retry.MaxAttempts), $3 limit integer
-- (MaxBatch).
-- Columns, in order, sorted by id: id uuid, name text, status text ('queued' or 'failed'),
-- attempts integer (0 when a held request was applied), next boolean (true when a held
-- request was applied: a 'queued' row counted lost_lease and logged next=true). Zero rows is
-- the normal case.
-- Served by job_lease_idx.
-- Fails 25006 (db.ErrReadOnly) on a read-only copy.
-- Constraint errors: none reachable.
WITH lapsed AS (
    SELECT id,
           next_run_at IS NOT NULL
               AND NOT (next_by_run AND attempts >= $2::integer) AS has_next,
           next_by_run AS by_run
    FROM core.job
    WHERE run_at <= $1::timestamptz
      AND status = 'running'
      AND leased_until < $1::timestamptz
    ORDER BY run_at, id
    LIMIT $3::integer
    FOR UPDATE SKIP LOCKED
)
UPDATE core.job AS j
SET status = CASE WHEN NOT lapsed.has_next AND j.attempts >= $2::integer
                  THEN 'failed' ELSE 'queued' END,
    run_at = CASE WHEN lapsed.has_next THEN coalesce(j.next_run_at, j.run_at) ELSE j.run_at END,
    args = CASE WHEN lapsed.has_next THEN coalesce(j.next_args, j.args) ELSE j.args END,
    attempts = CASE WHEN lapsed.has_next AND NOT lapsed.by_run THEN 0 ELSE j.attempts END,
    started_at = CASE WHEN lapsed.has_next THEN NULL ELSE j.started_at END,
    finished_at = CASE WHEN NOT lapsed.has_next AND j.attempts >= $2::integer
                       THEN $1::timestamptz END,
    last_error = CASE WHEN NOT lapsed.has_next AND j.attempts >= $2::integer
                      THEN 'gave_up' ELSE 'lost_lease' END,
    next_run_at = NULL,
    next_args = NULL,
    next_by_run = false,
    leased_until = NULL,
    updated_at = $1::timestamptz
FROM lapsed
WHERE j.id = lapsed.id
RETURNING j.id, j.name, j.status, j.attempts, lapsed.has_next AS next
