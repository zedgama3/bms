-- demo 0001: the seeded-database marker (core.demo_state), the demo accounts
-- (core.demo_account) and the captured mail (core.demo_mail) (demo.md, "Data"; D53). Schema
-- "core" is created by the migrator. Creates three new tables and touches no existing object,
-- so it is safe on any database; all three are empty (and stay unread unless demo.enabled), so
-- the index builds take no noticeable lock. The set is migrated on every install, demo or not,
-- so that Protected is always defined. References into other sets' tables: core.account
-- (identity) and core.person (people), both earlier in db's fixed set order (identity, people,
-- authz, departments, events, audit, settings, jobs, notify, files, demo).
--
-- Personal data. Only core.demo_mail holds any: to_address, subject and body (a stranger may
-- have typed a real address into the sign-up form despite the banner, and a body may name them
-- or hold a one-time link). Retention (demo.md, "Data"): there is no retention setting and no
-- job. The capture keeps the newest MaxMail (200) rows (trim_mail.sql, run in the same short
-- transaction as insert_mail.sql), and the reset drops the whole schema. core.demo_state and
-- core.demo_account hold nothing about a person: the accounts are generated, fictional
-- "<key>@demo.invalid" addresses. Reads of core.demo_mail are own-only (list_mail.sql filters
-- by the viewer's login address) and are not audited, because they are the viewer's own mail.
--
-- The spec lists exactly these columns, and none of the three tables has created_at or
-- updated_at: demo_state carries started_at/seeded_at, demo_account is written once by the seed
-- and never changes, and demo_mail's captured_at is its creation time. Timestamps come from the
-- service's clock, so none has a default.

-- At most one row: its presence says "this database is a demo, seeded by bms demo seed".
-- seeded_at stays NULL while the seed runs or after it failed; bms demo reset is the recovery.
CREATE TABLE core.demo_state (
    id           smallint    NOT NULL,
    -- demo.SeedVersion when the seed ran.
    seed_version integer     NOT NULL,
    -- The process instance that ran the seed (config's instance grammar, as core.job.instance).
    instance     text        NOT NULL,
    started_at   timestamptz NOT NULL,
    seeded_at    timestamptz,
    CONSTRAINT demo_state_pkey PRIMARY KEY (id),
    -- The single-row guarantee.
    CONSTRAINT demo_state_singleton_chk CHECK (id = 1),
    CONSTRAINT demo_state_seed_version_chk CHECK (seed_version >= 1),
    CONSTRAINT demo_state_instance_chk CHECK (
        instance ~ '^[a-z0-9]([a-z0-9-]*[a-z0-9])?$' AND octet_length(instance) BETWEEN 1 AND 64),
    CONSTRAINT demo_state_seeded_at_chk CHECK (seeded_at IS NULL OR seeded_at >= started_at)
);

COMMENT ON TABLE core.demo_state IS 'sensitivity: internal (the demo marker; at most one row; bms demo reset refuses a database without it; dropped with the schema at reset)';
COMMENT ON COLUMN core.demo_state.id IS 'sensitivity: none (always 1)';
COMMENT ON COLUMN core.demo_state.seed_version IS 'sensitivity: internal (demo.SeedVersion when the seed ran)';
COMMENT ON COLUMN core.demo_state.instance IS 'sensitivity: internal (the process instance that ran the seed)';
COMMENT ON COLUMN core.demo_state.started_at IS 'sensitivity: internal';
COMMENT ON COLUMN core.demo_state.seeded_at IS 'sensitivity: internal (NULL while seeding or after a failed seed)';

-- The ten shared demo accounts, written by the seed's accounts step. A row here is what makes
-- demo.Protected answer true. The RESTRICT foreign keys are the backstop behind the Guard: even
-- a direct deletion of the account or the person fails while this row references them.
CREATE TABLE core.demo_account (
    account_id uuid    NOT NULL,
    person_id  uuid    NOT NULL,
    -- The demoseed.Actor* key ("participant", "lead", ...).
    key        text    NOT NULL,
    -- "<key>@demo.invalid", a generated fictional address; not a person's.
    email      text    NOT NULL,
    -- Display order in GET /api/demo and the sign-in table.
    position   integer NOT NULL,
    CONSTRAINT demo_account_pkey PRIMARY KEY (account_id),
    CONSTRAINT demo_account_account_id_fkey FOREIGN KEY (account_id)
        REFERENCES core.account (id) ON DELETE RESTRICT,
    CONSTRAINT demo_account_person_id_fkey FOREIGN KEY (person_id)
        REFERENCES core.person (id) ON DELETE RESTRICT,
    CONSTRAINT demo_account_key_key UNIQUE (key),
    CONSTRAINT demo_account_position_key UNIQUE (position),
    CONSTRAINT demo_account_key_chk CHECK (key ~ '^[a-z][a-z0-9]*(-[a-z0-9]+)*$' AND octet_length(key) <= 32),
    -- Only the reserved fictional domain (RFC 2606) can ever be stored: a real mailbox cannot.
    CONSTRAINT demo_account_email_chk CHECK (
        octet_length(email) BETWEEN 3 AND 254 AND email ~ '^[a-z0-9-]+@demo\.invalid$'),
    CONSTRAINT demo_account_position_chk CHECK (position >= 0),
    -- The all-zero UUID means "none" in Go; it must never be stored.
    CONSTRAINT demo_account_nil_uuid_chk CHECK (
        account_id <> '00000000-0000-0000-0000-000000000000'
        AND person_id <> '00000000-0000-0000-0000-000000000000')
);

-- demo_account_pkey serves Protected (is_protected.sql); demo_account_person_id_fkey's RESTRICT
-- check on a person delete scans ten rows at most, so it needs no index.

COMMENT ON TABLE core.demo_account IS 'sensitivity: internal (the shared demo accounts; generated demo.invalid addresses, not a person''s; dropped with the schema at reset)';
COMMENT ON COLUMN core.demo_account.account_id IS 'sensitivity: internal';
COMMENT ON COLUMN core.demo_account.person_id IS 'sensitivity: internal';
COMMENT ON COLUMN core.demo_account.key IS 'sensitivity: internal (the demoseed actor key)';
COMMENT ON COLUMN core.demo_account.email IS 'sensitivity: internal (a generated <key>@demo.invalid address, public by design; not personal)';
COMMENT ON COLUMN core.demo_account.position IS 'sensitivity: internal (display order)';

-- Captured notifications: what the demo would have e-mailed. At most MaxMail rows (200) are kept.
CREATE TABLE core.demo_mail (
    id          uuid        NOT NULL,
    -- The recipient as notify gave it (model.Email); matched case-insensitively.
    to_address  text        NOT NULL,
    subject     text        NOT NULL,
    body        text        NOT NULL,
    -- Envelope.MessageID, "<id>@<hostname>".
    message_id  text        NOT NULL,
    captured_at timestamptz NOT NULL,
    CONSTRAINT demo_mail_pkey PRIMARY KEY (id),
    -- The all-zero UUID means "none" in Go; it must never be stored.
    CONSTRAINT demo_mail_nil_uuid_chk CHECK (id <> '00000000-0000-0000-0000-000000000000'),
    -- Bytes, to match the Go side. The limits equal notify's own for a message it can send
    -- (core.notify_message), so no deliverable envelope can fail here.
    CONSTRAINT demo_mail_to_address_chk CHECK (
        octet_length(to_address) BETWEEN 3 AND 254 AND to_address !~ '[[:cntrl:]]'),
    CONSTRAINT demo_mail_subject_chk CHECK (
        octet_length(subject) BETWEEN 1 AND 256 AND subject !~ '[[:cntrl:]]'),
    CONSTRAINT demo_mail_body_chk CHECK (
        octet_length(body) BETWEEN 1 AND 65536
        AND body !~ '[\x01-\x08\x0b\x0c\x0e-\x1f\x7f]'),
    CONSTRAINT demo_mail_message_id_chk CHECK (
        octet_length(message_id) BETWEEN 1 AND 512 AND message_id !~ '[[:cntrl:]]')
);

-- The trim (trim_mail.sql) walks the newest rows; the id breaks ties so the order is total
-- (the spec names (captured_at DESC); id DESC is added for a stable order).
CREATE INDEX demo_mail_captured_at_idx ON core.demo_mail (captured_at DESC, id DESC);

-- The own-only recipient filter (list_mail.sql): the viewer's rows, newest first, with no sort.
CREATE INDEX demo_mail_to_idx ON core.demo_mail ((lower(to_address)), captured_at DESC, id DESC);

COMMENT ON TABLE core.demo_mail IS 'sensitivity: personal (captured demo notifications; to_address, subject and body are personal; own-only reads, not audited; newest 200 kept, all dropped at reset; no retention setting or job)';
COMMENT ON COLUMN core.demo_mail.id IS 'sensitivity: none (row identifier)';
COMMENT ON COLUMN core.demo_mail.to_address IS 'sensitivity: personal (the recipient; a stranger may have typed a real address despite the sign-up note; shown only to the account it was sent to; never logged)';
COMMENT ON COLUMN core.demo_mail.subject IS 'sensitivity: personal (rendered subject; never logged)';
COMMENT ON COLUMN core.demo_mail.body IS 'sensitivity: personal (rendered body, may name the person or hold a one-time link; never logged)';
COMMENT ON COLUMN core.demo_mail.message_id IS 'sensitivity: internal (<id>@<hostname>)';
COMMENT ON COLUMN core.demo_mail.captured_at IS 'sensitivity: internal';
