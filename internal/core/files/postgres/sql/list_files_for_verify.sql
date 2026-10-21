-- Store.VerifyPage (files.Verify): every core.file row, in batches by ID (keyset on id,
-- ascending; run until fewer rows than the limit come back). The caller heads each object in
-- (store, bucket, object_key) and compares size and sha256; rows whose store is not the
-- process's cannot be headed and are counted by the caller as skipped, not missing. Use
-- Query, outside any transaction, on any copy. Owner-unscoped: an operator command.
-- Args: $1 after_id uuid (the last id of the previous batch; NULL for the first), $2 limit
--       integer (1000).
-- Columns, in order, sorted by id: id uuid, store text, bucket text, object_key text,
-- size_bytes bigint, sha256 bytea. Never the name.
-- Served by file_pkey. Constraint errors: none reachable.
SELECT id, store, bucket, object_key, size_bytes, sha256
FROM core.file
WHERE id > COALESCE($1::uuid, '00000000-0000-0000-0000-000000000000'::uuid)
ORDER BY id
LIMIT $2::integer
