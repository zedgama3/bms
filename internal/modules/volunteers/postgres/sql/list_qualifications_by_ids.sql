-- Store.QualificationsByIDs: SetRequirements validates each ID (exists, active, org-wide or
-- the position's department) and the shift/eligibility code names missing qualifications.
-- Use Query. Read-only. No ORDER BY: the result is matched by ID in Go.
-- Args: $1 ids uuid[] (Go rejects more than vol.MaxIDs = 200; unknown IDs yield no row).
-- Columns: the qualification columns (see insert_qualification.sql).
-- Served by qualification_pkey.
SELECT id, department_id, name, description, validity_days, archived_at, created_at, updated_at
FROM volunteers.qualification
WHERE id = ANY($1::uuid[])
