-- Store.Purge: delete up to $2 final rows (sent or failed) whose completion time is before
-- $1. Called by Run every PurgeInterval when notify.retention > 0, in a loop until fewer than
-- the limit are deleted (notify.md "Purge"). A final row older than the retention
-- (at least MinRetention = 7 days = MaxAge) is long past its expires_at, so it holds no text
-- once expire_stale_messages.sql has run; if the sweep has not reached it yet, deleting it
-- removes the text with it. Expiry does not depend on retention (it also runs at retention 0).
-- Use QueryRow (one row), standalone or in a Write transaction. Skips locked rows.
-- Args: $1 finished_before timestamptz (now - retention), $2 limit integer (1000).
-- Columns, in order: deleted bigint (scan into int64).
-- Served by notify_message_finished_idx. Fails 25006 (db.ErrReadOnly) on a read-only copy.
-- Constraint errors: none reachable.
WITH picked AS (
    SELECT id
    FROM core.notify_message
    WHERE status IN ('sent', 'failed')
      AND coalesce(sent_at, failed_at) < $1::timestamptz
    ORDER BY coalesce(sent_at, failed_at), id
    LIMIT $2::integer
    FOR UPDATE SKIP LOCKED
), gone AS (
    -- By an array, not a join: with a parameter limit the planner cannot size a join and a
    -- cached generic plan seq-scans the table (measured: 30 ms against 4 ms).
    DELETE FROM core.notify_message
    WHERE id = ANY (ARRAY(SELECT id FROM picked))
    RETURNING 1
)
SELECT count(*) AS deleted FROM gone
