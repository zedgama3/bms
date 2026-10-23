-- Store.UpdatePosition. Use QueryRow. Zero rows = unknown or archived (the use case reads
-- first with get_position_for_update.sql).
-- Args: $1 id uuid, $2 name text, $3 description text, $4 access text, $5 hide_shift_mates
--       boolean, $6 updated_at timestamptz.
-- Columns: the position columns. Constraint errors: as insert_position.sql.
UPDATE volunteers.position
SET name = $2::text, description = $3::text, access = $4::text, hide_shift_mates = $5::boolean,
    updated_at = $6::timestamptz
WHERE id = $1::uuid
  AND archived_at IS NULL
RETURNING id, department_id, name, description, access, hide_shift_mates, archived_at, created_at, updated_at
