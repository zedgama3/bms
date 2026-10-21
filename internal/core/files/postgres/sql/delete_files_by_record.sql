-- Store.DeleteByRecord, and Attach with Replace: delete every one of the owner's files of a
-- record and write a tombstone for each, in ONE statement (see delete_file.sql). Runs inside
-- the caller's Write transaction. Use Query; the deleted rows are returned for the audit
-- entries (one files.file.deleted each) and for AfterCommit. Zero rows is fine (count 0).
-- Args: $1 owner text, $2 record_kind text, $3 record_id uuid, $4 now timestamptz (the
--       tombstones' created_at, the service clock).
-- Columns, in order: the file columns (see get_file.sql), then tombstone_id uuid. Sorted by
-- created_at, id.
-- Constraint errors: as delete_file.sql.
-- Served by file_record_idx. Fails 25006 (db.ErrReadOnly) on a read-only copy.
WITH gone AS (
    DELETE FROM core.file
    WHERE owner = $1::text
      AND record_kind = $2::text
      AND record_id = $3::uuid
    RETURNING id, owner, record_kind, record_id, subject_person_id, uploaded_by, level, name,
              content_type, size_bytes, sha256, store, bucket, object_key, stripped, created_at,
              gen_random_uuid() AS tombstone_id
), tomb AS (
    INSERT INTO core.file_tombstone (id, store, bucket, object_key, created_at)
    SELECT tombstone_id, store, bucket, object_key, $4::timestamptz
    FROM gone
    RETURNING 1
)
SELECT id, owner, record_kind, record_id, subject_person_id, uploaded_by, level, name,
       content_type, size_bytes, sha256, store, bucket, object_key, stripped, created_at,
       tombstone_id
FROM gone
ORDER BY created_at, id
