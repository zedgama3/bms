-- Store.ListQualifications: the catalogue, in the caller's view. Use Query. Read-only.
-- Active entries are visible to every signed-in person; archived ones only where the caller
-- holds qualifications.manage (site-wide for org-wide entries, at the department for a
-- department's). The permission filter is in the query; the use case decides the arguments
-- from authz (ExtentOf) and answers Permission itself when IncludeArchived is asked without
-- any extent.
-- Args: $1 department_id uuid (NULL = no department filter: org-wide plus every department),
--       $2 include_org_wide boolean (with $1 set: also the org-wide entries, for pickers),
--       $3 archived_org_wide boolean (caller may see archived org-wide entries),
--       $4 archived_departments uuid[] (departments whose archived entries the caller may see;
--          '{}' for none),
--       $5 limit integer (vol.MaxList = 500; the catalogue is small, so one bounded page).
-- Columns: the qualification columns (see insert_qualification.sql).
-- Order: lower(name), id (stable). Seq scan: the table is a few dozen rows.
SELECT id, department_id, name, description, validity_days, archived_at, created_at, updated_at
FROM volunteers.qualification
WHERE ($1::uuid IS NULL OR department_id = $1::uuid OR ($2::boolean AND department_id IS NULL))
  AND (archived_at IS NULL
       OR (department_id IS NULL AND $3::boolean)
       OR department_id = ANY($4::uuid[]))
ORDER BY lower(name), id
LIMIT $5::integer
