-- Store.Lease: a tick's step 3. In one short Write transaction, take up to $2 due rows and
-- mark them running. Use Query inside the Write transaction. Due means queued with
-- run_at <= now, or running with an expired lease AND attempts < $5 (a crashed owner: the row
-- is re-leased and may run a second time, at-least-once; it is tagged last_error =
-- 'lost_lease'). A lapsed row whose attempts already reached $5 is NOT leased: it is a poison
-- row (every run killed its process) and is left for reclaim_expired_leases.sql, which fails
-- it 'gave_up'. This keeps lease_jobs.sql consistent with that sweep even when the sweep's
-- limit left some rows behind or the lease lapsed after the sweep ran.
-- FOR UPDATE SKIP LOCKED lets two processes on one database take different rows. Each lease
-- counts one attempt. The run_at <= now bound on both arms is implied for a lapsed lease (a
-- row is only leased once run_at has passed) and lets the index range stop at "now".
-- Args: $1 now timestamptz, $2 limit integer (free workers, at most MaxBatch),
-- $3 leased_until timestamptz (now + Lease), $4 instance text (config Instance.Name),
-- $5 max_attempts integer (Retry.MaxAttempts, the same value reclaim_expired_leases.sql takes
-- as $2). NEW in this revision: five parameters, not four.
-- leased_until is set from this parameter, never from now(), and is returned, so Go holds
-- the exact stored value (timestamptz keeps microseconds; a value with nanoseconds comes back
-- rounded, so always use the returned one). It is the fence for heartbeat_job.sql,
-- complete_job.sql, fail_job.sql, requeue_job.sql and release_job.sql. Two leases of one row
-- must not get the same value: $3 comes from a clock that moves forward.
-- Columns, in order, sorted by run_at, id: id uuid, name text, key text (NULL: none),
-- args jsonb (scan into []byte), attempts integer (already counting this attempt = Job.Attempt
-- and the backoff / MaxAttempts input), run_at timestamptz, leased_until timestamptz (the
-- fence: pass it back unchanged), trace_id text (NULL: none), started_at timestamptz (first
-- lease, set).
-- Served by job_due_idx (the attempts test is a filter on the entries it reads). Fails 25006 (db.ErrReadOnly) on a read-only copy.
-- Constraint errors: none reachable.
WITH due AS (
    SELECT id
    FROM core.job
    WHERE run_at <= $1::timestamptz
      AND (status = 'queued'
           OR (status = 'running' AND leased_until < $1::timestamptz
               AND attempts < $5::integer))
    ORDER BY run_at, id
    LIMIT $2::integer
    FOR UPDATE SKIP LOCKED
), leased AS (
    UPDATE core.job AS j
    SET last_error = CASE WHEN j.status = 'running' THEN 'lost_lease' ELSE j.last_error END,
        status = 'running',
        leased_until = $3::timestamptz,
        attempts = j.attempts + 1,
        started_at = coalesce(j.started_at, $1::timestamptz),
        instance = $4::text,
        updated_at = $1::timestamptz
    FROM due
    WHERE j.id = due.id
    RETURNING j.id, j.name, j.key, j.args, j.attempts, j.run_at, j.leased_until, j.trace_id,
              j.started_at
)
SELECT id, name, key, args, attempts, run_at, leased_until, trace_id, started_at
FROM leased
ORDER BY run_at, id
