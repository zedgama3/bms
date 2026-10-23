-- Store.PendingRecords: the verification queue, oldest first. Use Query. Read-only.
-- A lead sees pending records of the qualifications they may verify: a department's at
-- verify@D, an org-wide one in any verify extent (D41; the use case calls this only when the
-- caller has an extent, else Permission). The caller's own records are left out (no
-- self-verification, F31). With $1 set, the org-wide ones are still listed (any lead may
-- verify them) next to that department's.
-- Args: $1 department_id uuid (NULL = every department the caller may verify),
--       $2 all_departments boolean (site-wide verify holder),
--       $3 departments uuid[] (the caller's verify extent),
--       $4 actor uuid (the caller, excluded as the record's person),
--       $5 limit integer (vol.MaxList).
-- Columns: the record columns (see insert_record.sql), then qualification_department_id uuid NULL.
-- Order: created_at, id. Served by record_pending_created_idx (partial: verified_at IS NULL).
SELECT r.id, r.qualification_id, r.person_id, r.acquired_on, r.expires_on, r.verified_by, r.verified_at,
       r.certificate_file_id, r.created_at, r.updated_at,
       q.department_id AS qualification_department_id
FROM volunteers.qualification_record r
JOIN volunteers.qualification q ON q.id = r.qualification_id
WHERE r.verified_at IS NULL
  AND r.person_id <> $4::uuid
  AND (q.department_id IS NULL
       OR (($2::boolean OR q.department_id = ANY($3::uuid[]))
           AND ($1::uuid IS NULL OR q.department_id = $1::uuid)))
ORDER BY r.created_at, r.id
LIMIT $5::integer
