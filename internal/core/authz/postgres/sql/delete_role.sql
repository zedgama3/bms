-- Store.DeleteRole. Use Query; zero rows means the role does not exist or is the base role
-- (the service reads the role first and answers NotFound or Invalid base before this runs).
-- Args: $1 id uuid. Columns: id uuid. role_permission rows go with it (ON DELETE CASCADE).
-- A role with grants (active or ended) or rules fails with 23503 on role_grant_role_id_fkey or
-- sod_rule_role_id_fkey; db.Wrap says Invalid, the store re-kinds it to Conflict in_use.
DELETE FROM core.role
WHERE id = $1
  AND NOT base
RETURNING id
