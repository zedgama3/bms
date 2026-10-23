-- volunteers 0001: the module's tables (module-volunteers.md, "Data"). Schema "volunteers" is
-- created by the migrator. Creates new objects only and touches no existing table, so it is
-- safe on any database (every table is empty, so the index builds take no noticeable lock).
-- core.person, core.department and core.event come from the core sets, which run first.
--
-- Foreign keys into core are ON DELETE RESTRICT: people calls DeletePerson first (this module
-- deletes the person's sign-ups, memberships, records and "unlisted" row, and nulls the
-- who-did-it columns), and departments and events are never deleted. No query in this module
-- reads a core table (D10): names and states come from the core services.
--
-- Timestamps come from the service's clock, so none has a default. Lengths are in bytes
-- (octet_length) to match the Go constants in vol (MaxNameBytes = 100, MaxDescriptionBytes =
-- 2000, MaxLabelBytes = 100, MaxLocationBytes = 200, MaxSlots = 200, MaxShiftDuration = 48h,
-- MaxValidityDays = 3650). The all-zero UUID means "none" in Go and must never be stored.
--
-- Constraint names are the contract with the Go store (db.Error.Constraint); the ones the
-- store turns into field errors or Conflict codes are named in a comment at each one, and in
-- the header of each query that can raise them.
--
-- Sensitivity (privacy-data): nothing here is above "internal" except certificate_file_id,
-- which points at a "personal" file (a certificate carries a legal name). Retention is the one
-- key volunteers.retention (0 = forever), enforced by the volunteers.purge job: sign-ups of
-- ended shifts, canceled shifts with no sign-ups, records long expired (with their files).
-- Members and "unlisted" rows are current state and are never purged. DeletePerson removes a
-- person's rows or nulls the person's ID, so the RESTRICT keys never block a deletion.

-- ---------------------------------------------------------------------------------------
-- qualification: the catalogue. department_id NULL = org-wide. Nothing personal; kept forever.
-- ---------------------------------------------------------------------------------------
CREATE TABLE volunteers.qualification (
    id            uuid        NOT NULL,
    department_id uuid,
    name          text        NOT NULL,
    description   text        NOT NULL DEFAULT '',
    -- NULL = indefinite by default; else prefills a record's expires_on.
    validity_days integer,
    -- NULL = active.
    archived_at   timestamptz,
    created_at    timestamptz NOT NULL,
    updated_at    timestamptz NOT NULL,
    CONSTRAINT qualification_pkey PRIMARY KEY (id),
    CONSTRAINT qualification_department_id_fkey FOREIGN KEY (department_id)
        REFERENCES core.department (id) ON DELETE RESTRICT,
    CONSTRAINT qualification_nil_uuid_chk CHECK (
        id <> '00000000-0000-0000-0000-000000000000'
        AND (department_id IS NULL OR department_id <> '00000000-0000-0000-0000-000000000000')),
    CONSTRAINT qualification_name_length_chk CHECK (octet_length(name) BETWEEN 1 AND 100),
    CONSTRAINT qualification_name_chars_chk CHECK (name !~ '[[:cntrl:]]'),
    CONSTRAINT qualification_description_length_chk CHECK (octet_length(description) <= 2000),
    CONSTRAINT qualification_validity_days_chk CHECK (validity_days IS NULL OR validity_days BETWEEN 1 AND 3650)
);

-- A name is unique ignoring case within its scope (one department, or org-wide), archived
-- entries included so an un-archive never collides. Violation: constraint
-- "qualification_name_key": Conflict, field name, code taken.
CREATE UNIQUE INDEX qualification_name_key ON volunteers.qualification
    ((COALESCE(department_id, '00000000-0000-0000-0000-000000000000'::uuid)), (lower(name)));

-- A department's catalogue, and the RESTRICT check of the foreign key. Partial: org-wide
-- entries are few and found through qualification_name_key's expression.
CREATE INDEX qualification_department_idx ON volunteers.qualification (department_id)
    WHERE department_id IS NOT NULL;

COMMENT ON TABLE volunteers.qualification IS
    'sensitivity: none (the regional''s catalogue of qualifications; nothing about a person). Retention: kept forever.';
COMMENT ON COLUMN volunteers.qualification.id IS 'sensitivity: none';
COMMENT ON COLUMN volunteers.qualification.department_id IS 'sensitivity: none (NULL = org-wide)';
COMMENT ON COLUMN volunteers.qualification.name IS 'sensitivity: none';
COMMENT ON COLUMN volunteers.qualification.description IS 'sensitivity: none';
COMMENT ON COLUMN volunteers.qualification.validity_days IS 'sensitivity: none';
COMMENT ON COLUMN volunteers.qualification.archived_at IS 'sensitivity: none';

-- ---------------------------------------------------------------------------------------
-- qualification_record: who holds which qualification. Pending until a lead verifies it
-- (verified_at NULL); only a verified record counts (D40, D41). Internal (D42).
-- ---------------------------------------------------------------------------------------
CREATE TABLE volunteers.qualification_record (
    id                  uuid        NOT NULL,
    qualification_id    uuid        NOT NULL,
    person_id           uuid        NOT NULL,
    acquired_on         date        NOT NULL,
    -- NULL = indefinite.
    expires_on          date,
    -- The lead who vouched. NULL while pending, and NULL again after that lead's deletion
    -- (anonymised): verified_at, not verified_by, says whether the record is verified.
    verified_by         uuid,
    verified_at         timestamptz,
    -- The files.FileID of the uploaded certificate; NULL = none. No foreign key: files owns
    -- core.file and points back through core.file.record_kind/record_id. Deleted with the
    -- record through files.DeleteByRecord.
    certificate_file_id uuid,
    -- The date the expiry notice was sent; NULL = not yet (the expiry scan's dedupe).
    expiry_notified_on  date,
    created_at          timestamptz NOT NULL,
    updated_at          timestamptz NOT NULL,
    CONSTRAINT qualification_record_pkey PRIMARY KEY (id),
    CONSTRAINT qualification_record_qualification_id_fkey FOREIGN KEY (qualification_id)
        REFERENCES volunteers.qualification (id) ON DELETE RESTRICT,
    CONSTRAINT qualification_record_person_id_fkey FOREIGN KEY (person_id)
        REFERENCES core.person (id) ON DELETE RESTRICT,
    CONSTRAINT qualification_record_verified_by_fkey FOREIGN KEY (verified_by)
        REFERENCES core.person (id) ON DELETE RESTRICT,
    CONSTRAINT qualification_record_nil_uuid_chk CHECK (
        id <> '00000000-0000-0000-0000-000000000000'
        AND qualification_id <> '00000000-0000-0000-0000-000000000000'
        AND person_id <> '00000000-0000-0000-0000-000000000000'
        AND (verified_by IS NULL OR verified_by <> '00000000-0000-0000-0000-000000000000')
        AND (certificate_file_id IS NULL OR certificate_file_id <> '00000000-0000-0000-0000-000000000000')),
    -- F19: an expiry before the acquisition date is Invalid range.
    CONSTRAINT qualification_record_dates_chk CHECK (expires_on IS NULL OR expires_on >= acquired_on),
    -- A verifier implies a verification time (the reverse does not hold after anonymising).
    CONSTRAINT qualification_record_verified_chk CHECK (verified_by IS NULL OR verified_at IS NOT NULL),
    -- F31: nobody verifies their own record (D41: no self-attestation).
    CONSTRAINT qualification_record_not_self_verified_chk CHECK (verified_by IS NULL OR verified_by <> person_id)
);

-- At most one pending record per person and qualification (F24). Violation: constraint
-- "record_pending_key": Conflict, code pending_exists.
CREATE UNIQUE INDEX record_pending_key ON volunteers.qualification_record (qualification_id, person_id)
    WHERE verified_at IS NULL;

-- A person's records (RecordsOf, export, eligibility, the RESTRICT check of person_id).
CREATE INDEX record_person_idx ON volunteers.qualification_record (person_id);

-- The expiry scan (verified records) and the purge (any expired record, pending included)
-- walk records by expiry date. Partial on expires_on: indefinite records are never looked up
-- by date. (The spec sketched "where verified_at is not null"; the purge also needs the
-- pending ones.)
CREATE INDEX record_expires_idx ON volunteers.qualification_record (expires_on)
    WHERE expires_on IS NOT NULL;

-- The pending queue, oldest first (PendingRecords).
CREATE INDEX record_pending_created_idx ON volunteers.qualification_record (created_at, id)
    WHERE verified_at IS NULL;

-- The RESTRICT check of verified_by, and DeletePerson's null-out (anonymise_person.sql).
CREATE INDEX record_verified_by_idx ON volunteers.qualification_record (verified_by)
    WHERE verified_by IS NOT NULL;

COMMENT ON TABLE volunteers.qualification_record IS
    'sensitivity: internal (which person holds which qualification, D42). Retention: volunteers.retention after expiry (0 = forever); deleted by DeletePerson; certificate file deleted with the record.';
COMMENT ON COLUMN volunteers.qualification_record.id IS 'sensitivity: none (row identifier; also the files record ID)';
COMMENT ON COLUMN volunteers.qualification_record.qualification_id IS 'sensitivity: internal';
COMMENT ON COLUMN volunteers.qualification_record.person_id IS 'sensitivity: internal (who holds it; D42)';
COMMENT ON COLUMN volunteers.qualification_record.acquired_on IS 'sensitivity: internal';
COMMENT ON COLUMN volunteers.qualification_record.expires_on IS 'sensitivity: internal';
COMMENT ON COLUMN volunteers.qualification_record.verified_by IS 'sensitivity: internal (a person ID; NULL when pending and after that lead is deleted)';
COMMENT ON COLUMN volunteers.qualification_record.verified_at IS 'sensitivity: internal (NULL = pending)';
COMMENT ON COLUMN volunteers.qualification_record.certificate_file_id IS 'sensitivity: personal (points at a certificate file, which carries a legal name; files owns the bytes and audits their reads; deleted with the record via files.DeleteByRecord)';
COMMENT ON COLUMN volunteers.qualification_record.expiry_notified_on IS 'sensitivity: internal';

-- ---------------------------------------------------------------------------------------
-- position: a role in a department. Nothing personal.
-- ---------------------------------------------------------------------------------------
CREATE TABLE volunteers.position (
    id               uuid        NOT NULL,
    department_id    uuid        NOT NULL,
    name             text        NOT NULL,
    description      text        NOT NULL DEFAULT '',
    access           text        NOT NULL,
    -- true = volunteers never see who else is on this position's shifts (D42).
    hide_shift_mates boolean     NOT NULL DEFAULT false,
    archived_at      timestamptz,
    created_at       timestamptz NOT NULL,
    updated_at       timestamptz NOT NULL,
    CONSTRAINT position_pkey PRIMARY KEY (id),
    CONSTRAINT position_department_id_fkey FOREIGN KEY (department_id)
        REFERENCES core.department (id) ON DELETE RESTRICT,
    -- Target of shift_position_department_fkey: a shift's position is of the shift's
    -- department, enforced by the database (F71 "scope").
    CONSTRAINT position_id_department_key UNIQUE (id, department_id),
    CONSTRAINT position_nil_uuid_chk CHECK (
        id <> '00000000-0000-0000-0000-000000000000'
        AND department_id <> '00000000-0000-0000-0000-000000000000'),
    CONSTRAINT position_name_length_chk CHECK (octet_length(name) BETWEEN 1 AND 100),
    CONSTRAINT position_name_chars_chk CHECK (name !~ '[[:cntrl:]]'),
    CONSTRAINT position_description_length_chk CHECK (octet_length(description) <= 2000),
    CONSTRAINT position_access_chk CHECK (access IN ('open', 'members'))
);

-- Violation: constraint "position_name_key": Conflict, field name, code taken (F48).
CREATE UNIQUE INDEX position_name_key ON volunteers.position (department_id, lower(name));

COMMENT ON TABLE volunteers.position IS 'sensitivity: none (a role in a department; nothing about a person). Retention: kept forever.';
COMMENT ON COLUMN volunteers.position.id IS 'sensitivity: none';
COMMENT ON COLUMN volunteers.position.department_id IS 'sensitivity: none';
COMMENT ON COLUMN volunteers.position.name IS 'sensitivity: none';
COMMENT ON COLUMN volunteers.position.description IS 'sensitivity: none';
COMMENT ON COLUMN volunteers.position.access IS 'sensitivity: none (open or members)';
COMMENT ON COLUMN volunteers.position.hide_shift_mates IS 'sensitivity: none';
COMMENT ON COLUMN volunteers.position.archived_at IS 'sensitivity: none';

-- ---------------------------------------------------------------------------------------
-- position_requirement: the qualifications a position needs (a set). At most MaxRequirements
-- (20) per position and "org-wide or the position's department" are checked in Go (F53, F55):
-- neither is expressible as a plain constraint.
-- ---------------------------------------------------------------------------------------
CREATE TABLE volunteers.position_requirement (
    position_id      uuid        NOT NULL,
    qualification_id uuid        NOT NULL,
    created_at       timestamptz NOT NULL,
    updated_at       timestamptz NOT NULL,
    CONSTRAINT position_requirement_pkey PRIMARY KEY (position_id, qualification_id),
    CONSTRAINT position_requirement_position_id_fkey FOREIGN KEY (position_id)
        REFERENCES volunteers.position (id) ON DELETE CASCADE,
    CONSTRAINT position_requirement_qualification_id_fkey FOREIGN KEY (qualification_id)
        REFERENCES volunteers.qualification (id) ON DELETE RESTRICT
);

-- "Which positions require this qualification", and the RESTRICT check.
CREATE INDEX position_requirement_qualification_idx ON volunteers.position_requirement (qualification_id);

COMMENT ON TABLE volunteers.position_requirement IS 'sensitivity: none (a position needs a qualification; nothing about a person). Retention: kept forever.';
COMMENT ON COLUMN volunteers.position_requirement.position_id IS 'sensitivity: none';
COMMENT ON COLUMN volunteers.position_requirement.qualification_id IS 'sensitivity: none';

-- ---------------------------------------------------------------------------------------
-- member: who belongs to a department. Current state; never purged.
-- ---------------------------------------------------------------------------------------
CREATE TABLE volunteers.member (
    id            uuid        NOT NULL,
    department_id uuid        NOT NULL,
    person_id     uuid        NOT NULL,
    -- The lead who added the person; NULL after that lead's deletion.
    added_by      uuid,
    created_at    timestamptz NOT NULL,
    updated_at    timestamptz NOT NULL,
    CONSTRAINT member_pkey PRIMARY KEY (id),
    CONSTRAINT member_department_id_fkey FOREIGN KEY (department_id)
        REFERENCES core.department (id) ON DELETE RESTRICT,
    CONSTRAINT member_person_id_fkey FOREIGN KEY (person_id)
        REFERENCES core.person (id) ON DELETE RESTRICT,
    CONSTRAINT member_added_by_fkey FOREIGN KEY (added_by)
        REFERENCES core.person (id) ON DELETE RESTRICT,
    CONSTRAINT member_nil_uuid_chk CHECK (
        id <> '00000000-0000-0000-0000-000000000000'
        AND department_id <> '00000000-0000-0000-0000-000000000000'
        AND person_id <> '00000000-0000-0000-0000-000000000000'
        AND (added_by IS NULL OR added_by <> '00000000-0000-0000-0000-000000000000')),
    -- Violation: constraint "member_department_person_key": Conflict, code duplicate (F61).
    CONSTRAINT member_department_person_key UNIQUE (department_id, person_id)
);

-- A person's memberships (MyDepartments, the person-page section, export, the RESTRICT check
-- of person_id). The unique key above serves lookups by department.
CREATE INDEX member_person_idx ON volunteers.member (person_id);

-- The RESTRICT check of added_by, and DeletePerson's null-out. Partial: set for most rows
-- but only ever looked up when a lead is deleted.
CREATE INDEX member_added_by_idx ON volunteers.member (added_by) WHERE added_by IS NOT NULL;

COMMENT ON TABLE volunteers.member IS
    'sensitivity: internal (who belongs to which department). Retention: current state, never purged; deleted by DeletePerson.';
COMMENT ON COLUMN volunteers.member.id IS 'sensitivity: none';
COMMENT ON COLUMN volunteers.member.department_id IS 'sensitivity: internal';
COMMENT ON COLUMN volunteers.member.person_id IS 'sensitivity: internal (shown to leads and on the person page)';
COMMENT ON COLUMN volunteers.member.added_by IS 'sensitivity: internal (a person ID; nulled by that person''s deletion)';

-- ---------------------------------------------------------------------------------------
-- shift: a block of time in a position, for an event or standing work (event_id NULL).
-- ---------------------------------------------------------------------------------------
CREATE TABLE volunteers.shift (
    id            uuid        NOT NULL,
    department_id uuid        NOT NULL,
    position_id   uuid        NOT NULL,
    event_id      uuid,
    label         text        NOT NULL,
    starts        timestamptz NOT NULL,
    ends          timestamptz NOT NULL,
    -- IANA name; the event's when event_id is set (checked in Go: it needs core.event).
    zone          text        NOT NULL,
    slots         integer     NOT NULL,
    -- Operational ("Ranger HQ").
    location      text        NOT NULL DEFAULT '',
    -- Never personal data (the form says so).
    notes         text        NOT NULL DEFAULT '',
    canceled_at   timestamptz,
    cancel_reason text,
    -- The shift this one was copied from; NULL for an original, and after the source is purged.
    copied_from   uuid,
    created_by    uuid,
    created_at    timestamptz NOT NULL,
    updated_at    timestamptz NOT NULL,
    CONSTRAINT shift_pkey PRIMARY KEY (id),
    CONSTRAINT shift_department_id_fkey FOREIGN KEY (department_id)
        REFERENCES core.department (id) ON DELETE RESTRICT,
    -- Composite: the position must belong to the shift's department (stronger than the spec's
    -- plain position_id key; position_id_department_key is its target). F71 "scope".
    CONSTRAINT shift_position_department_fkey FOREIGN KEY (position_id, department_id)
        REFERENCES volunteers.position (id, department_id) ON DELETE RESTRICT,
    CONSTRAINT shift_event_id_fkey FOREIGN KEY (event_id)
        REFERENCES core.event (id) ON DELETE RESTRICT,
    CONSTRAINT shift_copied_from_fkey FOREIGN KEY (copied_from)
        REFERENCES volunteers.shift (id) ON DELETE SET NULL,
    CONSTRAINT shift_created_by_fkey FOREIGN KEY (created_by)
        REFERENCES core.person (id) ON DELETE RESTRICT,
    CONSTRAINT shift_nil_uuid_chk CHECK (
        id <> '00000000-0000-0000-0000-000000000000'
        AND department_id <> '00000000-0000-0000-0000-000000000000'
        AND position_id <> '00000000-0000-0000-0000-000000000000'
        AND (event_id IS NULL OR event_id <> '00000000-0000-0000-0000-000000000000')
        AND (copied_from IS NULL OR copied_from <> '00000000-0000-0000-0000-000000000000')
        AND (created_by IS NULL OR created_by <> '00000000-0000-0000-0000-000000000000')),
    CONSTRAINT shift_label_length_chk CHECK (octet_length(label) BETWEEN 1 AND 100),
    CONSTRAINT shift_label_chars_chk CHECK (label !~ '[[:cntrl:]]'),
    -- F74: Ends <= Starts, or longer than MaxShiftDuration (48h).
    CONSTRAINT shift_times_chk CHECK (ends > starts AND ends - starts <= interval '48 hours'),
    CONSTRAINT shift_zone_length_chk CHECK (octet_length(zone) BETWEEN 1 AND 64),
    CONSTRAINT shift_slots_chk CHECK (slots BETWEEN 1 AND 200),
    CONSTRAINT shift_location_length_chk CHECK (octet_length(location) <= 200),
    CONSTRAINT shift_notes_length_chk CHECK (octet_length(notes) <= 2000),
    CONSTRAINT shift_cancel_reason_chk CHECK (
        cancel_reason IS NULL
        OR cancel_reason IN ('self', 'lead', 'shift_canceled', 'department_archived', 'event_closed')),
    -- Canceled means both a time and a reason; active means neither.
    CONSTRAINT shift_canceled_chk CHECK ((canceled_at IS NULL) = (cancel_reason IS NULL))
);

-- ListShifts / purge by department and time.
CREATE INDEX shift_department_starts_idx ON volunteers.shift (department_id, starts);
-- ListShifts with no department or event filter: walks shifts in (starts, id) order and stops
-- at the LIMIT. Also the ordering for the keyset cursor.
CREATE INDEX shift_starts_idx ON volunteers.shift (starts, id);
-- ListShifts by event, CopyShifts, the event-closed handler.
CREATE INDEX shift_event_starts_idx ON volunteers.shift (event_id, starts) WHERE event_id IS NOT NULL;
-- ArchivePosition's in_use count, and the RESTRICT check of the composite key.
CREATE INDEX shift_position_idx ON volunteers.shift (position_id);
-- The idempotent copy (F88). Partial: originals have no source. Also serves the
-- ON DELETE SET NULL check of copied_from. Violation: constraint "shift_copied_key" (the
-- copy statement uses ON CONFLICT DO NOTHING, so a caller meets it only through a bug).
CREATE UNIQUE INDEX shift_copied_key ON volunteers.shift (copied_from, event_id)
    WHERE copied_from IS NOT NULL;
-- The purge finds canceled shifts by date. Partial: most shifts are never canceled.
CREATE INDEX shift_canceled_idx ON volunteers.shift (canceled_at) WHERE canceled_at IS NOT NULL;
-- DeletePerson's null-out and the RESTRICT check of created_by.
CREATE INDEX shift_created_by_idx ON volunteers.shift (created_by) WHERE created_by IS NOT NULL;

COMMENT ON TABLE volunteers.shift IS
    'sensitivity: none (a block of time a department needs filled; nothing about a person but created_by). Retention: canceled shifts after volunteers.retention (0 = forever, only when no sign-up is left); others kept.';
COMMENT ON COLUMN volunteers.shift.id IS 'sensitivity: none';
COMMENT ON COLUMN volunteers.shift.department_id IS 'sensitivity: none';
COMMENT ON COLUMN volunteers.shift.position_id IS 'sensitivity: none';
COMMENT ON COLUMN volunteers.shift.event_id IS 'sensitivity: none (NULL = standing work)';
COMMENT ON COLUMN volunteers.shift.label IS 'sensitivity: none';
COMMENT ON COLUMN volunteers.shift.starts IS 'sensitivity: internal (with a sign-up it is a person''s shift time)';
COMMENT ON COLUMN volunteers.shift.ends IS 'sensitivity: internal';
COMMENT ON COLUMN volunteers.shift.zone IS 'sensitivity: none';
COMMENT ON COLUMN volunteers.shift.slots IS 'sensitivity: none';
COMMENT ON COLUMN volunteers.shift.location IS 'sensitivity: none (operational; the form says no personal information)';
COMMENT ON COLUMN volunteers.shift.notes IS 'sensitivity: internal (free text; the form says "no personal information here")';
COMMENT ON COLUMN volunteers.shift.canceled_at IS 'sensitivity: none';
COMMENT ON COLUMN volunteers.shift.cancel_reason IS 'sensitivity: none';
COMMENT ON COLUMN volunteers.shift.copied_from IS 'sensitivity: none';
COMMENT ON COLUMN volunteers.shift.created_by IS 'sensitivity: internal (a person ID; nulled by that person''s deletion)';

-- ---------------------------------------------------------------------------------------
-- signup: a person booked into a shift. History is kept (and, later, credit) until DeletePerson
-- or volunteers.retention after the shift ends.
-- ---------------------------------------------------------------------------------------
CREATE TABLE volunteers.signup (
    id            uuid        NOT NULL,
    shift_id      uuid        NOT NULL,
    person_id     uuid        NOT NULL,
    status        text        NOT NULL,
    -- NULL = the person themself.
    booked_by     uuid,
    -- The lead who assigned despite a missing requirement (D42); NULL otherwise.
    waived_by     uuid,
    canceled_at   timestamptz,
    -- NULL = the system (a signal handler), or the person deleted.
    canceled_by   uuid,
    cancel_reason text,
    created_at    timestamptz NOT NULL,
    updated_at    timestamptz NOT NULL,
    CONSTRAINT signup_pkey PRIMARY KEY (id),
    CONSTRAINT signup_shift_id_fkey FOREIGN KEY (shift_id)
        REFERENCES volunteers.shift (id) ON DELETE RESTRICT,
    CONSTRAINT signup_person_id_fkey FOREIGN KEY (person_id)
        REFERENCES core.person (id) ON DELETE RESTRICT,
    CONSTRAINT signup_booked_by_fkey FOREIGN KEY (booked_by)
        REFERENCES core.person (id) ON DELETE RESTRICT,
    CONSTRAINT signup_waived_by_fkey FOREIGN KEY (waived_by)
        REFERENCES core.person (id) ON DELETE RESTRICT,
    CONSTRAINT signup_canceled_by_fkey FOREIGN KEY (canceled_by)
        REFERENCES core.person (id) ON DELETE RESTRICT,
    CONSTRAINT signup_nil_uuid_chk CHECK (
        id <> '00000000-0000-0000-0000-000000000000'
        AND shift_id <> '00000000-0000-0000-0000-000000000000'
        AND person_id <> '00000000-0000-0000-0000-000000000000'
        AND (booked_by IS NULL OR booked_by <> '00000000-0000-0000-0000-000000000000')
        AND (waived_by IS NULL OR waived_by <> '00000000-0000-0000-0000-000000000000')
        AND (canceled_by IS NULL OR canceled_by <> '00000000-0000-0000-0000-000000000000')),
    CONSTRAINT signup_status_chk CHECK (status IN ('booked', 'canceled')),
    CONSTRAINT signup_cancel_reason_chk CHECK (
        cancel_reason IS NULL
        OR cancel_reason IN ('self', 'lead', 'shift_canceled', 'department_archived', 'event_closed')),
    -- A booked sign-up carries no cancellation; a canceled one carries a time and a reason
    -- (canceled_by may be NULL: the system).
    CONSTRAINT signup_status_fields_chk CHECK (
        (status = 'booked' AND canceled_at IS NULL AND canceled_by IS NULL AND cancel_reason IS NULL)
        OR (status = 'canceled' AND canceled_at IS NOT NULL AND cancel_reason IS NOT NULL))
);

-- A person is booked on a shift at most once (F98). Violation: constraint
-- "signup_booked_key": Conflict, code duplicate. (It is also the lookup for "my booking".)
CREATE UNIQUE INDEX signup_booked_key ON volunteers.signup (shift_id, person_id) WHERE status = 'booked';
-- A person's bookings (overlap check, MySchedule, export, the RESTRICT check of person_id).
CREATE INDEX signup_person_status_idx ON volunteers.signup (person_id, status);
-- A shift's fill count and roster.
CREATE INDEX signup_shift_status_idx ON volunteers.signup (shift_id, status);

-- The RESTRICT checks of the three "who" columns and DeletePerson's null-out
-- (anonymise_person.sql). Partial: most sign-ups are self-booked and never canceled by anyone
-- else, so these stay small.
CREATE INDEX signup_booked_by_idx ON volunteers.signup (booked_by) WHERE booked_by IS NOT NULL;
CREATE INDEX signup_waived_by_idx ON volunteers.signup (waived_by) WHERE waived_by IS NOT NULL;
CREATE INDEX signup_canceled_by_idx ON volunteers.signup (canceled_by) WHERE canceled_by IS NOT NULL;

COMMENT ON TABLE volunteers.signup IS
    'sensitivity: internal (a person booked into a shift: shift times with a person). Retention: volunteers.retention after the shift ends (0 = forever); deleted by DeletePerson.';
COMMENT ON COLUMN volunteers.signup.id IS 'sensitivity: none';
COMMENT ON COLUMN volunteers.signup.shift_id IS 'sensitivity: internal';
COMMENT ON COLUMN volunteers.signup.person_id IS 'sensitivity: internal (who is on which shift)';
COMMENT ON COLUMN volunteers.signup.status IS 'sensitivity: internal';
COMMENT ON COLUMN volunteers.signup.booked_by IS 'sensitivity: internal (a person ID; nulled by that person''s deletion)';
COMMENT ON COLUMN volunteers.signup.waived_by IS 'sensitivity: internal (a person ID; the lead who waived a requirement; nulled by that person''s deletion)';
COMMENT ON COLUMN volunteers.signup.canceled_at IS 'sensitivity: internal';
COMMENT ON COLUMN volunteers.signup.canceled_by IS 'sensitivity: internal (a person ID; NULL = the system; nulled by that person''s deletion)';
COMMENT ON COLUMN volunteers.signup.cancel_reason IS 'sensitivity: internal';

-- ---------------------------------------------------------------------------------------
-- unlisted: people who chose not to appear in shift-mate lists (SetListed(false)). No row =
-- listed. Current state; never purged.
-- ---------------------------------------------------------------------------------------
CREATE TABLE volunteers.unlisted (
    person_id  uuid        NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT unlisted_pkey PRIMARY KEY (person_id),
    CONSTRAINT unlisted_person_id_fkey FOREIGN KEY (person_id)
        REFERENCES core.person (id) ON DELETE RESTRICT,
    CONSTRAINT unlisted_nil_uuid_chk CHECK (person_id <> '00000000-0000-0000-0000-000000000000')
);

COMMENT ON TABLE volunteers.unlisted IS
    'sensitivity: internal (a person''s choice not to be listed; read only by that person and by the list computation, never shown as who). Retention: current state, never purged; deleted by SetListed(true) or DeletePerson.';
COMMENT ON COLUMN volunteers.unlisted.person_id IS 'sensitivity: internal';
