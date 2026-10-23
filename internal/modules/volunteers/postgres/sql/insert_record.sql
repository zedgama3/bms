-- Store.InsertRecord: RecordQualification inserts it verified (verified_by and verified_at
-- set, no certificate); UploadCertificate inserts it pending (both NULL) and then calls
-- set_record_certificate.sql once files.Attach returned the FileID (Attach needs the record
-- ID). Use QueryRow. Runs in the use case's Write transaction.
-- Args: $1 id uuid, $2 qualification_id uuid, $3 person_id uuid, $4 acquired_on date,
--       $5 expires_on date (NULL = indefinite), $6 verified_by uuid (NULL = pending),
--       $7 verified_at timestamptz (NULL = pending), $8 created_at timestamptz (also updated_at).
-- Columns, in order (the "record columns"): id uuid, qualification_id uuid, person_id uuid,
--   acquired_on date, expires_on date NULL, verified_by uuid NULL, verified_at timestamptz NULL,
--   certificate_file_id uuid NULL, created_at timestamptz, updated_at timestamptz.
--   A record is verified when verified_at IS NOT NULL (verified_by is NULL for a record whose
--   verifier was deleted; see the Go type: use VerifiedAt, not VerifiedBy, for Verified()).
-- Constraint errors: record_pending_key (23505: Conflict, code pending_exists);
--   qualification_record_person_id_fkey (23503: Invalid, field person, code unknown);
--   qualification_record_qualification_id_fkey (23503: Invalid, field qualification, unknown);
--   qualification_record_dates_chk (23514: Invalid range on expires_on);
--   qualification_record_not_self_verified_chk, qualification_record_verified_chk (23514).
INSERT INTO volunteers.qualification_record
    (id, qualification_id, person_id, acquired_on, expires_on, verified_by, verified_at, created_at, updated_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::date, $5::date, $6::uuid, $7::timestamptz, $8::timestamptz, $8::timestamptz)
RETURNING id, qualification_id, person_id, acquired_on, expires_on, verified_by, verified_at,
          certificate_file_id, created_at, updated_at
