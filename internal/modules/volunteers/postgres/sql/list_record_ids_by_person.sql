-- Store.RecordIDsOf: DeletePerson: the person's records, to call files.DeleteByRecord for each
-- before delete_records_by_person.sql. Use Query. Complete (a person holds a handful).
-- Args: $1 person_id uuid. Columns: id uuid, certificate_file_id uuid NULL. Order: id.
-- Served by record_person_idx.
SELECT id, certificate_file_id
FROM volunteers.qualification_record
WHERE person_id = $1::uuid
ORDER BY id
