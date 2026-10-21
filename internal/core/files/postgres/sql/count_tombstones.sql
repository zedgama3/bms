-- Store.CountTombstones: the gauge bms.files.tombstones, Reap's "stuck" warning and
-- Verified.Tombstones. Use QueryRow, any copy. Counts every store, not only this process's:
-- an outstanding tombstone in another store is still outstanding.
-- Args: $1 stuck_attempts integer (24: the attempts at or above which a tombstone is stuck).
-- Columns, in order: outstanding bigint, stuck bigint (attempts >= $1).
-- Seq scan of a table that is empty or near-empty in a healthy installation.
-- Constraint errors: none reachable.
SELECT count(*)::bigint AS outstanding,
       (count(*) FILTER (WHERE attempts >= $1::integer))::bigint AS stuck
FROM core.file_tombstone
