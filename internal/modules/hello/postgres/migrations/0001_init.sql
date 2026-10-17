-- hello 0001: the module's one table. Schema "hello" is created by the migrator.
-- Safe on any database: creates new objects only, touches no existing table.

CREATE TABLE hello.greeting (
    id         uuid        NOT NULL,
    person_id  uuid        NOT NULL,
    greeting   text        NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT greeting_pkey PRIMARY KEY (id),
    -- Bytes, to match the Go side (greeting.MaxBytes = 64); bytes >= characters.
    CONSTRAINT greeting_text_length_chk CHECK (octet_length(greeting) BETWEEN 1 AND 64)
);

-- Serves CountByPerson, ListByPerson (ordered by created_at) and DeleteByPerson.
CREATE INDEX greeting_person_created_idx ON hello.greeting (person_id, created_at);

-- No foreign key on person_id: the core has no person table yet. Migration 0002 adds
-- "references core.person(id) on delete cascade" when people ships its table (M2).

COMMENT ON TABLE hello.greeting IS
    'Greetings saved by signed-in people (hello reference module). Retention: kept until DeletePerson; no retention setting in M0.';
COMMENT ON COLUMN hello.greeting.id IS 'sensitivity: none (row identifier)';
COMMENT ON COLUMN hello.greeting.person_id IS 'sensitivity: internal';
COMMENT ON COLUMN hello.greeting.greeting IS 'sensitivity: internal';
COMMENT ON COLUMN hello.greeting.created_at IS 'sensitivity: internal';
COMMENT ON COLUMN hello.greeting.updated_at IS 'sensitivity: internal';
