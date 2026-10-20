-- Store.CountOpenByStatus: the counts of rows that are not sent (queued, sending, failed), for
-- the bms.notify.outbox{status} gauge after each tick and for the "waiting" log line. Read-only;
-- use Query. Unlike count_messages_by_status.sql it does not read the sent rows, which pile up
-- without bound while notify.retention is 0, so it costs the same however old the installation
-- is: an index-only scan of notify_message_outbox_idx (status is INCLUDEd).
-- Args: none.
-- Columns, in order, sorted by status: status text, count bigint (scan into int64). A status
-- with no rows is absent (the caller fills zeros).
SELECT status, count(*) AS count
FROM core.notify_message
WHERE status <> 'sent'
GROUP BY status
ORDER BY status
