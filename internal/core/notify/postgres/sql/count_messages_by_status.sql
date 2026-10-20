-- Store.CountByStatus: OutboxView.Counts and the bms.notify.outbox{status} gauge. Read-only;
-- use Query. A status with no rows is absent (the caller fills zeros).
-- Args: none.
-- Columns, in order, sorted by status: status text, count bigint (scan into int64).
-- Counts every row including sent ones, so it reads the whole table once (about 17 ms for
-- 300 000 rows): fine for the admin screen, too heavy for every poll tick. The gauge uses
-- count_open_messages_by_status.sql, which never reads sent rows.
SELECT status, count(*) AS count
FROM core.notify_message
GROUP BY status
ORDER BY status
