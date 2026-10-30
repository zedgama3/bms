-- Store.MarkSkipped: the recipient has no e-mail address (D57). Ends a leased row as skipped
-- with the token 'no_address'; there is nothing to retry, so the address, subject and body are
-- cleared in the same UPDATE (notify_message_sent_cleared_chk). failed_at holds the instant
-- the row ended (notify_message_failed_at_chk). Use Query (RETURNING), in its own short Write
-- transaction.
-- Args: $1 id uuid, $2 leased_until timestamptz (the fence, as in mark_sent.sql), $3
-- last_error text (a token: [a-z0-9_]{1,32}), $4 failed_at timestamptz.
-- Columns, in order: id uuid. ZERO rows: as mark_sent.sql.
-- Fails 25006 (db.ErrReadOnly) on a read-only copy.
UPDATE core.notify_message
SET status = 'skipped',
    failed_at = $4::timestamptz,
    last_error = $3::text,
    leased_until = NULL,
    address = NULL,
    subject = NULL,
    body = NULL
WHERE id = $1::uuid
  AND status = 'sending'
  AND leased_until = $2::timestamptz
RETURNING id
