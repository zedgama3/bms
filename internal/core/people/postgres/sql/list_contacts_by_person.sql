-- Store.Contacts: a person's emergency contacts in display order. Read-only; use Query.
-- Bounded (MaxEmergencyContacts = 10 enforced by insert_contact.sql), so no paging.
-- Args: $1 person_id uuid.
-- Columns, in order: id uuid, person_id uuid, name text, phone text, relationship text,
-- position integer, created_at timestamptz, updated_at timestamptz. Served by
-- person_emergency_contact_person_position_key.
SELECT id, person_id, name, phone, relationship, position, created_at, updated_at
FROM core.person_emergency_contact
WHERE person_id = $1
ORDER BY position
