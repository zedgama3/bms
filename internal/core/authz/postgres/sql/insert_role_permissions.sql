-- Store.InsertRolePermissions: the permissions of a new role, or UpdateRole's replacement
-- (after delete_role_permissions.sql). Use Exec.
-- Args: $1 role_id uuid, $2 permissions text[] (unique; a repeat fails on role_permission_pkey
-- and a malformed name on role_permission_permission_chk: Invalid). A missing role fails on
-- role_permission_role_id_fkey.
INSERT INTO core.role_permission (role_id, permission)
SELECT $1::uuid, p
FROM unnest($2::text[]) AS p
