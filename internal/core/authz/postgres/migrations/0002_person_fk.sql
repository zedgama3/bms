-- authz 0002: foreign keys from core.role_grant to core.person (authz.md decision 14 and
-- Proposed change 9(f); people.md "Foreign keys from elsewhere"). Adds constraints and one
-- index; creates no table and rewrites none. Depends on core.person from people 0001, which
-- runs first (db's fixed set order: identity, people, authz, audit, then modules).
--
-- person_id: ON DELETE RESTRICT. people.Delete calls authz.DeletePerson (which deletes the
-- person's grants, ended ones too) before it deletes the core.person row, so a grant left
-- behind is a bug that must fail loudly (people.md F37: Conflict in_use), not vanish in a
-- cascade. A grant to an unknown person fails with 23503 on "role_grant_person_id_fkey",
-- which the authz service reports as Invalid, field person, code unknown.
--
-- granted_by: ON DELETE SET NULL. A grant outlives its granter ("NULL: the operator, a seed,
-- or the granter was deleted"); authz.DeletePerson also clears it explicitly, the foreign
-- key is the safety net. Constraint "role_grant_granted_by_fkey". Note this makes an actor
-- whose core.person row is missing (the account-created handler failed and no UpdateMe has
-- repaired it yet, people.md decision 2) unable to grant: the insert fails on this key.
--
-- Safe on a database with data. people 0001 backfilled one core.person row per account, so
-- grants made by or to any account holder are covered. Two cases can still exist from the
-- M1 window, when authz did not check the person:
--   * a granted_by that has no person row: set NULL first (the same state as "the granter was
--     deleted"; no information about a living person is lost).
--   * a person_id that has no person row: a grant to nobody. Not deleted silently (that is
--     an access-control record and history). The migration stops with a foreign_key_violation
--     and rolls back whole. What the operator sees from `bms migrate` is the error's table
--     and constraint (core.role_grant, role_grant_person_id_fkey): db keeps only the code,
--     schema, table, column and constraint, not the message. The message (the row count,
--     never IDs or values) reaches the server log and psql only. The operator decides
--     (delete the rows, or create the person), then runs migrate again.
--
-- Locking: ADD CONSTRAINT ... FOREIGN KEY takes SHARE ROW EXCLUSIVE on core.role_grant and on
-- core.person for the rest of the transaction: reads continue, writes to those two tables
-- (sign-up's person row, grants) wait. It also scans role_grant once to validate. The
-- migrator runs each file in one transaction, so the NOT VALID + VALIDATE CONSTRAINT split
-- would not shorten the lock window (the first statement's lock is held to commit), and
-- role_grant is tiny (grants number in the thousands at most). Not split.

DO $$
DECLARE
    orphans bigint;
BEGIN
    SELECT count(*) INTO orphans
    FROM core.role_grant g
    WHERE NOT EXISTS (SELECT 1 FROM core.person p WHERE p.id = g.person_id);
    IF orphans > 0 THEN
        RAISE EXCEPTION 'authz 0002: % row(s) in core.role_grant name a person with no core.person row; delete them or create the person, then run migrate again', orphans
            USING ERRCODE = 'foreign_key_violation', SCHEMA = 'core', TABLE = 'role_grant',
                  CONSTRAINT = 'role_grant_person_id_fkey';
    END IF;
END
$$;

UPDATE core.role_grant g
SET granted_by = NULL
WHERE g.granted_by IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM core.person p WHERE p.id = g.granted_by);

ALTER TABLE core.role_grant
    ADD CONSTRAINT role_grant_person_id_fkey FOREIGN KEY (person_id)
        REFERENCES core.person (id) ON DELETE RESTRICT,
    ADD CONSTRAINT role_grant_granted_by_fkey FOREIGN KEY (granted_by)
        REFERENCES core.person (id) ON DELETE SET NULL;

-- person_id is served by role_grant_person_id_idx (0001). granted_by needs its own index or
-- every person deletion scans role_grant for the SET NULL action (and authz.DeletePerson's
-- explicit clear). Partial: most grants are made by someone, but the index stays small and
-- rows with NULL (operator, seed) are never looked up by it.
CREATE INDEX role_grant_granted_by_idx ON core.role_grant (granted_by)
    WHERE granted_by IS NOT NULL;
