-- Store.DeleteRolePermissions: UpdateRole replaces the permission set (this, then
-- insert_role_permissions.sql, in one transaction). Use Exec. Args: $1 role_id uuid.
DELETE FROM core.role_permission
WHERE role_id = $1
