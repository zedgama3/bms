-- Store.ByRecord: a record's files, oldest first. Use Query. Always bounded: a record cannot
-- hold more than files.MaxFilesPerRecordCeiling (200) files through Attach, and the caller
-- passes that ceiling as the limit, so the list is one page by construction.
-- Args: $1 owner text, $2 record_kind text, $3 record_id uuid, $4 limit integer
--       (MaxFilesPerRecordCeiling).
-- Columns, in order: the file columns (see get_file.sql). Sorted by created_at, id (stable).
-- Zero rows for a record without files.
-- Served by file_record_idx (index scan, already in order; no sort). Read-only: any copy.
-- Constraint errors: none reachable.
SELECT id, owner, record_kind, record_id, subject_person_id, uploaded_by, level, name,
       content_type, size_bytes, sha256, store, bucket, object_key, stripped, created_at
FROM core.file
WHERE owner = $1::text
  AND record_kind = $2::text
  AND record_id = $3::uuid
ORDER BY created_at, id
LIMIT $4::integer
