-- Store.Restore: clear archived_at. Use Query; zero rows: unknown or not archived (a safety net
-- after the locked check: treat as Conflict not_archived).
-- Args: $1 id uuid, $2 at timestamptz (updated_at).
-- Columns, in order: as get_department.sql.
UPDATE core.department SET archived_at = NULL, updated_at = $2::timestamptz
WHERE id = $1 AND archived_at IS NOT NULL
RETURNING id, name, description, contact_email, archived_at, created_at, updated_at
