-- Store.Reopen: clear closed_at. Use Query; zero rows: unknown or not closed (a safety net
-- after the locked check: treat as Conflict open).
-- Args: $1 id uuid, $2 at timestamptz (updated_at).
-- Columns, in order: as get_event.sql.
UPDATE core.event SET closed_at = NULL, updated_at = $2::timestamptz
WHERE id = $1 AND closed_at IS NOT NULL
RETURNING id, name, kind, starts_at, ends_at, zone, closed_at, created_at, updated_at
