-- Store.GetContact: one contact of one person. Read-only; use Query or QueryRow (zero rows:
-- NotFound, also when the contact belongs to someone else). Args: $1 person_id uuid, $2 id
-- uuid. Columns, in order: as list_contacts_by_person.sql.
SELECT id, person_id, name, phone, relationship, position, created_at, updated_at
FROM core.person_emergency_contact
WHERE person_id = $1 AND id = $2
