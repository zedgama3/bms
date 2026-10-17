-- Applied files of one set, for CheckSchema. Args: $1 set_name text.
-- Columns: version int4, name text, checksum text. Uses the primary key (set_name, version).
SELECT version, name, checksum
FROM core.schema_migration
WHERE set_name = $1
ORDER BY version;
