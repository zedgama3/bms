-- Store.VerifyRecord: marks a pending record verified, optionally correcting the dates. Use
-- QueryRow. Zero rows = the record is gone or already verified (the use case read it FOR
-- UPDATE first and answers Conflict status; zero rows after that is a race: Conflict status).
-- Args: $1 id uuid, $2 verified_by uuid, $3 verified_at timestamptz (also updated_at),
--       $4 acquired_on date (NULL = keep what the person entered),
--       $5 expires_on date (NULL = keep),
--       $6 clear_expiry boolean (VerifyInput.NoExpiry: set expires_on to NULL, the certificate
--          never expires; the use case refuses it together with a $5 date, Invalid one_of).
-- Columns: the record columns (see insert_record.sql).
-- Constraint errors: qualification_record_dates_chk (23514: Invalid range);
--   qualification_record_not_self_verified_chk (23514: Invalid, code self; the use case checks
--   first and answers Invalid self before reaching the database).
UPDATE volunteers.qualification_record
SET verified_by = $2::uuid,
    verified_at = $3::timestamptz,
    acquired_on = COALESCE($4::date, acquired_on),
    expires_on  = CASE WHEN $6::boolean THEN NULL ELSE COALESCE($5::date, expires_on) END,
    updated_at  = $3::timestamptz
WHERE id = $1::uuid
  AND verified_at IS NULL
RETURNING id, qualification_id, person_id, acquired_on, expires_on, verified_by, verified_at,
          certificate_file_id, created_at, updated_at
