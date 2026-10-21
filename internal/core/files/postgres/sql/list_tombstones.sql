-- Store.Tombstones (files.Reap): outstanding tombstones for the store this process is
-- configured for, oldest first, in batches. Keyset on (created_at, id) ascending: Reap
-- moves the cursor past a tombstone whose delete failed, so a stuck one at the head cannot
-- starve the rest; run until fewer rows than the limit come back. Use Query, on the writing
-- copy. Only this process's store is listed: objects.Delete on another store would find
-- nothing and the tombstone would be removed while the object lives on.
-- Args: $1 store text (objects.Kind of the configured store), $2 after_created_at
--       timestamptz, $3 after_id uuid (the last row of the previous batch; both NULL for the
--       first batch), $4 limit integer (1000).
-- Columns, in order: id uuid, store text, bucket text, object_key text, created_at
-- timestamptz, attempts integer. Sorted by created_at, id.
-- Served by file_tombstone_created_idx (filtered by store). The cursor is spelled as in
-- audit's export_entries_by_subject.sql. Read-only statement, but Reap's writes follow it.
-- Constraint errors: none reachable.
SELECT id, store, bucket, object_key, created_at, attempts
FROM core.file_tombstone
WHERE store = $1::text
  AND created_at >= COALESCE($2::timestamptz, '-infinity'::timestamptz)
  AND (created_at > COALESCE($2::timestamptz, '-infinity'::timestamptz)
       OR id > COALESCE($3::uuid, '00000000-0000-0000-0000-000000000000'::uuid))
ORDER BY created_at, id
LIMIT $4::integer
