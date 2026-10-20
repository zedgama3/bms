-- Store.MarkSent: the server accepted the message. Clears the address, subject and body in the
-- same UPDATE (notify_message_sent_cleared_chk). Use Query (RETURNING); run it
-- outside the lease transaction, in its own short Write transaction or a standalone statement.
-- The fence is the lease itself: $2 is the exact leased_until that lease_messages.sql returned
-- for this row. It is unique to one lease (a re-lease or a Retry gives a later value or none),
-- unlike attempts, which Retry resets to 0 and so can repeat. A sender whose lease expired and
-- was taken over, or whose row was retried and leased again, matches nothing.
-- Args: $1 id uuid, $2 leased_until timestamptz (the fence), $3 sent_at timestamptz.
-- (Changed: $2 was attempts integer.)
-- Columns, in order: id uuid. ZERO rows means the row is gone (canceled, or the person was
-- deleted) or was re-leased (or retried and leased again) by another sender: nothing changed; the caller logs it by ID.
-- Fails 25006 (db.ErrReadOnly) on a read-only copy: the lease is left to expire (F28).
-- Constraint errors: none reachable.
UPDATE core.notify_message
SET status = 'sent',
    sent_at = $3::timestamptz,
    leased_until = NULL,
    last_error = NULL,
    address = NULL,
    subject = NULL,
    body = NULL
WHERE id = $1::uuid
  AND status = 'sending'
  AND leased_until = $2::timestamptz
RETURNING id
