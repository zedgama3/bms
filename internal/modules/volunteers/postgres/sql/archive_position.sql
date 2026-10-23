-- Store.ArchivePosition: after count_future_shifts_by_position.sql returned 0 inside the same
-- Write transaction (and get_position_for_update.sql locked the row). Use QueryRow. Zero rows =
-- unknown or already archived.
-- Args: $1 id uuid, $2 at timestamptz. Columns: the position columns.
UPDATE volunteers.position
SET archived_at = $2::timestamptz, updated_at = $2::timestamptz
WHERE id = $1::uuid
  AND archived_at IS NULL
RETURNING id, department_id, name, description, access, hide_shift_mates, archived_at, created_at, updated_at
