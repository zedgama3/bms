-- Store.ListOutbox: OutboxView.Recent, the admin's rows that are not sent (queued, sending,
-- failed, skipped), newest first, keyset-paged. Read-only; use Query. NEVER selects address, subject
-- or body: an admin sees IDs, names and tokens only (permission notify.outbox.manage).
-- Args: $1 limit integer (notify.MaxOutboxList), $2 before_created_at timestamptz,
-- $3 before_id uuid (the last row of the previous page; both NULL for the first page).
-- Columns, in order: id uuid, owner text, template text, person_id uuid (NULL: an explicit
-- address, shown as "address"), status text, attempts integer, last_error text (token or
-- NULL), created_at timestamptz, next_attempt_at timestamptz, expires_at timestamptz (so the screen can
-- disable Retry on an expired row: the text is gone from then on).
-- Order: created_at DESC, id DESC. The cursor is spelled as in audit's search_entries.sql
-- (no row comparison). Served by notify_message_outbox_idx.
SELECT id, owner, template, person_id, status, attempts, last_error, created_at, next_attempt_at,
       expires_at
FROM core.notify_message
WHERE status <> 'sent'
  AND created_at <= COALESCE($2::timestamptz, 'infinity'::timestamptz)
  AND (created_at < COALESCE($2::timestamptz, 'infinity'::timestamptz)
       OR id < COALESCE($3::uuid, 'ffffffff-ffff-ffff-ffff-ffffffffffff'::uuid))
ORDER BY created_at DESC, id DESC
LIMIT $1::integer
