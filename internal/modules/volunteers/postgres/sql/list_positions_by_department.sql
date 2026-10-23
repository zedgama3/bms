-- Store.ListPositions. Use Query. Read-only. The use case decides $2 (archived ones need
-- positions.manage at the department; else Permission).
-- Args: $1 department_id uuid, $2 include_archived boolean, $3 limit integer (vol.MaxList).
-- Columns: the position columns (see insert_position.sql).
-- Order: lower(name), id. Served by position_name_key (leading column department_id).
SELECT id, department_id, name, description, access, hide_shift_mates, archived_at, created_at, updated_at
FROM volunteers.position
WHERE department_id = $1::uuid
  AND ($2::boolean OR archived_at IS NULL)
ORDER BY lower(name), id
LIMIT $3::integer
