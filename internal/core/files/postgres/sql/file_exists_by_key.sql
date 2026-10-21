-- Store.Attached: is this Staged object attached? Two callers: Discard deletes the object only
-- when no row refers to it (QueryRow outside any transaction: a read, any copy), and Attach
-- with Replace refuses a Staged that is already attached before it deletes the record's rows
-- (QueryRow inside the owner's Write transaction).
-- Args: $1 store text, $2 bucket text (the owner's), $3 object_key text.
-- Columns, in order: attached boolean.
-- Served by file_store_bucket_key_uidx (index-only). Constraint errors: none reachable.
SELECT EXISTS (
    SELECT 1
    FROM core.file
    WHERE store = $1::text
      AND bucket = $2::text
      AND object_key = $3::text
) AS attached
