-- Store.InsertKind: CreateKind. A regional's own kind: builtin is always false (only migration
-- 0001 sets it). Use Query. Inside the Write transaction.
-- Args: $1 key text, $2 name text, $3 created_at timestamptz (also updated_at).
-- Columns, in order: as list_kinds.sql.
-- Constraint errors (*db.Error.Constraint): event_kind_pkey (Conflict, key taken);
-- event_kind_key_chk (Invalid, key malformed); event_kind_name_length_chk,
-- event_kind_name_chars_chk (Invalid, name).
INSERT INTO core.event_kind (key, name, builtin, created_at, updated_at)
VALUES ($1, $2, false, $3, $3)
RETURNING key, name, builtin, created_at, updated_at
