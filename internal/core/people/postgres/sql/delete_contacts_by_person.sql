-- Store.DeleteContactsByPerson: Delete, before the person row, so the report counts them.
-- Use Query; the row count is the count. Idempotent: no row, no error.
-- Args: $1 person_id uuid. Columns, in order: id uuid.
DELETE FROM core.person_emergency_contact
WHERE person_id = $1
RETURNING id
