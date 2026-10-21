-- files 0001: the attachment metadata (core.file) and the pending object deletions
-- (core.file_tombstone) (files.md, "Data"). Schema "core" is created by the migrator.
-- Creates two new tables and touches no existing object, so it is safe on any database (both
-- are empty, so the index builds take no noticeable lock). The one reference into another
-- set's table is core.person (people runs first in db's fixed set order: identity, people,
-- authz, departments, events, audit, settings, jobs, notify, files).
--
-- No file contents anywhere (D43): no bytea column holds bytes. The bytes live in the object
-- store (internal/core/objects: local disk or an S3-compatible bucket); a core.file row says
-- who owns the file, what it is about, how sensitive it is, and where (store, bucket,
-- object_key) the object is.
--
-- Personal data. core.file.name is personal when the file is (a certificate is often named
-- after its holder); the other columns are IDs, tokens and sizes. subject_person_id and
-- uploaded_by are person IDs (pseudonymous). Retention (files.md decision 15, "Data"): a row
-- lives as long as its owning record. The owner's retention job deletes the record and calls
-- DeleteByRecord (delete_files_by_record.sql), which removes the row and writes the tombstone
-- in one statement; there is no files.retention key. A person's rows are handled by the
-- owners' DeletePerson first and by files' DeletePerson as a backstop
-- (anonymise_uploaded_by.sql clears uploaded_by; count_files_about_person.sql refuses with
-- Conflict in_use if a subject file remains), because both foreign keys to core.person are
-- RESTRICT. A tombstone holds no personal data (a store, a bucket and a key, which is a file
-- ID) and lives until its object is confirmed gone (Reap).
--
-- Timestamps come from the service's clock, so none has a default. Per the spec core.file has
-- no updated_at (rows are immutable: replace = delete + create; the one exception is
-- uploaded_by being cleared when the uploader is deleted) and core.file_tombstone has none
-- either (its only change is attempts, which Reap increments).

CREATE TABLE core.file (
    id                uuid        NOT NULL,
    -- The area of record_kind, and the bucket in M4. A Scoped files.Store filters every
    -- statement by it, so a module can never see another's files (files.md decision 1).
    owner             text        NOT NULL,
    -- audit.Ref.Kind: "<area>.<ident>". The area must be the owner (file_owner_area_chk).
    record_kind       text        NOT NULL,
    record_id         uuid        NOT NULL,
    -- Whom the file is about; NULL for nobody in particular.
    subject_person_id uuid,
    -- Who uploaded it; NULL for an operator command and after the uploader is deleted.
    uploaded_by       uuid,
    -- model.Sensitivity: 1 public, 2 internal, 3 personal, 4 sensitive.
    level             smallint    NOT NULL,
    -- The cleaned base name with the kind's extension.
    name              text        NOT NULL,
    -- The type sniffed from the bytes at upload, never the declared one.
    content_type      text        NOT NULL,
    size_bytes        bigint      NOT NULL,
    -- 32 bytes; the ETag, and what Verify compares with the object's checksum.
    sha256            bytea       NOT NULL,
    -- objects.Kind of the store that holds the object: disk or s3.
    store             text        NOT NULL,
    -- = owner in M4; a column so that a later move can rename.
    bucket            text        NOT NULL,
    -- = id as text in M4.
    object_key        text        NOT NULL,
    -- Image metadata was removed at upload.
    stripped          boolean     NOT NULL,
    created_at        timestamptz NOT NULL,
    CONSTRAINT file_pkey PRIMARY KEY (id),
    CONSTRAINT file_subject_person_id_fkey FOREIGN KEY (subject_person_id)
        REFERENCES core.person (id) ON DELETE RESTRICT,
    CONSTRAINT file_uploaded_by_fkey FOREIGN KEY (uploaded_by)
        REFERENCES core.person (id) ON DELETE RESTRICT,
    -- The all-zero UUID means "none" in Go; it must never be stored.
    CONSTRAINT file_nil_uuid_chk CHECK (
        id <> '00000000-0000-0000-0000-000000000000'
        AND record_id <> '00000000-0000-0000-0000-000000000000'
        AND (subject_person_id IS NULL OR subject_person_id <> '00000000-0000-0000-0000-000000000000')
        AND (uploaded_by IS NULL OR uploaded_by <> '00000000-0000-0000-0000-000000000000')),
    -- The objects.Bucket grammar (an owner's name).
    CONSTRAINT file_owner_chk CHECK (owner ~ '^[a-z][a-z0-9]{0,31}$'),
    -- The audit.Ref kind grammar, in bytes; and the owner is the area (decision 1, enforced
    -- here as well as in Go).
    CONSTRAINT file_record_kind_chk CHECK (
        octet_length(record_kind) <= 96
        AND record_kind ~ '^[a-z][a-z0-9]*(\.[a-z][a-z0-9_-]*)+$'),
    CONSTRAINT file_owner_area_chk CHECK (split_part(record_kind, '.', 1) = owner),
    CONSTRAINT file_level_chk CHECK (level BETWEEN 1 AND 4),
    -- Bytes, to match the Go side (MaxNameBytes = 255); no control characters, no path
    -- separators.
    CONSTRAINT file_name_chk CHECK (
        octet_length(name) BETWEEN 1 AND 255 AND name !~ '[[:cntrl:]/\\]'),
    -- A media type token pair, from the catalogue; the grammar, not the list, so a new kind
    -- needs no migration.
    CONSTRAINT file_content_type_chk CHECK (
        octet_length(content_type) <= 127 AND content_type ~ '^[a-z]+/[a-z0-9][a-z0-9.+-]*$'),
    CONSTRAINT file_size_chk CHECK (size_bytes > 0),
    CONSTRAINT file_sha256_chk CHECK (octet_length(sha256) = 32),
    CONSTRAINT file_store_chk CHECK (store IN ('disk', 's3')),
    CONSTRAINT file_bucket_chk CHECK (bucket ~ '^[a-z][a-z0-9]{0,31}$'),
    -- The objects.Key grammar: 1..255 bytes of [A-Za-z0-9._/-] with no empty, "." or ".."
    -- segment.
    CONSTRAINT file_object_key_chk CHECK (
        octet_length(object_key) <= 255
        AND object_key ~ '^[A-Za-z0-9._/-]+$'
        AND object_key !~ '(^|/)\.{0,2}(/|$)')
);

-- A Staged object attached twice is caught here (Attach maps it to Internal), and Discard's
-- "is this object attached?" (file_exists_by_key.sql), the orphan sweep
-- (known_object_keys.sql) and the bucket list (list_buckets.sql) use it.
CREATE UNIQUE INDEX file_store_bucket_key_uidx
    ON core.file (store, bucket, object_key);

-- A record's files, oldest first (get/list by record, the per-record count in
-- insert_file.sql, delete by record). The spec's (owner, record_kind, record_id, created_at)
-- plus id, so the order is total.
CREATE INDEX file_record_idx
    ON core.file (owner, record_kind, record_id, created_at, id);

-- A person's files as subject (export, the in_use check, and the RESTRICT check of the
-- foreign key). Partial: most files are about nobody in particular.
CREATE INDEX file_subject_idx
    ON core.file (subject_person_id)
    WHERE subject_person_id IS NOT NULL;

-- A person's uploads (export, anonymise, and the RESTRICT check of the foreign key).
CREATE INDEX file_uploaded_by_idx
    ON core.file (uploaded_by)
    WHERE uploaded_by IS NOT NULL;

COMMENT ON TABLE core.file IS 'sensitivity: internal (attachment metadata; no bytes, D43; name is personal when the file is)';
COMMENT ON COLUMN core.file.id IS 'sensitivity: internal (FileID; also the object key)';
COMMENT ON COLUMN core.file.owner IS 'sensitivity: internal (the area of record_kind; also the bucket)';
COMMENT ON COLUMN core.file.record_kind IS 'sensitivity: internal (audit.Ref kind of the owning record)';
COMMENT ON COLUMN core.file.record_id IS 'sensitivity: internal (audit.Ref id of the owning record)';
COMMENT ON COLUMN core.file.subject_person_id IS 'sensitivity: internal (a person ID, whom the file is about; export and deletion find it here; DeletePerson refuses while one remains)';
COMMENT ON COLUMN core.file.uploaded_by IS 'sensitivity: internal (a person ID; set NULL by files DeletePerson, anonymise_uploaded_by.sql)';
COMMENT ON COLUMN core.file.level IS 'sensitivity: none (the sensitivity level the owner declared for the file; decides audit of reads)';
COMMENT ON COLUMN core.file.name IS 'sensitivity: personal (follows level: at least internal, personal or sensitive when the file is; a certificate is often named after its holder; deleted with the row; never logged)';
COMMENT ON COLUMN core.file.content_type IS 'sensitivity: internal (sniffed type from the catalogue)';
COMMENT ON COLUMN core.file.size_bytes IS 'sensitivity: internal';
COMMENT ON COLUMN core.file.sha256 IS 'sensitivity: internal (the ETag; what Verify compares)';
COMMENT ON COLUMN core.file.store IS 'sensitivity: internal (objects.Kind: disk or s3)';
COMMENT ON COLUMN core.file.bucket IS 'sensitivity: internal (= owner in M4)';
COMMENT ON COLUMN core.file.object_key IS 'sensitivity: internal (= id as text in M4; carries no name and no personal data)';
COMMENT ON COLUMN core.file.stripped IS 'sensitivity: none';
COMMENT ON COLUMN core.file.created_at IS 'sensitivity: internal';

-- One row per object whose core.file row has been deleted and whose bytes may still exist
-- (files.md decision 16). Written in the same statement as the delete; removed once the
-- object is confirmed gone. Reap retries until then.
CREATE TABLE core.file_tombstone (
    id         uuid        NOT NULL,
    store      text        NOT NULL,
    bucket     text        NOT NULL,
    object_key text        NOT NULL,
    created_at timestamptz NOT NULL,
    -- Incremented by Reap on each failed delete; never reset. The error is logged, not stored.
    attempts   integer     NOT NULL DEFAULT 0,
    CONSTRAINT file_tombstone_pkey PRIMARY KEY (id),
    CONSTRAINT file_tombstone_nil_uuid_chk CHECK (id <> '00000000-0000-0000-0000-000000000000'),
    CONSTRAINT file_tombstone_store_chk CHECK (store IN ('disk', 's3')),
    CONSTRAINT file_tombstone_bucket_chk CHECK (bucket ~ '^[a-z][a-z0-9]{0,31}$'),
    CONSTRAINT file_tombstone_object_key_chk CHECK (
        octet_length(object_key) <= 255
        AND object_key ~ '^[A-Za-z0-9._/-]+$'
        AND object_key !~ '(^|/)\.{0,2}(/|$)'),
    CONSTRAINT file_tombstone_attempts_chk CHECK (attempts >= 0)
);

-- One tombstone per object; also what the orphan sweep checks (known_object_keys.sql).
CREATE UNIQUE INDEX file_tombstone_store_bucket_key_uidx
    ON core.file_tombstone (store, bucket, object_key);

-- Reap's oldest-first walk (list_tombstones.sql).
CREATE INDEX file_tombstone_created_idx
    ON core.file_tombstone (created_at, id);

COMMENT ON TABLE core.file_tombstone IS 'sensitivity: none (a pending object deletion: a store, a bucket and a key, which is a file ID; no personal data)';
COMMENT ON COLUMN core.file_tombstone.id IS 'sensitivity: none';
COMMENT ON COLUMN core.file_tombstone.store IS 'sensitivity: none';
COMMENT ON COLUMN core.file_tombstone.bucket IS 'sensitivity: none';
COMMENT ON COLUMN core.file_tombstone.object_key IS 'sensitivity: none (a file ID)';
COMMENT ON COLUMN core.file_tombstone.created_at IS 'sensitivity: none';
COMMENT ON COLUMN core.file_tombstone.attempts IS 'sensitivity: none';
