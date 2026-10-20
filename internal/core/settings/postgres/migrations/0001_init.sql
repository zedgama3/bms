-- settings 0001: the regional's configured values, the one revision number every write bumps,
-- and the regional's terminology overrides (settings.md, "Data").
-- Schema "core" is created by the migrator. Creates three new tables and touches no existing
-- one, so it is safe on any database.
--
-- Nothing here is about a person: no column is personal or sensitive, nothing references
-- core.person, and every row is kept forever, so there is no retention job and no export or
-- deletion step. core.setting.sealed holds a Secret value as ciphertext only (decision 3);
-- it is the one column that must never be logged or returned.
--
-- Timestamps come from the service's clock, so none has a default. The spec gives these
-- tables no created_at (a row is rewritten in place and the audit log keeps the history).
--
-- No logo or other brand image table: the logo waits for the files spec (D39).

-- One row per setting set away from its default; a reset deletes the row (decision 1). A row
-- for a key no process declares (a disabled or removed module) is kept and read by nobody.
CREATE TABLE core.setting (
    -- "<area>.<key>"; both parts are idents [a-z][a-z0-9]*(_[a-z0-9]+)* of at most 64 bytes.
    key        text        NOT NULL,
    -- The text form of the value; NULL for a Secret. Trimmed and parsed by kind in Go.
    value      text,
    -- nonce (12 bytes) || AES-256-GCM ciphertext (plaintext + 16-byte tag) for a Secret; NULL
    -- otherwise. The AAD is the full key, so a ciphertext moved to another key fails to open.
    sealed     bytea,
    updated_at timestamptz NOT NULL,
    CONSTRAINT setting_pkey PRIMARY KEY (key),
    CONSTRAINT setting_key_chk CHECK (
        octet_length(key) <= 129
        AND key ~ '^[a-z][a-z0-9]*(_[a-z0-9]+)*\.[a-z][a-z0-9]*(_[a-z0-9]+)*$'
    ),
    -- Exactly one of the two is set (spec).
    CONSTRAINT setting_value_xor_sealed_chk CHECK ((value IS NULL) <> (sealed IS NULL)),
    -- Bytes, to match the Go side (MaxTextBytes = 4096). Control characters other than
    -- newline and tab are refused, as for the Text and Secret kinds.
    CONSTRAINT setting_value_length_chk CHECK (octet_length(value) <= 4096),
    CONSTRAINT setting_value_chars_chk CHECK (value !~ '[\x01-\x08\x0b-\x1f\x7f]'),
    -- 12-byte nonce + 16-byte tag at least; plus MaxTextBytes of plaintext at most.
    CONSTRAINT setting_sealed_length_chk CHECK (octet_length(sealed) BETWEEN 28 AND 4124)
);

COMMENT ON TABLE core.setting IS 'sensitivity: internal (a regional''s configuration; nothing personal)';
COMMENT ON COLUMN core.setting.value IS 'sensitivity: internal (configuration text; a Secret is never stored here)';
COMMENT ON COLUMN core.setting.sealed IS 'sensitivity: sensitive (a Secret setting, e.g. an SMTP password, as AES-256-GCM ciphertext under the settings sealing key; never returned by a route, never logged)';

-- The single revision row. Every write transaction takes its row lock, which serialises
-- writers and orders snapshots; a poll reads it to learn that the data moved.
CREATE TABLE core.setting_revision (
    id       smallint NOT NULL,
    revision bigint   NOT NULL,
    CONSTRAINT setting_revision_pkey PRIMARY KEY (id),
    CONSTRAINT setting_revision_single_chk CHECK (id = 1),
    CONSTRAINT setting_revision_positive_chk CHECK (revision >= 1)
);

INSERT INTO core.setting_revision (id, revision) VALUES (1, 1);

-- The regional's overrides of the core terms (decision 7). Defaults are embedded in Go
-- (terms/<lang>.json); a row exists only for a form set away from its default.
CREATE TABLE core.terminology (
    -- A member of settings.Languages, a lowercase tag such as "en". Membership is checked in
    -- Go so adding a language needs no migration; the database bounds the shape.
    lang       text        NOT NULL,
    -- A member of settings.CoreTerms ("department", "theme_camp"), checked in Go likewise.
    term       text        NOT NULL,
    form       text        NOT NULL,
    value      text        NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT terminology_pkey PRIMARY KEY (lang, term, form),
    CONSTRAINT terminology_lang_chk CHECK (lang ~ '^[a-z]{2,3}(-[A-Za-z0-9]{2,8})*$' AND octet_length(lang) <= 35),
    CONSTRAINT terminology_term_chk CHECK (term ~ '^[a-z][a-z0-9]*(_[a-z0-9]+)*$' AND octet_length(term) <= 64),
    -- The four forms of decision 7 are fixed by the Forms type, so the database pins them.
    CONSTRAINT terminology_form_chk CHECK (form IN ('singular', 'plural', 'singular_title', 'plural_title')),
    -- Bytes, to match the Go side (MaxTermBytes = 60). Trimmed in Go; here only bounded.
    CONSTRAINT terminology_value_length_chk CHECK (octet_length(value) BETWEEN 1 AND 60),
    CONSTRAINT terminology_value_chars_chk CHECK (value !~ '[[:cntrl:]]')
);

COMMENT ON TABLE core.terminology IS 'sensitivity: internal (the regional''s words for core terms; nothing personal)';
COMMENT ON COLUMN core.terminology.value IS 'sensitivity: public (shown to everyone in GET /api/settings/public)';
