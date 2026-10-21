-- Store.Delete: delete one of the owner's files and write its tombstone, in ONE statement,
-- so there is no state in which the row is gone and nothing remembers the object (files.md
-- decision 16). Runs inside the caller's Write transaction. Use Query; the deleted row (if
-- any) is returned for the audit entry (files.file.deleted) and for AfterCommit.
-- Args: $1 owner text, $2 id uuid, $3 now timestamptz (the tombstone's created_at, the
--       service clock).
-- Columns, in order: the file columns (see get_file.sql), then tombstone_id uuid (the id of
-- the tombstone row written, generated here with gen_random_uuid(); AfterCommit deletes the
-- object (store, bucket, object_key), then delete_tombstone.sql with this id).
-- ZERO rows: unknown ID or another owner's file -> NotFound; nothing was written.
-- Constraint errors: file_tombstone_store_bucket_key_uidx / file_tombstone_pkey (23505): a
-- tombstone for this object already exists, which cannot happen for a live row -> Internal.
-- Served by file_pkey; the tombstone insert is by key. Fails 25006 (db.ErrReadOnly) on a read-only copy.
WITH gone AS (
    DELETE FROM core.file
    WHERE owner = $1::text
      AND id = $2::uuid
    RETURNING id, owner, record_kind, record_id, subject_person_id, uploaded_by, level, name,
              content_type, size_bytes, sha256, store, bucket, object_key, stripped, created_at,
              gen_random_uuid() AS tombstone_id
), tomb AS (
    INSERT INTO core.file_tombstone (id, store, bucket, object_key, created_at)
    SELECT tombstone_id, store, bucket, object_key, $3::timestamptz
    FROM gone
    RETURNING 1
)
SELECT id, owner, record_kind, record_id, subject_person_id, uploaded_by, level, name,
       content_type, size_bytes, sha256, store, bucket, object_key, stripped, created_at,
       tombstone_id
FROM gone
