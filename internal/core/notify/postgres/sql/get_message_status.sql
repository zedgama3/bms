-- Store.StatusOf: tells Retry and Cancel why retry_message.sql or cancel_message.sql changed
-- nothing. Read-only; use QueryRow.
-- Args: $1 id uuid.
-- Columns, in order: status text, expires_at timestamptz, has_text boolean (the row still
-- holds its subject, body and any address). pgx.ErrNoRows (NotFound) when there is no such
-- row. For Retry: status failed and (NOT has_text OR expires_at <= the caller's now) is
-- Conflict code expired; any other status is Conflict code status. For Cancel: sending or sent
-- is Conflict code status.
SELECT status, expires_at, (subject IS NOT NULL) AS has_text
FROM core.notify_message
WHERE id = $1::uuid
