-- Store.LockDeletion: first statement of Delete, FulfilRequest and DeleteCommand, inside the
-- Write transaction, so two deletions of one person serialise (the second then finds no
-- person: NotFound). Released at commit or rollback. Use Exec.
-- Args: $1 person_id uuid. No columns that matter (void).
SELECT pg_advisory_xact_lock(hashtext('people.delete:' || $1::uuid::text))
