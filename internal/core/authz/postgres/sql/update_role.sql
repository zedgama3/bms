-- Store.UpdateRole: name and description only; scope, key and base are immutable and are not
-- in this statement. Use Exec; RowsAffected 0 means NotFound.
-- Args: $1 id uuid, $2 name text, $3 description text, $4 updated_at timestamptz.
-- Constraint errors: role_name_key (Conflict, name taken); role_name_required_chk (a regional's
-- own role cannot be unnamed: Invalid, name required); the length checks (Invalid).
UPDATE core.role
SET name = $2, description = $3, updated_at = $4
WHERE id = $1
