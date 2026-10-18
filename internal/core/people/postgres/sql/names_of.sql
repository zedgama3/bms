-- Store.NamesOf: the batch lookup behind Directory.NamesOf and GET /names. Read-only; use
-- Query. Internal level: the playa name only, no personal column is named here.
-- Args: $1 ids uuid[] (Go rejects more than MaxNames = 200 before calling; duplicates are
-- harmless). Columns, in order: id uuid, playa_name text. Unknown or deleted IDs yield no
-- row (the use case omits them); an unfilled name is ''. No ORDER BY: the result is a map.
SELECT id, playa_name
FROM core.person
WHERE id = ANY($1::uuid[])
