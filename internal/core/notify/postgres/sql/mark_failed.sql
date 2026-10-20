-- Store.MarkFailed: a final failure (rejected, no_address, or a transient failure on the last
-- attempt). KEEPS the address, subject and body, so an admin can retry the row after fixing the
-- relay; expire_stale_messages.sql clears them at expires_at (notify.md Amendments 2). Use
-- Query (RETURNING).
-- Args: $1 id uuid, $2 leased_until timestamptz (the fence, as in mark_sent.sql; was attempts
-- integer), $3 last_error text (a token: [a-z0-9_]{1,32}, never a server reply), $4 failed_at
-- timestamptz.
-- Columns, in order: id uuid. ZERO rows: as mark_sent.sql.
-- Constraint errors (Invalid, a caller bug): notify_message_last_error_chk (not a token),
-- notify_message_expired_cleared_chk (the token 'expired' belongs to the sweep, which clears).
-- Fails 25006 (db.ErrReadOnly) on a read-only copy.
UPDATE core.notify_message
SET status = 'failed',
    failed_at = $4::timestamptz,
    last_error = $3::text,
    leased_until = NULL
WHERE id = $1::uuid
  AND status = 'sending'
  AND leased_until = $2::timestamptz
RETURNING id
