-- departments 0001: the regional's standing departments (departments-events.md, "Data").
-- Schema "core" is created by the migrator. Creates one new object set and touches no existing
-- table, so it is safe on any database.
--
-- Nothing here is personal data: every column is "internal" and kept forever (departments are
-- archived, never deleted), so there is no retention job and no export or deletion step. The
-- contact address is the department's shared mailbox, never a person's address (decision 7).
-- No column references core.person. Modules may reference core.department(id) with a foreign
-- key ON DELETE RESTRICT but never read this table (D10).
--
-- Timestamps come from the service's clock, so none has a default.

CREATE TABLE core.department (
    id            uuid        NOT NULL,
    name          text        NOT NULL,
    description   text        NOT NULL DEFAULT '',
    -- The department's shared mailbox, stored as entered ('' = none). Shape is checked in Go
    -- by model.ParseEmail; the database only bounds the length (RFC 5321: 254).
    contact_email text        NOT NULL DEFAULT '',
    -- NULL = active. Archived departments keep their grants (decision 4).
    archived_at   timestamptz,
    created_at    timestamptz NOT NULL,
    updated_at    timestamptz NOT NULL,
    CONSTRAINT department_pkey PRIMARY KEY (id),
    CONSTRAINT department_nil_uuid_chk CHECK (id <> '00000000-0000-0000-0000-000000000000'),
    -- Bytes, to match the Go side (MaxNameBytes, MaxDescriptionBytes).
    CONSTRAINT department_name_length_chk CHECK (octet_length(name) BETWEEN 1 AND 100),
    CONSTRAINT department_name_chars_chk CHECK (name !~ '[[:cntrl:]]'),
    CONSTRAINT department_description_length_chk CHECK (octet_length(description) <= 2000),
    CONSTRAINT department_contact_email_length_chk CHECK (octet_length(contact_email) <= 254)
);

-- Names are unique ignoring case among all departments, archived included, so a restore never
-- collides (decision 15). A violation reports constraint "department_name_key": Conflict,
-- field name, code taken.
CREATE UNIQUE INDEX department_name_key ON core.department (lower(name));

COMMENT ON COLUMN core.department.contact_email IS 'sensitivity: internal (shared mailbox, never a person''s address)';
