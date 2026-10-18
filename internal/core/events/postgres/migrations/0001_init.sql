-- events 0001: event kinds, events and their schedule items, and the three seeded kinds
-- (departments-events.md, "Data"). Schema "core" is created by the migrator. Creates new
-- objects only and touches no existing table, so it is safe on any database.
--
-- The three kinds (burn, decompression, other) are inserted here, flagged builtin; this is the
-- one time a core migration writes data besides authz's starter roles. Their timestamps are
-- this migration's now(). The UI shows builtin kinds through catalogue keys
-- core.events.kind.<key>, so the seeded names are English fallbacks.
--
-- Nothing here is personal data: every column is "internal" and kept forever (events are
-- closed, never deleted; kinds are never deleted). There is no retention job and no export or
-- deletion step. No column references core.person. Modules may reference core.event(id) with
-- a foreign key ON DELETE RESTRICT but never read these tables (D10).
--
-- Timestamps come from the service's clock, so none has a default (the seed rows aside).

CREATE TABLE core.event_kind (
    key        text        NOT NULL,
    name       text        NOT NULL,
    builtin    boolean     NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    -- A duplicate key reports constraint "event_kind_pkey": Conflict, field key, code taken.
    CONSTRAINT event_kind_pkey PRIMARY KEY (key),
    -- Same grammar as module-contract idents; MaxKindKeyBytes = 32.
    CONSTRAINT event_kind_key_chk CHECK (
        octet_length(key) <= 32 AND key ~ '^[a-z][a-z0-9]*(-[a-z0-9]+)*$'),
    -- MaxKindNameBytes = 60.
    CONSTRAINT event_kind_name_length_chk CHECK (octet_length(name) BETWEEN 1 AND 60),
    CONSTRAINT event_kind_name_chars_chk CHECK (name !~ '[[:cntrl:]]')
);

INSERT INTO core.event_kind (key, name, builtin, created_at, updated_at) VALUES
    ('burn',          'Burn',          true, now(), now()),
    ('decompression', 'Decompression', true, now(), now()),
    ('other',         'Other',         true, now(), now());

CREATE TABLE core.event (
    id         uuid        NOT NULL,
    name       text        NOT NULL,
    kind       text        NOT NULL,
    -- Instants; the range is half-open (model.TimeRange).
    starts_at  timestamptz NOT NULL,
    ends_at    timestamptz NOT NULL,
    -- IANA name the dates are shown in; validated in Go with time.LoadLocation.
    zone       text        NOT NULL,
    -- NULL = open.
    closed_at  timestamptz,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT event_pkey PRIMARY KEY (id),
    -- An unknown kind reports constraint "event_kind_fkey" (23503): Invalid, field kind, code
    -- unknown.
    CONSTRAINT event_kind_fkey FOREIGN KEY (kind) REFERENCES core.event_kind (key)
        ON DELETE RESTRICT,
    CONSTRAINT event_nil_uuid_chk CHECK (id <> '00000000-0000-0000-0000-000000000000'),
    -- Bytes, to match the Go side (MaxNameBytes, MaxZoneBytes).
    CONSTRAINT event_name_length_chk CHECK (octet_length(name) BETWEEN 1 AND 100),
    CONSTRAINT event_name_chars_chk CHECK (name !~ '[[:cntrl:]]'),
    CONSTRAINT event_dates_chk CHECK (ends_at > starts_at),
    CONSTRAINT event_zone_length_chk CHECK (octet_length(zone) BETWEEN 1 AND 64)
);

-- Names are unique ignoring case among all events, closed included (decision 15). A violation
-- reports constraint "event_name_key": Conflict, field name, code taken.
CREATE UNIQUE INDEX event_name_key ON core.event (lower(name));

-- List is newest first.
CREATE INDEX event_starts_at_idx ON core.event (starts_at DESC);

CREATE TABLE core.event_schedule_item (
    id         uuid        NOT NULL,
    event_id   uuid        NOT NULL,
    -- An optional token modules anchor to ("temple-burn"); '' = none. Unique per event.
    key        text        NOT NULL DEFAULT '',
    label      text        NOT NULL,
    starts_at  timestamptz NOT NULL,
    -- NULL = a moment.
    ends_at    timestamptz,
    -- Insertion order; ties in starts_at sort by it.
    position   integer     NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT event_schedule_item_pkey PRIMARY KEY (id),
    CONSTRAINT event_schedule_item_event_id_fkey FOREIGN KEY (event_id) REFERENCES core.event (id)
        ON DELETE RESTRICT,
    CONSTRAINT event_schedule_item_nil_uuid_chk CHECK (id <> '00000000-0000-0000-0000-000000000000'),
    CONSTRAINT event_schedule_item_key_chk CHECK (
        key = '' OR (octet_length(key) <= 32 AND key ~ '^[a-z][a-z0-9]*(-[a-z0-9]+)*$')),
    -- MaxLabelBytes = 100.
    CONSTRAINT event_schedule_item_label_length_chk CHECK (octet_length(label) BETWEEN 1 AND 100),
    CONSTRAINT event_schedule_item_label_chars_chk CHECK (label !~ '[[:cntrl:]]'),
    CONSTRAINT event_schedule_item_ends_chk CHECK (ends_at IS NULL OR ends_at > starts_at)
);

-- A repeated key within an event reports constraint "event_schedule_key": Conflict, field key,
-- code duplicate.
CREATE UNIQUE INDEX event_schedule_key ON core.event_schedule_item (event_id, key) WHERE key <> '';

-- Get reads one event's schedule sorted by (starts_at, position); the event_id prefix also
-- serves the foreign key.
CREATE INDEX event_schedule_item_event_idx
    ON core.event_schedule_item (event_id, starts_at, position);
