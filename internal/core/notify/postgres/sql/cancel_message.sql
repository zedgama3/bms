-- Store.Cancel: an admin cancels a queued or failed row by deleting it. Use Query (RETURNING)
-- inside the Write transaction that also records notify.message.canceled. A sending row is not
-- canceled (the sender has it).
-- Args: $1 id uuid.
-- Columns, in order: id uuid. ZERO rows: no such row (NotFound) or it is sending or sent
-- (Conflict, field status); ask get_message_status.sql which.
-- Fails 25006 (db.ErrReadOnly) on a read-only copy.
-- Constraint errors: none reachable.
DELETE FROM core.notify_message
WHERE id = $1::uuid
  AND status IN ('queued', 'failed')
RETURNING id
