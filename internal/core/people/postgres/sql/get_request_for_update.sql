-- Store.RequestForUpdate: FulfilRequest, inside the Write transaction. The person is learned
-- from the request, so the order is: get_request.sql (person), lock_person_deletion.sql,
-- then this statement to check the status under the lock, then the deletion. The row lock
-- makes a concurrent withdraw or decline wait. Zero rows: NotFound.
-- Args: $1 id uuid. Columns: as insert_request.sql.
SELECT id, person_id, kind, status, note, created_at, decided_at, decided_by, decision_note
FROM core.data_request
WHERE id = $1
FOR UPDATE
