-- Store.TouchPosition: SetRequirements bumps updated_at after replacing the set. Use QueryRow;
-- zero rows = NotFound.
-- Args: $1 id uuid, $2 updated_at timestamptz. Columns: the position columns.
UPDATE volunteers.position
SET updated_at = $2::timestamptz
WHERE id = $1::uuid
RETURNING id, department_id, name, description, access, hide_shift_mates, archived_at, created_at, updated_at
