-- identity 0001: the account table. Schema "core" is created by the migrator.
-- Creates new objects only; touches no existing table, so it is safe on any database.
--
-- core.account holds personal data (email, email_canonical, disabled_at, last_login_at) and a
-- credential (password_hash). Retention: none of its own. A row lives until the person is
-- deleted (Service.DeletePerson, called by people's deletion), because an account that exists
-- must stay reachable. The audit trail of logins is audit's retention (audit.retention).
-- Person IDs stay in audit rows after deletion (audit.md decision 10).

CREATE TABLE core.account (
    id                       uuid        NOT NULL,
    person_id                uuid        NOT NULL,
    -- As entered (model.Email); the login lookup compares email_canonical, never this column.
    email                    text        NOT NULL,
    -- Uniqueness ignoring case (model.md decision 11). Generated, so no writer can get it
    -- wrong; lookups spell it "email_canonical = lower($1)".
    email_canonical          text        GENERATED ALWAYS AS (lower(email)) STORED,
    -- PHC string "$argon2id$v=19$m=...,t=...,p=...$<salt>$<hash>". Read only to verify.
    password_hash            text        NOT NULL,
    superuser                boolean     NOT NULL DEFAULT false,
    password_change_required boolean     NOT NULL DEFAULT false,
    -- Per-account revocation (09 section 2): a token carries the generation it was issued
    -- under; a bump ends every older session. Written as "session_generation + 1" in SQL.
    session_generation       integer     NOT NULL DEFAULT 0,
    disabled_at              timestamptz,
    last_login_at            timestamptz,
    password_changed_at      timestamptz NOT NULL,
    created_at               timestamptz NOT NULL,
    updated_at               timestamptz NOT NULL,
    CONSTRAINT account_pkey PRIMARY KEY (id),
    CONSTRAINT account_person_id_key UNIQUE (person_id),
    CONSTRAINT account_email_canonical_key UNIQUE (email_canonical),
    -- Bytes, to match the Go side (model.Email); bytes >= characters.
    CONSTRAINT account_email_length_chk CHECK (octet_length(email) BETWEEN 3 AND 254),
    CONSTRAINT account_password_hash_length_chk CHECK (octet_length(password_hash) BETWEEN 1 AND 512),
    CONSTRAINT account_session_generation_chk CHECK (session_generation >= 0),
    -- The all-zero UUID means "none" in Go; it must never be stored.
    CONSTRAINT account_nil_uuid_chk CHECK (
        id        <> '00000000-0000-0000-0000-000000000000'
        AND person_id <> '00000000-0000-0000-0000-000000000000')
);

-- Serves the admin list: newest first, keyset on (created_at, id).
-- account_email_canonical_key serves the sign-in lookup; account_person_id_key serves
-- ByPerson, export and deletion; account_pkey serves ByID and Authenticate's state read.
CREATE INDEX account_created_at_id_idx ON core.account (created_at DESC, id DESC);

COMMENT ON TABLE core.account IS
    'Local login accounts, one per person (identity). Holds personal data and a credential. Retention: kept until Service.DeletePerson (people''s per-person deletion); no retention setting. Export: Service.ExportPerson, never the hash.';
COMMENT ON COLUMN core.account.id IS 'sensitivity: none (row identifier)';
COMMENT ON COLUMN core.account.person_id IS 'sensitivity: internal (modules refer to the person, never the account)';
COMMENT ON COLUMN core.account.email IS 'sensitivity: personal (the login address as entered; shown to the person and to accounts.read, audited)';
COMMENT ON COLUMN core.account.email_canonical IS 'sensitivity: personal (lower(email), generated; uniqueness and lookup)';
COMMENT ON COLUMN core.account.password_hash IS 'sensitivity: sensitive (a credential; read only to verify; never exported, logged or shown)';
COMMENT ON COLUMN core.account.superuser IS 'sensitivity: internal (D23 break-glass flag)';
COMMENT ON COLUMN core.account.password_change_required IS 'sensitivity: internal (a temporary password is set)';
COMMENT ON COLUMN core.account.session_generation IS 'sensitivity: internal (revocation counter)';
COMMENT ON COLUMN core.account.disabled_at IS 'sensitivity: personal (an admin-only fact about the person)';
COMMENT ON COLUMN core.account.last_login_at IS 'sensitivity: personal (audited when an admin reads it)';
COMMENT ON COLUMN core.account.password_changed_at IS 'sensitivity: internal';
COMMENT ON COLUMN core.account.created_at IS 'sensitivity: internal';
COMMENT ON COLUMN core.account.updated_at IS 'sensitivity: internal';
