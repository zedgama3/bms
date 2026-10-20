-- Store.QueueStats: the bms.jobs.queue{status} gauge (queued, running) and the backlog age,
-- refreshed each tick on the writer or a replica. Read-only; use Query. Never reads a final
-- row (job_due_idx holds only queued and running rows).
-- Args: $1 now timestamptz.
-- Columns, in order, sorted by status: status text ('queued' or 'running'; absent when no
-- row has it, the caller reports 0), depth bigint (scan into int64: rows in that status),
-- due bigint (rows with run_at <= now: for queued, work ready to start; for running, all
-- of them), oldest_due timestamptz (the smallest run_at among the due rows, NULL when due is
-- 0; lag = now - oldest_due is how long the oldest ready job has waited).
-- Served by job_due_idx.
SELECT status,
       count(*) AS depth,
       count(*) FILTER (WHERE run_at <= $1::timestamptz) AS due,
       min(run_at) FILTER (WHERE run_at <= $1::timestamptz) AS oldest_due
FROM core.job
WHERE status IN ('queued', 'running')
GROUP BY status
ORDER BY status
