-- Store.GetKind: one kind. Read-only; use Query (zero rows: NotFound).
-- Args: $1 key text. Columns, in order: as list_kinds.sql.
SELECT key, name, builtin, created_at, updated_at
FROM core.event_kind
WHERE key = $1
