-- hello 0002: foreign key from hello.greeting.person_id to core.person(id)
-- (module-hello.md decision 9; people.md Proposed change 8). Adds one constraint; creates no
-- table and rewrites none. core.person comes from people 0001, which runs before any module.
--
-- ON DELETE RESTRICT, not CASCADE (this replaces the "on delete cascade" sketch in 0001's
-- trailing comment): people.Delete calls hello's DeletePerson first, and a cascade would
-- hide a module that forgot to. A row left behind fails the person's deletion with
-- Conflict in_use naming "greeting_person_id_fkey" (people.md F37).
--
-- Safe on a database with data. A greeting is written only by a signed-in person, and
-- people 0001 backfilled a core.person row for every account, so rows normally validate.
-- If one does not (a greeting by a person who has no row), the migration stops with a
-- foreign_key_violation and rolls back whole. What the operator sees from `bms migrate` is
-- the error's table and constraint (hello.greeting, greeting_person_id_fkey): db keeps only
-- the code, schema, table, column and constraint, not the message. The message (the row
-- count, never IDs or values) reaches the server log and psql only. The operator deletes
-- those rows (hello's DeletePerson would) or creates the person, and runs migrate again.
-- Nothing is deleted silently: they are a person's saved text.
--
-- Locking: ADD CONSTRAINT ... FOREIGN KEY takes SHARE ROW EXCLUSIVE on hello.greeting and
-- on core.person until the transaction ends: reads continue, writes to those two tables
-- wait, and hello.greeting is scanned once to validate. The migrator runs each file in one
-- transaction, so splitting into NOT VALID + VALIDATE CONSTRAINT would not shorten the lock
-- window; the table is small (a few rows per person). Not split.
--
-- No new index: greeting_person_created_idx (0001) leads with person_id, so it serves the
-- validation scan and the RESTRICT check on a person's deletion.

DO $$
DECLARE
    orphans bigint;
BEGIN
    SELECT count(*) INTO orphans
    FROM hello.greeting g
    WHERE NOT EXISTS (SELECT 1 FROM core.person p WHERE p.id = g.person_id);
    IF orphans > 0 THEN
        RAISE EXCEPTION 'hello 0002: % row(s) in hello.greeting name a person with no core.person row; delete them or create the person, then run migrate again', orphans
            USING ERRCODE = 'foreign_key_violation', SCHEMA = 'hello', TABLE = 'greeting',
                  CONSTRAINT = 'greeting_person_id_fkey';
    END IF;
END
$$;

ALTER TABLE hello.greeting
    ADD CONSTRAINT greeting_person_id_fkey FOREIGN KEY (person_id)
        REFERENCES core.person (id) ON DELETE RESTRICT;
