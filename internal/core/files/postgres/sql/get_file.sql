-- Store.Get / Store.Open / Delete's pre-read: one file's metadata, only if it belongs to the
-- owner. Use QueryRow. No row (pgx.ErrNoRows -> NotFound) for an unknown ID and for another
-- owner's file alike, so an ID is never confirmed across owners.
-- Args: $1 owner text, $2 id uuid.
-- Columns, in order (the "file columns", the same in every statement that returns a file):
--   id uuid, owner text, record_kind text, record_id uuid, subject_person_id uuid (NULL:
--   nobody), uploaded_by uuid (NULL: none), level smallint, name text, content_type text,
--   size_bytes bigint, sha256 bytea (32 bytes), store text, bucket text, object_key text,
--   stripped boolean, created_at timestamptz.
-- Served by file_pkey. Read-only: runs on any copy.
-- Constraint errors: none reachable.
SELECT id, owner, record_kind, record_id, subject_person_id, uploaded_by, level, name,
       content_type, size_bytes, sha256, store, bucket, object_key, stripped, created_at
FROM core.file
WHERE owner = $1::text
  AND id = $2::uuid
