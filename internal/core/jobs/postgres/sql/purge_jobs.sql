-- Store.Purge: jobs.purge. Delete up to $2 final rows (done, failed, canceled) whose
-- finished_at is before $1. Called every hour on the writer in a loop until fewer than the
-- limit are deleted (notify's purge shape). Waiting and running rows are never touched. Use
-- QueryRow (one row), standalone or in a Write transaction. Skips locked rows.
-- Args: $1 finished_before timestamptz (now - DoneTTL), $2 limit integer (MaxBatch = 1000).
-- Columns, in order: deleted bigint (scan into int64).
-- Served by job_finished_idx. Fails 25006 (db.ErrReadOnly) on a read-only copy.
-- Constraint errors: none reachable.
WITH picked AS (
    SELECT id
    FROM core.job
    WHERE status IN ('done', 'failed', 'canceled')
      AND finished_at < $1::timestamptz
    ORDER BY finished_at, id
    LIMIT $2::integer
    FOR UPDATE SKIP LOCKED
), gone AS (
    -- By an array, not a join: with a parameter limit the planner cannot size a join and a
    -- cached generic plan may seq-scan the table (the notify purge measured this).
    DELETE FROM core.job
    WHERE id = ANY (ARRAY(SELECT id FROM picked))
    RETURNING 1
)
SELECT count(*) AS deleted FROM gone
