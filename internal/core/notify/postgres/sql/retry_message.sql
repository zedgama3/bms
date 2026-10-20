-- Store.Retry: an admin requeues a Failed row: attempts back to 0, due now, expires_at
-- unchanged. Use Query (RETURNING) inside the Write transaction that also records
-- notify.message.retried. Only while the text is still held and the row has not expired.
-- Args: $1 id uuid, $2 now timestamptz.
-- Columns, in order: id uuid. ZERO rows: no such row (NotFound), the row is not failed
-- (Conflict, field status), or it expired / lost its text (Conflict, code expired); ask
-- get_message_status.sql which.
-- Fails 25006 (db.ErrReadOnly) on a read-only copy.
-- Constraint errors: none reachable.
UPDATE core.notify_message
SET status = 'queued',
    attempts = 0,
    next_attempt_at = $2::timestamptz,
    failed_at = NULL,
    last_error = NULL
WHERE id = $1::uuid
  AND status = 'failed'
  AND subject IS NOT NULL
  AND expires_at > $2::timestamptz
RETURNING id
