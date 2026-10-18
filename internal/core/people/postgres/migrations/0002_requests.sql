-- people 0002: deletion requests. Creates a new table only; touches nothing that exists, so
-- it is safe on any database. Depends on core.person from 0001.
--
-- note and decision_note are the person's and the decider's own words: personal. Retention:
-- a row lives until its person is deleted (the foreign key cascades as a safety net;
-- Service.Delete removes requests explicitly so the count is reported). A fulfilled request
-- disappears with its person; the trail is the audit log (people.request.fulfilled,
-- people.person.deleted). No retention setting (people.md decision 17).

CREATE TABLE core.data_request (
    id            uuid        NOT NULL,
    person_id     uuid        NOT NULL,
    -- 'delete' in M2; the column exists so an export request kind needs no migration (widen
    -- the check then).
    kind          text        NOT NULL,
    status        text        NOT NULL,
    note          text        NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL,
    -- NULL while open; set when withdrawn or declined.
    decided_at    timestamptz,
    -- NULL while open, when withdrawn by the person, and when the decider is deleted.
    decided_by    uuid,
    decision_note text        NOT NULL DEFAULT '',
    CONSTRAINT data_request_pkey PRIMARY KEY (id),
    CONSTRAINT data_request_person_id_fkey FOREIGN KEY (person_id)
        REFERENCES core.person (id) ON DELETE CASCADE,
    CONSTRAINT data_request_decided_by_fkey FOREIGN KEY (decided_by)
        REFERENCES core.person (id) ON DELETE SET NULL,
    CONSTRAINT data_request_nil_uuid_chk CHECK (
        id <> '00000000-0000-0000-0000-000000000000'
        AND person_id <> '00000000-0000-0000-0000-000000000000'
        AND (decided_by IS NULL OR decided_by <> '00000000-0000-0000-0000-000000000000')),
    CONSTRAINT data_request_kind_chk CHECK (kind = 'delete'),
    CONSTRAINT data_request_status_chk CHECK (status IN ('open', 'withdrawn', 'declined')),
    -- decided_at is set exactly when the request is no longer open.
    CONSTRAINT data_request_decided_chk CHECK ((status = 'open') = (decided_at IS NULL)),
    CONSTRAINT data_request_note_length_chk CHECK (octet_length(note) <= 500),
    CONSTRAINT data_request_decision_note_length_chk CHECK (octet_length(decision_note) <= 500)
);

-- One open request per person. A race on two simultaneous requests fails 23505 on this
-- index (db.Wrap: Conflict, Constraint "data_request_open_idx"): Conflict duplicate.
CREATE UNIQUE INDEX data_request_open_idx ON core.data_request (person_id) WHERE status = 'open';

-- The requests screen: by status, oldest first, keyset on (created_at, id).
CREATE INDEX data_request_status_created_at_id_idx ON core.data_request (status, created_at, id);

-- A person's own requests, newest first, keyset on (created_at, id); also the per-person
-- filter of the requests screen and the delete and export by person.
CREATE INDEX data_request_person_created_at_id_idx ON core.data_request (person_id, created_at DESC, id DESC);

COMMENT ON TABLE core.data_request IS
    'A person''s request to have their data deleted, and what became of it (people). Carries personal free text. Retention: kept until Service.Delete (explicitly, so the count is reported); no retention setting. Export: Service.Export.';
COMMENT ON COLUMN core.data_request.id IS 'sensitivity: internal (row identifier)';
COMMENT ON COLUMN core.data_request.person_id IS 'sensitivity: internal';
COMMENT ON COLUMN core.data_request.kind IS 'sensitivity: internal (delete in M2)';
COMMENT ON COLUMN core.data_request.status IS 'sensitivity: internal (open, withdrawn or declined)';
COMMENT ON COLUMN core.data_request.note IS 'sensitivity: personal (the person''s own words; shown to the person and to people.data.delete, audited)';
COMMENT ON COLUMN core.data_request.created_at IS 'sensitivity: internal';
COMMENT ON COLUMN core.data_request.decided_at IS 'sensitivity: internal';
COMMENT ON COLUMN core.data_request.decided_by IS 'sensitivity: internal (a person ID; set NULL when the decider is deleted)';
COMMENT ON COLUMN core.data_request.decision_note IS 'sensitivity: personal (the decider''s words to the person)';
