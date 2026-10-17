-- Set "db" (the migrator's own bootstrap set), version 0001. Idempotent throughout: Migrate
-- runs it when core.schema_migration does not exist yet, and records it afterwards.
-- No column holds personal data. Retention: forever (db.md "Data").
CREATE SCHEMA IF NOT EXISTS core;

CREATE TABLE IF NOT EXISTS core.schema_migration (
    set_name    text        NOT NULL,
    version     integer     NOT NULL CHECK (version > 0),
    name        text        NOT NULL,          -- the file name without number and suffix
    checksum    text        NOT NULL,          -- SHA-256 of the file's bytes, lower-case hex
    duration_ms integer     NOT NULL CHECK (duration_ms >= 0),
    applied_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (set_name, version)
);

COMMENT ON TABLE core.schema_migration IS 'one row per applied migration file; append-only, kept forever; no personal data';
