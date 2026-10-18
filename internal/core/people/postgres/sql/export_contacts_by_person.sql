-- Store.ExportContacts: the "emergency_contacts" array of the export document. Read-only;
-- use Query (zero rows: []). Args: $1 person_id uuid.
-- Columns, in order: id uuid, person_id uuid, name text, phone text, relationship text,
-- position integer, created_at timestamptz, updated_at timestamptz (as list_contacts_by_person.sql;
-- the document uses name, phone, relationship, position). Display order.
SELECT id, person_id, name, phone, relationship, position, created_at, updated_at
FROM core.person_emergency_contact
WHERE person_id = $1
ORDER BY position
