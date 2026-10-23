-- Store.GetQualification. Use QueryRow; no row = NotFound. Read-only: any copy.
-- Args: $1 id uuid.
-- Columns: the qualification columns (see insert_qualification.sql).
-- Served by qualification_pkey.
SELECT id, department_id, name, description, validity_days, archived_at, created_at, updated_at
FROM volunteers.qualification
WHERE id = $1::uuid
