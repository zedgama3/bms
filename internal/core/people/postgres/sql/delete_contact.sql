-- Store.RemoveContact, step 1. Use Query; zero rows: NotFound (no such contact, or another
-- person's). Follow with renumber_contacts.sql in the same transaction.
-- Args: $1 id uuid, $2 person_id uuid. Columns, in order: id uuid.
DELETE FROM core.person_emergency_contact
WHERE id = $1 AND person_id = $2
RETURNING id
