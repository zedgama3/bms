-- Store.UnknownKeys (files.SweepOrphans): of a batch of object keys the store listed for one
-- bucket, answer the ones that ARE a core.file row or a tombstone. The caller subtracts them
-- from the batch: the rest are orphans (a crash between Put and Attach), which the sweep
-- deletes once older than OrphanAge (the age test is the caller's, on the listing's
-- Modified). Call it with batches of at most 1000 keys. A file's row and its tombstone are
-- written in one statement, so an object is always known to at least one of the two until
-- Reap has deleted it. Use Query, on the writing copy. Asked this way round so that a
-- database that holds nothing answers "no key is known" and the sweep tests need no rows.
-- Args: $1 store text, $2 bucket text, $3 keys text[] (listed keys; ProbeKey is skipped by
--       the caller).
-- Columns, in order, sorted: object_key text. Zero rows: no key is known.
-- Served by file_store_bucket_key_uidx and file_tombstone_store_bucket_key_uidx (one probe
-- per key). Constraint errors: none reachable.
SELECT k.object_key
FROM unnest($3::text[]) AS k(object_key)
WHERE EXISTS (
          SELECT 1 FROM core.file f
          WHERE f.store = $1::text AND f.bucket = $2::text AND f.object_key = k.object_key)
   OR EXISTS (
          SELECT 1 FROM core.file_tombstone t
          WHERE t.store = $1::text AND t.bucket = $2::text AND t.object_key = k.object_key)
ORDER BY k.object_key
