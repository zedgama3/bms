-- Store.ExportUploaded (files.ExportPerson, the "uploaded" list): every file the person
-- uploaded whose subject is somebody else or nobody (a file about the person themself is in
-- the "about" list, export_files_about_person.sql), across all owners, oldest first, in
-- batches (keyset as export_files_about_person.sql). Use Query, outside any transaction.
-- Metadata only: the caller must not read the objects.
-- Args: $1 person_id uuid (required), $2 after_created_at timestamptz, $3 after_id uuid
--       (both NULL for the first batch), $4 limit integer.
-- Columns, in order: the file columns (see get_file.sql). Sorted by created_at, id.
-- Served by file_uploaded_by_idx. Read-only: any copy. Constraint errors: none reachable.
SELECT id, owner, record_kind, record_id, subject_person_id, uploaded_by, level, name,
       content_type, size_bytes, sha256, store, bucket, object_key, stripped, created_at
FROM core.file
WHERE uploaded_by = $1::uuid
  AND subject_person_id IS DISTINCT FROM $1::uuid
  AND created_at >= COALESCE($2::timestamptz, '-infinity'::timestamptz)
  AND (created_at > COALESCE($2::timestamptz, '-infinity'::timestamptz)
       OR id > COALESCE($3::uuid, '00000000-0000-0000-0000-000000000000'::uuid))
ORDER BY created_at, id
LIMIT $4::integer
