-- Record one applied file; run in the same transaction as the file itself (db.md decision 10).
-- Args: $1 set_name text, $2 version int4, $3 name text, $4 checksum text (lower-case hex
-- SHA-256), $5 duration_ms int4. A duplicate (set_name, version) is a unique violation (23505)
-- on constraint schema_migration_pkey, which is the right failure for a regular file.
INSERT INTO core.schema_migration (set_name, version, name, checksum, duration_ms)
VALUES ($1, $2, $3, $4, $5);
