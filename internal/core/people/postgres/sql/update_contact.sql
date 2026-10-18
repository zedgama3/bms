-- Store.UpdateContact. Use Query; zero rows: NotFound (no such contact, or it is another
-- person's: the same answer, F22). The position does not change.
-- Args: $1 id uuid, $2 person_id uuid, $3 name text, $4 phone text, $5 relationship text,
-- $6 updated_at timestamptz.
-- Columns, in order: as list_contacts_by_person.sql.
UPDATE core.person_emergency_contact SET
    name = $3::text, phone = $4::text, relationship = $5::text, updated_at = $6::timestamptz
WHERE id = $1 AND person_id = $2
RETURNING id, person_id, name, phone, relationship, position, created_at, updated_at
