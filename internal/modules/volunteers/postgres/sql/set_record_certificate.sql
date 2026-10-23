-- Store.SetRecordCertificate: UploadCertificate, after files.Attach returned the FileID, in the
-- same Write transaction. Use QueryRow; zero rows = NotFound.
-- Args: $1 id uuid, $2 certificate_file_id uuid, $3 updated_at timestamptz.
-- Columns: the record columns (see insert_record.sql).
UPDATE volunteers.qualification_record
SET certificate_file_id = $2::uuid, updated_at = $3::timestamptz
WHERE id = $1::uuid
RETURNING id, qualification_id, person_id, acquired_on, expires_on, verified_by, verified_at,
          certificate_file_id, created_at, updated_at
