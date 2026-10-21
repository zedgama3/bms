-- Store.Usage: what is stored in total, for the advisory quota pre-check in Stage (Attach
-- does the enforcing check inside insert_file.sql). Call it only when Policy.QuotaBytes > 0.
-- Use QueryRow, outside any transaction (a read; any copy).
-- Args: none.
-- Columns, in order: files bigint (rows), bytes bigint (sum of size_bytes; 0 when empty).
-- A scan of core.file (an installation holds thousands of files, not millions; measured in
-- the report). Constraint errors: none reachable.
SELECT count(*)::bigint AS files,
       coalesce(sum(size_bytes), 0)::bigint AS bytes
FROM core.file
