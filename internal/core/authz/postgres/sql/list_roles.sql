-- Store.ListRoles: roles without their permissions (list_role_permissions.sql adds them).
-- Read-only; use Query. Roles are few (tens), so this is not paginated.
-- Args: $1 scope text (NULL: every role; 'department': only department roles, for a caller
-- whose extent is departments).
-- Columns, in order: id uuid, key text (NULL for a regional's own role), name text,
-- description text, scope text ('site' or 'department'), base boolean, created_at timestamptz,
-- updated_at timestamptz. Sorted by name, or by key for an unrenamed starter role (byte
-- order, lower-case), then id.
SELECT id, key, name, description, scope, base, created_at, updated_at
FROM core.role
WHERE $1::text IS NULL OR scope = $1::text
ORDER BY lower(coalesce(nullif(name, ''), key)) COLLATE "C", id
