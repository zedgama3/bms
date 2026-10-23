-- Store.GetRecord: one record plus the department of its qualification (for the permission
-- check: AllowIn at that department, or ExtentOf for an org-wide one). Use QueryRow; no row =
-- NotFound. Read-only: any copy.
-- Args: $1 id uuid.
-- Columns: the record columns (see insert_record.sql), then qualification_department_id uuid
--   NULL (NULL = the qualification is org-wide).
-- Served by qualification_record_pkey and qualification_pkey.
SELECT r.id, r.qualification_id, r.person_id, r.acquired_on, r.expires_on, r.verified_by, r.verified_at,
       r.certificate_file_id, r.created_at, r.updated_at,
       q.department_id AS qualification_department_id
FROM volunteers.qualification_record r
JOIN volunteers.qualification q ON q.id = r.qualification_id
WHERE r.id = $1::uuid
