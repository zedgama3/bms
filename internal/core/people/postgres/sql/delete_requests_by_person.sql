-- Store.DeleteRequestsByPerson: Delete, before the person row. Use Query; the row count is
-- the count. Idempotent. Requests this person decided about others keep, with decided_by
-- set NULL when the person row goes. Args: $1 person_id uuid. Columns, in order: id uuid.
DELETE FROM core.data_request
WHERE person_id = $1
RETURNING id
