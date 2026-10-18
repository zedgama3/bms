-- audit 0001: the append-only audit log, its guard and the one sanctioned deletion path.
-- Schema "core" is created by the migrator. Creates new objects only; touches no existing
-- table, so it is safe on any database.
--
-- Table core.audit_entry holds personal data (actor_person_id, subject_person_id, reason).
-- Retention: audit.retention (settings, default 0 = keep forever, D30). The retention job
-- (jobs, M4) calls Log.Purge, which calls core.audit_purge below: the only way a row leaves.
-- Rows keep their person IDs after a person is deleted (audit.md decision 10).

CREATE TABLE core.audit_entry (
    id                uuid        NOT NULL,
    at                timestamptz NOT NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    instance          text        NOT NULL,
    trace_id          text,
    spooled           boolean     NOT NULL DEFAULT false,
    action            text        NOT NULL,
    outcome           text        NOT NULL,
    actor_kind        text        NOT NULL,
    actor_person_id   uuid,
    actor_superuser   boolean     NOT NULL DEFAULT false,
    actor_titles      text[]      NOT NULL DEFAULT '{}',
    subject_person_id uuid,
    record_kind       text,
    record_id         uuid,
    fields            text[]      NOT NULL DEFAULT '{}',
    level             text,
    reason            text,
    -- Append-only: no updated_at (audit.md "Data"; exempt from the postgres-sql rule).
    -- No foreign keys: an actor or subject may be deleted, or arrive from another copy
    -- before its person row does (decision 10).
    CONSTRAINT audit_entry_pkey PRIMARY KEY (id),

    -- Grammars and limits mirror the Go side; bytes, not characters. All are ASCII, so
    -- length() equals octet_length().
    CONSTRAINT audit_entry_instance_chk CHECK (
        instance ~ '^[a-z0-9]([a-z0-9-]*[a-z0-9])?$' AND octet_length(instance) BETWEEN 1 AND 64),
    CONSTRAINT audit_entry_trace_id_chk CHECK (trace_id IS NULL OR trace_id ~ '^[0-9a-f]{32}$'),
    CONSTRAINT audit_entry_action_chk CHECK (
        action ~ '^[a-z][a-z0-9]*(-[a-z0-9]+)*(\.[a-z][a-z0-9]*(-[a-z0-9]+)*)+$'
        AND octet_length(action) <= 96),
    CONSTRAINT audit_entry_outcome_chk CHECK (outcome IN ('succeeded', 'denied', 'failed')),
    CONSTRAINT audit_entry_actor_kind_chk CHECK (
        actor_kind IN ('person', 'anonymous', 'operator', 'system')),
    -- actor_person_id is set iff the actor is a person; the superuser mark needs a person.
    CONSTRAINT audit_entry_actor_person_chk CHECK ((actor_kind = 'person') = (actor_person_id IS NOT NULL)),
    CONSTRAINT audit_entry_actor_superuser_chk CHECK (NOT actor_superuser OR actor_kind = 'person'),
    -- audit.MaxTitles = 16, audit.MaxTitleBytes = 64. Per-element length cannot be checked
    -- without a subquery; the joined length bounds the total (16*64 + 15 separators).
    CONSTRAINT audit_entry_actor_titles_chk CHECK (
        cardinality(actor_titles) <= 16
        AND array_position(actor_titles, NULL) IS NULL
        AND octet_length(array_to_string(actor_titles, chr(31))) <= 1039),
    -- record_kind and record_id are both set or both null.
    CONSTRAINT audit_entry_record_pair_chk CHECK ((record_kind IS NULL) = (record_id IS NULL)),
    CONSTRAINT audit_entry_record_kind_chk CHECK (
        record_kind IS NULL
        OR (record_kind ~ '^[a-z][a-z0-9]*(\.[a-z][a-z0-9_-]*)+$' AND octet_length(record_kind) <= 96)),
    -- The all-zero UUID means "none" in Go; it must never be stored.
    CONSTRAINT audit_entry_nil_uuid_chk CHECK (
        actor_person_id   IS DISTINCT FROM '00000000-0000-0000-0000-000000000000'
        AND subject_person_id IS DISTINCT FROM '00000000-0000-0000-0000-000000000000'
        AND record_id         IS DISTINCT FROM '00000000-0000-0000-0000-000000000000'),
    -- audit.MaxFields = 64, audit.MaxFieldBytes = 64; names only, "[a-z][a-z0-9_]*".
    -- Uniqueness is checked by Entry.Validate (a CHECK cannot use a subquery).
    CONSTRAINT audit_entry_fields_chk CHECK (
        cardinality(fields) <= 64
        AND array_position(fields, NULL) IS NULL
        AND (cardinality(fields) = 0
             OR array_to_string(fields, ',') ~ '^[a-z][a-z0-9_]{0,63}(,[a-z][a-z0-9_]{0,63})*$')),
    CONSTRAINT audit_entry_level_chk CHECK (
        level IS NULL OR level IN ('public', 'internal', 'personal', 'sensitive')),
    -- level is null iff fields is empty.
    CONSTRAINT audit_entry_level_fields_chk CHECK ((level IS NULL) = (cardinality(fields) = 0)),
    -- audit.MaxReasonBytes = 256; an empty reason is stored as NULL.
    CONSTRAINT audit_entry_reason_chk CHECK (reason IS NULL OR octet_length(reason) BETWEEN 1 AND 256)
);

-- Keyset order everywhere is (at DESC, id DESC).
-- Search by subject, and the per-person export (scanned backwards for oldest first).
CREATE INDEX audit_entry_subject_idx ON core.audit_entry (subject_person_id, at DESC, id DESC)
    WHERE subject_person_id IS NOT NULL;
-- Search by actor.
CREATE INDEX audit_entry_actor_idx ON core.audit_entry (actor_person_id, at DESC, id DESC)
    WHERE actor_person_id IS NOT NULL;
-- Search by record.
CREATE INDEX audit_entry_record_idx ON core.audit_entry (record_kind, record_id, at DESC, id DESC)
    WHERE record_id IS NOT NULL;
-- Unfiltered and date-range search, and the retention purge (at < before).
CREATE INDEX audit_entry_at_idx ON core.audit_entry (at DESC, id DESC);
-- Search "superuser only".
CREATE INDEX audit_entry_superuser_idx ON core.audit_entry (at DESC) WHERE actor_superuser;

-- The guard. UPDATE and TRUNCATE are always refused; DELETE only while the transaction-local
-- flag bms.audit_purge is 'on', which core.audit_purge sets. P0001 (raise_exception) is what
-- db.Wrap maps to Internal: a refused write is a bug, not a user error.
CREATE FUNCTION core.audit_entry_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' AND current_setting('bms.audit_purge', true) = 'on' THEN
        RETURN OLD;
    END IF;
    RAISE EXCEPTION 'audit entries are append-only'
        USING HINT = 'rows leave core.audit_entry only through core.audit_purge';
END;
$$;

CREATE TRIGGER audit_entry_guard_row
    BEFORE UPDATE OR DELETE ON core.audit_entry
    FOR EACH ROW EXECUTE FUNCTION core.audit_entry_guard();

CREATE TRIGGER audit_entry_guard_truncate
    BEFORE TRUNCATE ON core.audit_entry
    FOR EACH STATEMENT EXECUTE FUNCTION core.audit_entry_guard();

-- A guard rail against bugs, not against the operator, who can always re-grant (audit.md
-- decision 9). The installation has one role, so DELETE cannot be revoked: the trigger
-- decides.
REVOKE UPDATE, TRUNCATE ON core.audit_entry FROM CURRENT_USER;

-- The one sanctioned deletion: removes entries with at < before and returns the count.
-- It sets the transaction-local flag the guard honours and clears it before returning, so a
-- plain DELETE later in the same transaction is refused again. (A function-level SET clause
-- would restore it automatically, but PostgreSQL lets only a superuser put an unknown custom
-- parameter in one, and the application role is not one.) Called as
-- SELECT core.audit_purge($1) in a Write transaction; on a read-only copy the DELETE fails
-- 25006, which db maps to ErrReadOnly. Log.Purge also checks its argument in Go (F40); the checks here are a rail.
CREATE FUNCTION core.audit_purge(before timestamptz) RETURNS bigint
LANGUAGE plpgsql AS $$
DECLARE
    deleted bigint;
BEGIN
    IF before IS NULL THEN
        RAISE EXCEPTION 'before must not be null' USING ERRCODE = '22004';
    END IF;
    -- A safety rail: a cutoff in the future (or 'infinity') would delete the whole log. A real
    -- purge is at least audit.retention (floor 365 days) in the past. 22023 maps to Invalid.
    IF before > now() THEN
        RAISE EXCEPTION 'before must not be in the future' USING ERRCODE = '22023';
    END IF;
    PERFORM set_config('bms.audit_purge', 'on', true);
    DELETE FROM core.audit_entry WHERE at < before;
    GET DIAGNOSTICS deleted = ROW_COUNT;
    PERFORM set_config('bms.audit_purge', 'off', true);
    RETURN deleted;
END;
$$;

COMMENT ON TABLE core.audit_entry IS
    'Append-only audit log (UPDATE/DELETE/TRUNCATE refused by trigger audit_entry_guard_row/_truncate). Holds personal data: actor_person_id, subject_person_id, reason. Retention: setting audit.retention (default forever), purged only by core.audit_purge; person IDs are kept after a person is deleted.';
COMMENT ON FUNCTION core.audit_entry_guard() IS 'refuses UPDATE, TRUNCATE, and DELETE unless bms.audit_purge = on';
COMMENT ON FUNCTION core.audit_purge(timestamptz) IS 'the only deletion path for core.audit_entry: deletes entries with at < before, returns the count; retention job via audit.Purge';

COMMENT ON COLUMN core.audit_entry.id IS 'sensitivity: none (row identifier)';
COMMENT ON COLUMN core.audit_entry.at IS 'sensitivity: internal';
COMMENT ON COLUMN core.audit_entry.created_at IS 'sensitivity: internal';
COMMENT ON COLUMN core.audit_entry.instance IS 'sensitivity: internal';
COMMENT ON COLUMN core.audit_entry.trace_id IS 'sensitivity: internal';
COMMENT ON COLUMN core.audit_entry.spooled IS 'sensitivity: internal';
COMMENT ON COLUMN core.audit_entry.action IS 'sensitivity: internal';
COMMENT ON COLUMN core.audit_entry.outcome IS 'sensitivity: internal';
COMMENT ON COLUMN core.audit_entry.actor_kind IS 'sensitivity: internal';
COMMENT ON COLUMN core.audit_entry.actor_person_id IS 'sensitivity: personal';
COMMENT ON COLUMN core.audit_entry.actor_superuser IS 'sensitivity: internal';
COMMENT ON COLUMN core.audit_entry.actor_titles IS 'sensitivity: internal';
COMMENT ON COLUMN core.audit_entry.subject_person_id IS 'sensitivity: personal';
COMMENT ON COLUMN core.audit_entry.record_kind IS 'sensitivity: internal';
COMMENT ON COLUMN core.audit_entry.record_id IS 'sensitivity: internal';
COMMENT ON COLUMN core.audit_entry.fields IS 'sensitivity: internal (field names only, never values)';
COMMENT ON COLUMN core.audit_entry.level IS 'sensitivity: internal';
COMMENT ON COLUMN core.audit_entry.reason IS 'sensitivity: personal (what the person typed; shown to audit reviewers only)';
