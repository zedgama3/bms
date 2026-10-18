-- Store.InsertGrant: Grant. Use Exec. Inside the Write transaction, after lock_invariants.sql.
-- Args: $1 id uuid, $2 person_id uuid, $3 role_id uuid, $4 department_id uuid (NULL:
-- site-wide), $5 ends_at timestamptz (NULL: no end), $6 granted_by uuid (NULL: operator),
-- $7 created_at timestamptz.
-- Constraint errors: role_grant_person_role_department_key (Conflict, duplicate: the same
-- person, role and department, active or ended); role_grant_role_id_fkey (unknown role).
INSERT INTO core.role_grant (id, person_id, role_id, department_id, ends_at, granted_by, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
