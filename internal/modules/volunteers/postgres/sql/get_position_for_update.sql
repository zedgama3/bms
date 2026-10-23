-- Store.GetPositionForUpdate: first statement of UpdatePosition, ArchivePosition and
-- SetRequirements (serialises two leads editing one position; decision "SetRequirements ...
-- take FOR UPDATE on their row"). No row = NotFound.
-- Args: $1 id uuid. Columns: the position columns (see insert_position.sql).
-- FOR NO KEY UPDATE: shifts' foreign-key checks on the position are not blocked.
SELECT id, department_id, name, description, access, hide_shift_mates, archived_at, created_at, updated_at
FROM volunteers.position
WHERE id = $1::uuid
FOR NO KEY UPDATE
