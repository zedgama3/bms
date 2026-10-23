-- Store.GetQualificationForUpdate: first statement of UpdateQualification and
-- ArchiveQualification inside the Write transaction. No row = NotFound.
-- Args: $1 id uuid. Columns: the qualification columns (see insert_qualification.sql).
-- FOR NO KEY UPDATE: does not block foreign-key checks (records, requirements) on the row.
SELECT id, department_id, name, description, validity_days, archived_at, created_at, updated_at
FROM volunteers.qualification
WHERE id = $1::uuid
FOR NO KEY UPDATE
