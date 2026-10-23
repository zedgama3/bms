-- Store.ExportRecords: ExportPerson's "qualification_records". Use Query. Read-only. Complete.
-- Args: $1 person_id uuid.
-- Columns: id uuid, qualification_name text, acquired_on date, expires_on date NULL, verified
--   boolean, verified_at timestamptz NULL, certificate_file_id uuid NULL (files exports the
--   bytes itself).
-- Order: created_at, id. Served by record_person_idx.
SELECT r.id, q.name AS qualification_name, r.acquired_on, r.expires_on,
       (r.verified_at IS NOT NULL) AS verified, r.verified_at, r.certificate_file_id
FROM volunteers.qualification_record r
JOIN volunteers.qualification q ON q.id = r.qualification_id
WHERE r.person_id = $1::uuid
ORDER BY r.created_at, r.id
