-- Store.ArchiveQualification. Use QueryRow. Zero rows = unknown or already archived (the use
-- case reads first and answers NotFound or Conflict archived).
-- Args: $1 id uuid, $2 at timestamptz (archived_at and updated_at).
-- Columns: the qualification columns (see insert_qualification.sql).
UPDATE volunteers.qualification
SET archived_at = $2::timestamptz, updated_at = $2::timestamptz
WHERE id = $1::uuid
  AND archived_at IS NULL
RETURNING id, department_id, name, description, validity_days, archived_at, created_at, updated_at
