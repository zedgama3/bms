-- Store.ListKinds: all kinds. Read-only; use Query. Not paginated (a handful of rows).
-- Args: none.
-- Columns, in order: key text, name text, builtin boolean, created_at timestamptz,
-- updated_at timestamptz. Builtin kinds first, then by key (stable).
SELECT key, name, builtin, created_at, updated_at
FROM core.event_kind
ORDER BY builtin DESC, key
