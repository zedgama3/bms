-- Store.DeletePerson: the last step of Delete, after the modules, authz, identity and
-- delete_requests/consents/contacts_by_person.sql. Use Query; the row count is the count in
-- the report (zero: already gone, NotFound). A foreign key still pointing at the person
-- (a module that forgot a table) fails 23503 with the constraint name; the store re-kinds it
-- to Conflict in_use. Rows of other people that name this person (created_by, recorded_by,
-- decided_by) are set NULL by their foreign keys. Args: $1 person_id uuid. Columns: id uuid.
DELETE FROM core.person
WHERE id = $1
RETURNING id
