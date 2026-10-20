-- Store.CountByStatus: StatusView.Counts. Read-only; use Query. A status with no rows is
-- absent (the caller fills zeros).
-- Args: none.
-- Columns, in order, sorted by status: status text, count bigint (scan into int64).
-- Counts every row including final ones (at most DoneTTL of them), so it reads the whole
-- table once: fine for the admin screen, too heavy for every poll tick. The metrics gauge
-- uses queue_stats.sql.
SELECT status, count(*) AS count
FROM core.job
GROUP BY status
ORDER BY status
