-- Store.ExpireStale: the expiry sweep (notify.md Amendments 4). Every row with expires_at <= $1
-- that still holds text or an address has the address, subject and body cleared:
--   * queued, or sending with an EXPIRED lease: becomes failed, failed_at = $1, last_error
--     'expired', lease cleared (it will never be sent);
--   * failed (already final, kept its text for a retry): only cleared; failed_at and
--     last_error stay as they were;
--   * sending with a live lease (leased_until >= $1): left alone; a later sweep gets it. The
--     sender holds it and its mark_* will end it.
-- (sent rows hold no text, so they never match.) Runs whatever notify.retention is. Use QueryRow
-- (one row), standalone or in a Write transaction. Skips rows locked by another transaction.
-- Args: $1 now timestamptz, $2 limit integer (1000).
-- Columns, in order: ended bigint (scan into int64): every row changed this call, including
-- failed rows that were only cleared; expired bigint: the rows that BECAME failed/expired in
-- this call, i.e. whose status was not already failed (count these for the
-- bms.notify.delivered{outcome=expired} metric). expired <= ended. Call again while ended
-- equals the limit.
-- Served by notify_message_expiry_idx. Fails 25006 (db.ErrReadOnly) on a read-only copy.
-- Constraint errors: none reachable.
WITH stale AS (
    SELECT id, status
    FROM core.notify_message
    WHERE subject IS NOT NULL
      AND expires_at <= $1::timestamptz
      AND NOT (status = 'sending' AND leased_until >= $1::timestamptz)
    ORDER BY expires_at, id
    LIMIT $2::integer
    FOR UPDATE SKIP LOCKED
), ended AS (
    -- By an array, not a join: with a parameter limit the planner cannot size a join and a
    -- cached generic plan can seq-scan the table. The rows are locked by stale, so the status
    -- stale saw is the status this UPDATE replaces.
    UPDATE core.notify_message
    SET failed_at = CASE WHEN status = 'failed' THEN failed_at ELSE $1::timestamptz END,
        last_error = CASE WHEN status = 'failed' THEN last_error ELSE 'expired' END,
        status = 'failed',
        leased_until = NULL,
        address = NULL,
        subject = NULL,
        body = NULL
    WHERE id = ANY (ARRAY(SELECT id FROM stale))
    RETURNING id
)
SELECT count(*) AS ended,
       count(*) FILTER (WHERE stale.status <> 'failed') AS expired
FROM ended
JOIN stale USING (id)
