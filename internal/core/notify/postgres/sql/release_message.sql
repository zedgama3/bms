-- Store.Release: the context was canceled mid-batch (shutdown): the row goes back to queued,
-- due now, and the attempt does not count (F27). Also the way to give back rows the rest of a
-- batch never reached. Use Query (RETURNING).
-- Args: $1 id uuid, $2 leased_until timestamptz (the fence, as in mark_sent.sql; was attempts
-- integer), $3 now timestamptz (becomes next_attempt_at), $4 last_error text (a token, e.g.
-- canceled).
-- Columns, in order: id uuid. ZERO rows: as mark_sent.sql.
-- Constraint errors (Invalid, a caller bug): notify_message_last_error_chk,
-- notify_message_expired_cleared_chk.
-- Fails 25006 (db.ErrReadOnly) on a read-only copy (the lease then expires on its own).
UPDATE core.notify_message
SET status = 'queued',
    next_attempt_at = $3::timestamptz,
    leased_until = NULL,
    attempts = greatest(attempts - 1, 0),
    last_error = $4::text
WHERE id = $1::uuid
  AND status = 'sending'
  AND leased_until = $2::timestamptz
RETURNING id
