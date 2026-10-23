-- Store.GetPosition. Use QueryRow; no row = NotFound. Read-only: any copy.
-- Args: $1 id uuid. Columns: the position columns (see insert_position.sql).
-- Served by position_pkey.
SELECT id, department_id, name, description, access, hide_shift_mates, archived_at, created_at, updated_at
FROM volunteers.position
WHERE id = $1::uuid
