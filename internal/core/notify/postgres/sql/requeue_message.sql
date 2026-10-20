-- Store.Requeue: a transient failure with attempts left: back to queued, due at the backoff
-- time. The attempt stays counted. Use Query (RETURNING).
-- Args: $1 id uuid, $2 leased_until timestamptz (the fence, as in mark_sent.sql; was attempts
-- integer), $3 last_error text (a token), $4 next_attempt_at timestamptz (now + retry.Policy
-- delay).
-- Columns, in order: id uuid. ZERO rows: as mark_sent.sql.
-- Constraint errors (Invalid, a caller bug): notify_message_last_error_chk,
-- notify_message_expired_cleared_chk.
-- Fails 25006 (db.ErrReadOnly) on a read-only copy.
UPDATE core.notify_message
SET status = 'queued',
    next_attempt_at = $4::timestamptz,
    leased_until = NULL,
    last_error = $3::text
WHERE id = $1::uuid
  AND status = 'sending'
  AND leased_until = $2::timestamptz
RETURNING id
