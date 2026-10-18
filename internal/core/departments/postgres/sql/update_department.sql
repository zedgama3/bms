-- Store.Update: a partial update. Use Query; zero rows: NotFound. Call after
-- get_department_for_update.sql has checked the department is active (Conflict archived).
-- Args: $1 id uuid, $2 name text, $3 description text, $4 contact_email text, $5 updated_at
-- timestamptz. $2, $3, $4: NULL leaves the column unchanged; '' sets it empty (valid for
-- description and contact_email; Go rejects an empty name before this runs).
-- Columns, in order: as get_department.sql.
-- Constraint errors: department_name_key (Conflict, name taken); the length and chars checks as
-- in insert_department.sql (Invalid).
UPDATE core.department SET
    name          = COALESCE($2::text, name),
    description   = COALESCE($3::text, description),
    contact_email = COALESCE($4::text, contact_email),
    updated_at    = $5::timestamptz
WHERE id = $1
RETURNING id, name, description, contact_email, archived_at, created_at, updated_at
