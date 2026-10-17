-- Does core.schema_migration exist? One bool row. Status and CheckSchema read this first so an
-- empty database is "nothing applied" rather than 42P01 (db.md decision 18).
SELECT to_regclass('core.schema_migration') IS NOT NULL AS present;
