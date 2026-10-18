-- Store.InfoOf: the batch lookup behind Directory.InfoOf. Read-only; use Query.
-- Args: $1 ids uuid[] (Go rejects more than MaxInfo = 200 before calling; duplicates are
-- harmless). Columns, in order: id uuid, name text, kind text, starts_at timestamptz, ends_at
-- timestamptz, zone text, closed boolean (closed_at IS NOT NULL). Unknown IDs yield no row.
-- No ORDER BY: the result is a map.
SELECT id, name, kind, starts_at, ends_at, zone, closed_at IS NOT NULL AS closed
FROM core.event
WHERE id = ANY($1::uuid[])
