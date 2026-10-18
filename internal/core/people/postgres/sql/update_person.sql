-- Store.Update: an admin's change to an existing person (Update). Use Query; zero rows:
-- NotFound. Same partial-update rules as upsert_person.sql (NULL: unchanged; '' clears;
-- $7 '' clears contact_email).
-- Args: $1 id uuid, $2 playa_name text, $3 pronouns text, $4 language text, $5 legal_name
-- text, $6 phone text, $7 contact_email text, $8 updated_at timestamptz.
-- Columns, in order: as get_person.sql.
UPDATE core.person SET
    playa_name    = COALESCE($2::text, playa_name),
    pronouns      = COALESCE($3::text, pronouns),
    language      = COALESCE($4::text, language),
    legal_name    = COALESCE($5::text, legal_name),
    phone         = COALESCE($6::text, phone),
    contact_email = CASE WHEN $7::text IS NULL THEN contact_email ELSE NULLIF($7::text, '') END,
    updated_at    = $8::timestamptz
WHERE id = $1
RETURNING id, playa_name, pronouns, language, legal_name, phone, contact_email, created_at, updated_at
