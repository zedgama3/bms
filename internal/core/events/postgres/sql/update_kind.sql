-- Store.UpdateKind: rename. The key is immutable and is not in this statement; builtin kinds
-- may be renamed. Use Query; zero rows: NotFound.
-- Args: $1 key text, $2 name text, $3 updated_at timestamptz.
-- Columns, in order: as list_kinds.sql.
-- Constraint errors: event_kind_name_length_chk, event_kind_name_chars_chk (Invalid, name).
UPDATE core.event_kind SET name = $2, updated_at = $3
WHERE key = $1
RETURNING key, name, builtin, created_at, updated_at
