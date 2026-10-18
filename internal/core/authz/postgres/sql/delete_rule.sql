-- Store.DeleteRule. Use Query (zero rows: NotFound). Args: $1 id uuid. Columns: id uuid.
DELETE FROM core.sod_rule
WHERE id = $1
RETURNING id
