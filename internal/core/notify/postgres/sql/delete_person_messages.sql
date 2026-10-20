-- Store.DeletePerson: delete every row addressed to one person, whatever its status (queued
-- rows are never sent). Runs in people's Write transaction, before core.person is deleted
-- (the foreign key is RESTRICT). Use QueryRow (one row).
-- Args: $1 person_id uuid.
-- Columns, in order: deleted bigint (scan into int64).
-- Served by notify_message_person_id_idx. A row a sender is delivering at this moment is
-- deleted too; its later mark_* then matches nothing. Fails 25006 (db.ErrReadOnly) on a
-- read-only copy.
-- Constraint errors: none reachable.
WITH gone AS (
    DELETE FROM core.notify_message
    WHERE person_id = $1::uuid
    RETURNING 1
)
SELECT count(*) AS deleted FROM gone
