-- Every applied file, for Migrate's and Status's comparison with the embedded sets.
-- Columns: set_name text, version int4, name text, checksum text. No args.
SELECT set_name, version, name, checksum
FROM core.schema_migration
ORDER BY set_name, version;
