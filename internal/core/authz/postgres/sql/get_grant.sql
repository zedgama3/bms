-- Store.GetGrant: Revoke reads the grant before checking the caller's extent. Read-only; use
-- Query (zero rows: NotFound). Args: $1 id uuid.
-- Columns, in order: id uuid, person_id uuid, role_id uuid, department_id uuid (NULL:
-- site-wide), ends_at timestamptz (NULL: no end), granted_by uuid (NULL: none), created_at
-- timestamptz. The same columns, in the same order, as list_grants.sql and delete_grant.sql.
SELECT id, person_id, role_id, department_id, ends_at, granted_by, created_at
FROM core.role_grant
WHERE id = $1
