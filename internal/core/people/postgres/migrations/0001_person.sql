-- people 0001: the person row, emergency contacts, consents, and the backfill of one person
-- row per existing account. Schema "core" is created by the migrator. Creates new objects
-- only; the one statement that touches another set's table is the final INSERT, which only
-- READS core.account (identity 0001 has run first: db's fixed set order is identity, people,
-- authz, ...). That read is the one place people's SQL touches identity's table, in a
-- migration only, never in a query (people.md "Backfill"; db.md amended 2026-10-18).
--
-- core.person holds personal data (legal_name, phone, contact_email) and
-- core.person_emergency_contact holds personal data about a third party (name, phone,
-- relationship). Retention: every row lives until its person is deleted (Service.Delete,
-- people.md decision 17); no time-based retention in M2 (D30). The person ID stays in audit
-- rows after deletion (audit.md decision 10).
--
-- No foreign key from core.account: identity inserts the account before this row exists
-- (people.md decision 2). Foreign keys from authz and modules INTO core.person are theirs.
--
-- Safe on a database with data: new tables, then one INSERT ... SELECT over core.account
-- (one row per account; an installation has accounts in the tens to thousands), in the same
-- transaction. ON CONFLICT DO NOTHING makes it harmless to repeat.

CREATE TABLE core.person (
    id            uuid        NOT NULL,
    -- internal. "" until the person fills it in (a new account's row is created empty).
    playa_name    text        NOT NULL DEFAULT '',
    pronouns      text        NOT NULL DEFAULT '',
    -- A BCP 47 tag or '' (the regional's default); the grammar is checked in Go.
    language      text        NOT NULL DEFAULT '',
    legal_name    text        NOT NULL DEFAULT '',
    phone         text        NOT NULL DEFAULT '',
    -- As entered (model.Email); NULL: none. For a person without an account only (checked in
    -- Go: has_account). Never '' (writers use NULLIF). Matched by lower(contact_email).
    contact_email text,
    -- The profile manager who created an account-less person; NULL for sign-up rows,
    -- commands, and when that manager is deleted.
    created_by    uuid,
    created_at    timestamptz NOT NULL,
    updated_at    timestamptz NOT NULL,
    CONSTRAINT person_pkey PRIMARY KEY (id),
    CONSTRAINT person_created_by_fkey FOREIGN KEY (created_by)
        REFERENCES core.person (id) ON DELETE SET NULL,
    -- The all-zero UUID means "none" in Go; it must never be stored.
    CONSTRAINT person_nil_uuid_chk CHECK (
        id <> '00000000-0000-0000-0000-000000000000'
        AND (created_by IS NULL OR created_by <> '00000000-0000-0000-0000-000000000000')),
    -- Bytes, to match the Go side (MaxPlayaNameBytes and the others); bytes >= characters.
    CONSTRAINT person_playa_name_length_chk CHECK (octet_length(playa_name) <= 100),
    CONSTRAINT person_pronouns_length_chk CHECK (octet_length(pronouns) <= 40),
    CONSTRAINT person_language_length_chk CHECK (octet_length(language) <= 35),
    CONSTRAINT person_legal_name_length_chk CHECK (octet_length(legal_name) <= 200),
    CONSTRAINT person_phone_length_chk CHECK (octet_length(phone) <= 40),
    CONSTRAINT person_contact_email_length_chk CHECK (
        contact_email IS NULL OR octet_length(contact_email) BETWEEN 3 AND 254)
);

-- Search by playa name (people.md "Data"): serves the stable order of Store.Search,
-- (lower(playa_name) in byte order, id), and so its keyset paging. COLLATE "C" makes the
-- order independent of the database's locale, so a cursor means the same on every copy, and
-- is the order the index holds. (The spec's text_pattern_ops index orders the same bytes but
-- cannot serve a plain ORDER BY; this one serves the order, and the prefix range of a LIKE
-- 'x%' as well.) Never ordered by legal name: that column has no index and no ORDER BY.
CREATE INDEX person_playa_name_idx ON core.person ((lower(playa_name) COLLATE "C"), id);

-- Exact-address lookup for the contact-address arm of Store.Search (lower(contact_email) =
-- lower($q)), for people without an account. Not unique: two account-less people may share a
-- household address (model.md Proposed change 5). Partial: most rows have none.
CREATE INDEX person_contact_email_idx ON core.person ((lower(contact_email)))
    WHERE contact_email IS NOT NULL;

CREATE TABLE core.person_emergency_contact (
    id           uuid        NOT NULL,
    person_id    uuid        NOT NULL,
    name         text        NOT NULL,
    phone        text        NOT NULL,
    relationship text        NOT NULL DEFAULT '',
    -- 1-based display order, kept contiguous (Store.Renumber after a delete).
    position     integer     NOT NULL,
    created_at   timestamptz NOT NULL,
    updated_at   timestamptz NOT NULL,
    CONSTRAINT person_emergency_contact_pkey PRIMARY KEY (id),
    -- A safety net only: Service.Delete removes the contacts explicitly so the count is
    -- reported.
    CONSTRAINT person_emergency_contact_person_id_fkey FOREIGN KEY (person_id)
        REFERENCES core.person (id) ON DELETE CASCADE,
    -- Deferrable (initially immediate: checked at the end of each statement), so a statement
    -- that shifts several positions at once does not trip over its own intermediate rows.
    -- Also serves "a person's contacts in order" and the per-person count. A violation is
    -- two concurrent adds taking the same position: Conflict (the use case locks the person
    -- row first, so it should not happen).
    CONSTRAINT person_emergency_contact_person_position_key UNIQUE (person_id, position)
        DEFERRABLE INITIALLY IMMEDIATE,
    CONSTRAINT person_emergency_contact_nil_uuid_chk CHECK (
        id <> '00000000-0000-0000-0000-000000000000'
        AND person_id <> '00000000-0000-0000-0000-000000000000'),
    CONSTRAINT person_emergency_contact_name_length_chk CHECK (octet_length(name) BETWEEN 1 AND 200),
    CONSTRAINT person_emergency_contact_phone_length_chk CHECK (octet_length(phone) BETWEEN 1 AND 40),
    CONSTRAINT person_emergency_contact_relationship_length_chk CHECK (octet_length(relationship) <= 60),
    CONSTRAINT person_emergency_contact_position_chk CHECK (position >= 1)
);

CREATE TABLE core.person_consent (
    person_id   uuid        NOT NULL,
    -- "<owner>.<ident>", at most 64 bytes; the grammar is checked in Go (Purpose.Validate).
    purpose     text        NOT NULL,
    granted     boolean     NOT NULL,
    -- The person, or the profile manager who recorded a paper form; NULL when that manager
    -- is deleted.
    recorded_by uuid,
    changed_at  timestamptz NOT NULL,
    CONSTRAINT person_consent_pkey PRIMARY KEY (person_id, purpose),
    CONSTRAINT person_consent_person_id_fkey FOREIGN KEY (person_id)
        REFERENCES core.person (id) ON DELETE CASCADE,
    CONSTRAINT person_consent_recorded_by_fkey FOREIGN KEY (recorded_by)
        REFERENCES core.person (id) ON DELETE SET NULL,
    CONSTRAINT person_consent_nil_uuid_chk CHECK (
        person_id <> '00000000-0000-0000-0000-000000000000'
        AND (recorded_by IS NULL OR recorded_by <> '00000000-0000-0000-0000-000000000000')),
    CONSTRAINT person_consent_purpose_length_chk CHECK (octet_length(purpose) BETWEEN 3 AND 64)
);

COMMENT ON TABLE core.person IS
    'One row per person (people). Profile: playa name, pronouns, language (internal); legal name, phone, contact address (personal). Retention: kept until Service.Delete (per-person deletion); no retention setting. Export: Service.Export.';
COMMENT ON COLUMN core.person.id IS 'sensitivity: internal (the PersonID identity minted or people generated)';
COMMENT ON COLUMN core.person.playa_name IS 'sensitivity: internal (the name shown everywhere; empty until filled in)';
COMMENT ON COLUMN core.person.pronouns IS 'sensitivity: internal';
COMMENT ON COLUMN core.person.language IS 'sensitivity: internal (BCP 47 tag or empty)';
COMMENT ON COLUMN core.person.legal_name IS 'sensitivity: personal (waivers and gate; shown to the person and to people.contacts.read, audited)';
COMMENT ON COLUMN core.person.phone IS 'sensitivity: personal (reaching a volunteer; shown to the person and to people.contacts.read, audited)';
COMMENT ON COLUMN core.person.contact_email IS 'sensitivity: personal (reaching a person who has no account; shown to the person and to people.contacts.read, audited)';
COMMENT ON COLUMN core.person.created_by IS 'sensitivity: internal (the profile manager who created an account-less person)';
COMMENT ON COLUMN core.person.created_at IS 'sensitivity: internal';
COMMENT ON COLUMN core.person.updated_at IS 'sensitivity: internal';

COMMENT ON TABLE core.person_emergency_contact IS
    'A person''s emergency contacts (people). Every text column is personal data about a third party. Retention: kept until Service.Delete (explicitly, so the count is reported); no retention setting. Export: Service.Export.';
COMMENT ON COLUMN core.person_emergency_contact.id IS 'sensitivity: internal (row identifier)';
COMMENT ON COLUMN core.person_emergency_contact.person_id IS 'sensitivity: internal';
COMMENT ON COLUMN core.person_emergency_contact.name IS 'sensitivity: personal (a third party''s name; shown to the person and to people.contacts.read, audited)';
COMMENT ON COLUMN core.person_emergency_contact.phone IS 'sensitivity: personal (a third party''s phone; shown to the person and to people.contacts.read, audited)';
COMMENT ON COLUMN core.person_emergency_contact.relationship IS 'sensitivity: personal (how the contact relates to the person)';
COMMENT ON COLUMN core.person_emergency_contact."position" IS 'sensitivity: internal (display order, 1-based, contiguous)';
COMMENT ON COLUMN core.person_emergency_contact.created_at IS 'sensitivity: internal';
COMMENT ON COLUMN core.person_emergency_contact.updated_at IS 'sensitivity: internal';

COMMENT ON TABLE core.person_consent IS
    'The current state of each consent purpose for each person (people). History is the audit log (people.consent.changed). Retention: kept until Service.Delete; no retention setting. Export: Service.Export.';
COMMENT ON COLUMN core.person_consent.person_id IS 'sensitivity: internal';
COMMENT ON COLUMN core.person_consent.purpose IS 'sensitivity: internal (a purpose name, "<owner>.<ident>")';
COMMENT ON COLUMN core.person_consent.granted IS 'sensitivity: internal (read by modules through Directory.ConsentOf)';
COMMENT ON COLUMN core.person_consent.recorded_by IS 'sensitivity: internal (the person, or the profile manager who recorded a paper form; set NULL when that manager is deleted)';
COMMENT ON COLUMN core.person_consent.changed_at IS 'sensitivity: internal';

-- Backfill: an M1 installation has accounts but no person rows. One empty row per account,
-- carrying the account's creation time. ON CONFLICT makes a rerun (or a person row already
-- written by the account-created handler) harmless.
INSERT INTO core.person (id, created_at, updated_at)
SELECT person_id, created_at, now()
FROM core.account
ON CONFLICT (id) DO NOTHING;
