-- Record the bootstrap file (set "db", version 1) if no row exists yet; the file itself is
-- idempotent and may have run before. Same args as insert_applied.sql. Affects 0 rows when
-- the row is already there.
INSERT INTO core.schema_migration (set_name, version, name, checksum, duration_ms)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (set_name, version) DO NOTHING;
