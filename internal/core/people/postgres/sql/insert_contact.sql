-- Store.AddContact. Use Query (not QueryRow). Call lock_person.sql first in the same Write
-- transaction (it serialises the count and the position, and answers NotFound for an unknown
-- person). Appends at the next position (max + 1; positions are kept contiguous).
-- ZERO ROWS MEANS THE LIMIT WAS REACHED (the person already has $7 contacts): the use case
-- answers Invalid (contacts, too_many {max}). A concurrent add that slipped past the lock
-- fails 23505 on person_emergency_contact_person_position_key: Conflict.
-- Args: $1 id uuid, $2 person_id uuid, $3 name text, $4 phone text, $5 relationship text,
-- $6 now timestamptz (created_at and updated_at), $7 max integer (MaxEmergencyContacts).
-- Columns, in order: as list_contacts_by_person.sql.
INSERT INTO core.person_emergency_contact
       (id, person_id, name, phone, relationship, position, created_at, updated_at)
SELECT $1::uuid, $2::uuid, $3::text, $4::text, $5::text, COALESCE(MAX(position), 0) + 1,
       $6::timestamptz, $6::timestamptz
FROM core.person_emergency_contact
WHERE person_id = $2::uuid
HAVING COUNT(*) < $7::integer
RETURNING id, person_id, name, phone, relationship, position, created_at, updated_at
