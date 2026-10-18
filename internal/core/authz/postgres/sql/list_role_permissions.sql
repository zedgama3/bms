-- Store.ListRolePermissions: every role's permissions, for ListRoles. Read-only; use Query.
-- No args.
-- Columns, in order: role_id uuid, permission text. Sorted by role_id, then permission (byte
-- order), so the caller can walk it alongside list_roles.sql rows.
SELECT role_id, permission
FROM core.role_permission
ORDER BY role_id, permission COLLATE "C"
