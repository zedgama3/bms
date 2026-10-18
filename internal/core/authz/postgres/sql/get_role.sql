-- Store.GetRole: one role without its permissions. Read-only; use Query (zero rows: NotFound).
-- Args: $1 id uuid. Columns: as list_roles.sql.
SELECT id, key, name, description, scope, base, created_at, updated_at
FROM core.role
WHERE id = $1
