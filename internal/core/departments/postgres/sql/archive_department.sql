-- Store.Archive: set archived_at. Use Query; zero rows: the department is unknown or already
-- archived (the use case has locked the row and checked, so this is a safety net: treat as
-- Conflict archived). Grants are untouched (decision 4).
-- Args: $1 id uuid, $2 at timestamptz (archived_at and updated_at).
-- Columns, in order: as get_department.sql.
UPDATE core.department SET archived_at = $2::timestamptz, updated_at = $2::timestamptz
WHERE id = $1 AND archived_at IS NULL
RETURNING id, name, description, contact_email, archived_at, created_at, updated_at
