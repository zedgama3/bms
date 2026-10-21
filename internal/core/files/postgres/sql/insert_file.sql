-- Store.Insert (files.Store.Attach): bind one Staged object to a record. Runs inside the
-- caller's Write transaction, after delete_files_by_record.sql when Attachment.Replace is
-- set (the count below then sees the deletions). Use QueryRow (exactly one row comes back).
-- The per-record count and the quota are checked IN this statement, from one snapshot, so
-- there is no gap between the check and the insert within the statement; two concurrent
-- Attach calls may still overshoot by one file, which the spec accepts (files.md, Attach 4).
-- Nothing is inserted when a limit would be exceeded; the flags below say which.
-- Args:
--   $1  id                uuid        FileID (= the Staged key)
--   $2  owner             text        the scoped owner (= bucket)
--   $3  record_kind       text        audit.Ref.Kind; its area must equal $2
--   $4  record_id         uuid
--   $5  subject_person_id uuid        NULL for nobody (the zero PersonID must be sent as NULL)
--   $6  uploaded_by       uuid        NULL for no actor (same)
--   $7  level             smallint    model.Sensitivity, 1..4
--   $8  name              text
--   $9  content_type      text
--   $10 size_bytes        bigint      > 0
--   $11 sha256            bytea       32 bytes
--   $12 store             text        'disk' | 's3'
--   $13 bucket            text
--   $14 object_key        text
--   $15 stripped          boolean
--   $16 created_at        timestamptz the service clock
--   $17 max_per_record    integer     Policy.MaxPerRecord
--   $18 quota_bytes       bigint      Policy.QuotaBytes; 0 = unlimited (the total is then not read)
-- Columns, in order: inserted boolean (the row exists now), too_many boolean (the record
-- already holds max_per_record files), over_quota boolean (the total plus size_bytes would
-- exceed quota_bytes). When inserted is false at least one flag is true: report too_many
-- first (Conflict too_many {max}), else over_quota (Conflict quota {max}).
-- Constraint errors (*db.Error.Constraint):
--   file_store_bucket_key_uidx      23505 Conflict: the same Staged attached twice -> Internal
--   file_pkey                       23505 Conflict: a duplicate id -> Internal
--   file_subject_person_id_fkey     23503 Invalid: no such person -> Invalid subject unknown
--   file_uploaded_by_fkey           23503 Invalid: the actor is not a person -> Internal
--   file_owner_area_chk, file_owner_chk, file_record_kind_chk, file_level_chk, file_name_chk,
--   file_content_type_chk, file_size_chk, file_sha256_chk, file_store_chk, file_bucket_chk,
--   file_object_key_chk, file_nil_uuid_chk   23514 Invalid: a caller bug (Validate runs first)
-- Served by file_record_idx (the record count); the quota total is a scan of core.file read
-- only when quota_bytes > 0. Fails 25006 (db.ErrReadOnly) on a read-only copy.
WITH here AS (
    SELECT count(*) AS n
    FROM core.file
    WHERE owner = $2::text
      AND record_kind = $3::text
      AND record_id = $4::uuid
), used AS (
    SELECT CASE WHEN $18::bigint > 0
                THEN (SELECT coalesce(sum(size_bytes), 0) FROM core.file)
                ELSE 0 END AS bytes
), room AS (
    SELECT here.n < $17::integer AS count_ok,
           ($18::bigint = 0 OR used.bytes + $10::bigint <= $18::bigint) AS quota_ok
    FROM here, used
), ins AS (
    INSERT INTO core.file
        (id, owner, record_kind, record_id, subject_person_id, uploaded_by, level, name,
         content_type, size_bytes, sha256, store, bucket, object_key, stripped, created_at)
    SELECT $1::uuid, $2::text, $3::text, $4::uuid, $5::uuid, $6::uuid, $7::smallint, $8::text,
           $9::text, $10::bigint, $11::bytea, $12::text, $13::text, $14::text, $15::boolean,
           $16::timestamptz
    FROM room
    WHERE room.count_ok AND room.quota_ok
    RETURNING id
)
SELECT EXISTS (SELECT 1 FROM ins) AS inserted,
       NOT room.count_ok AS too_many,
       NOT room.quota_ok AS over_quota
FROM room
