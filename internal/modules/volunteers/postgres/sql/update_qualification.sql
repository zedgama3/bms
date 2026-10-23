-- Store.UpdateQualification: name, description, validity; never the department. Use QueryRow.
-- Zero rows = unknown or archived (the use case reads first with get_qualification_for_update
-- and answers NotFound or Conflict archived; zero rows here after that is a race: NotFound).
-- Args: $1 id uuid, $2 name text, $3 description text, $4 validity_days integer (NULL = none),
--       $5 updated_at timestamptz.
-- Columns: the qualification columns (see insert_qualification.sql).
-- Constraint errors: as insert_qualification.sql.
UPDATE volunteers.qualification
SET name = $2::text, description = $3::text, validity_days = $4::integer, updated_at = $5::timestamptz
WHERE id = $1::uuid
  AND archived_at IS NULL
RETURNING id, department_id, name, description, validity_days, archived_at, created_at, updated_at
