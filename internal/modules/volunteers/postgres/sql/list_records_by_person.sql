-- Store.RecordsOf and the eligibility load: a person's records, filtered to what the caller
-- may see. Use Query. Read-only: any copy.
-- Args: $1 person_id uuid,
--       $2 all_departments boolean (true for the person themself, or a site-wide
--          qualifications.read/verify holder: no department filter),
--       $3 departments uuid[] (the caller's qualifications.read extent; org-wide qualifications
--          are always included),
--       $4 limit integer (vol.MaxList).
-- Columns: the record columns (see insert_record.sql), then qualification_department_id uuid NULL.
-- Order: created_at, id (stable). Served by record_person_idx.
SELECT r.id, r.qualification_id, r.person_id, r.acquired_on, r.expires_on, r.verified_by, r.verified_at,
       r.certificate_file_id, r.created_at, r.updated_at,
       q.department_id AS qualification_department_id
FROM volunteers.qualification_record r
JOIN volunteers.qualification q ON q.id = r.qualification_id
WHERE r.person_id = $1::uuid
  AND ($2::boolean OR q.department_id IS NULL OR q.department_id = ANY($3::uuid[]))
ORDER BY r.created_at, r.id
LIMIT $4::integer
