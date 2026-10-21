-- Store.Buckets (files.SweepOrphans): the buckets that hold or recently held objects in one
-- store, so the sweep knows which to List. (The service adds every provisioned owner; a
-- bucket with no row and no tombstone has nothing the sweep could mistake for known.)
-- Use Query, any copy.
-- Args: $1 store text.
-- Columns, in order, sorted: bucket text.
-- The distinct buckets of core.file are found by a loose index scan (a recursive step that
-- jumps to the next bucket through file_store_bucket_key_uidx), so the cost is a few index
-- probes per bucket, not a read of every file row. The tombstone table is read whole: it is
-- empty or small unless a store has been down for a long time.
-- Constraint errors: none reachable.
WITH RECURSIVE file_buckets(bucket) AS (
    (SELECT bucket
     FROM core.file
     WHERE store = $1::text
     ORDER BY bucket
     LIMIT 1)
    UNION ALL
    SELECT (SELECT f.bucket
            FROM core.file f
            WHERE f.store = $1::text
              AND f.bucket > b.bucket
            ORDER BY f.bucket
            LIMIT 1)
    FROM file_buckets b
    WHERE b.bucket IS NOT NULL
)
SELECT bucket FROM file_buckets WHERE bucket IS NOT NULL
UNION
SELECT bucket FROM core.file_tombstone WHERE store = $1::text
ORDER BY bucket
