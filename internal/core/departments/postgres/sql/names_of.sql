-- Store.NamesOf: the batch lookup behind Directory.NamesOf. Read-only; use Query. Archived
-- departments are included so history stays labelled.
-- Args: $1 ids uuid[] (Go rejects more than MaxNames = 200 before calling; duplicates are
-- harmless). Columns, in order: id uuid, name text. Unknown IDs yield no row. No ORDER BY: the
-- result is a map.
SELECT id, name
FROM core.department
WHERE id = ANY($1::uuid[])
