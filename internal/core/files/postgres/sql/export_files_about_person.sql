-- Store.ExportAbout (files.ExportPerson, the "about" list): every file whose subject is the
-- person, across all owners, oldest first, in batches (keyset on (created_at, id),
-- ascending; run until fewer rows than the limit come back). Use Query, outside any
-- transaction. The caller reads each object (Get) for the base64 content; metadata only here.
-- Args: $1 person_id uuid (required), $2 after_created_at timestamptz, $3 after_id uuid (the
--       last row of the previous batch; both NULL for the first batch), $4 limit integer.
-- Columns, in order: the file columns (see get_file.sql). Sorted by created_at, id.
-- Served by file_subject_idx (a person's rows are few; the sort is over them only). The
-- cursor is spelled as in audit's export_entries_by_subject.sql. Read-only: any copy.
-- Constraint errors: none reachable.
SELECT id, owner, record_kind, record_id, subject_person_id, uploaded_by, level, name,
       content_type, size_bytes, sha256, store, bucket, object_key, stripped, created_at
FROM core.file
WHERE subject_person_id = $1::uuid
  AND created_at >= COALESCE($2::timestamptz, '-infinity'::timestamptz)
  AND (created_at > COALESCE($2::timestamptz, '-infinity'::timestamptz)
       OR id > COALESCE($3::uuid, '00000000-0000-0000-0000-000000000000'::uuid))
ORDER BY created_at, id
LIMIT $4::integer
