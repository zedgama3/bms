-- Store.RolePermissions: one role's permissions (GetRole, UpdateRole's diff, Grant's
-- no-escalation check). Read-only; use Query.
-- Args: $1 role_id uuid. Columns, in order: permission text. Sorted in byte order. Zero rows:
-- the role holds nothing (or does not exist; GetRole tells them apart).
SELECT permission
FROM core.role_permission
WHERE role_id = $1
ORDER BY permission COLLATE "C"
