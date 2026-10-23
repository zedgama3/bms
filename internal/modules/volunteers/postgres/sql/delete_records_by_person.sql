-- Store.DeleteRecordsByPerson: DeletePerson, after files.DeleteByRecord for each record.
-- Exec; count = RowsAffected(). Idempotent.
-- Args: $1 person_id uuid.
DELETE FROM volunteers.qualification_record
WHERE person_id = $1::uuid
