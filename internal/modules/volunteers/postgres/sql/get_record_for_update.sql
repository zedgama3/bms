-- Store.GetRecordForUpdate: first statement of VerifyRecord, RejectRecord and RemoveRecord in
-- the Write transaction, so two leads cannot verify or reject the same record. No row =
-- NotFound.
-- Args: $1 id uuid. Columns: as get_record.sql.
-- The lock is on the record only (FOR UPDATE OF r), not the qualification.
SELECT r.id, r.qualification_id, r.person_id, r.acquired_on, r.expires_on, r.verified_by, r.verified_at,
       r.certificate_file_id, r.created_at, r.updated_at,
       q.department_id AS qualification_department_id
FROM volunteers.qualification_record r
JOIN volunteers.qualification q ON q.id = r.qualification_id
WHERE r.id = $1::uuid
FOR UPDATE OF r
