-- Store.GetForUpdate: get_person.sql with a row lock, so Update and UpdateMe can compute the
-- changed fields (for the audit entry) against exactly the row they then write. Inside a
-- Write transaction. FOR NO KEY UPDATE does not block other tables' foreign-key checks on
-- the row. Args: $1 person_id uuid. Columns: as get_person.sql. Zero rows: NotFound.
SELECT id, playa_name, pronouns, language, legal_name, phone, contact_email, created_at, updated_at
FROM core.person
WHERE id = $1
FOR NO KEY UPDATE
