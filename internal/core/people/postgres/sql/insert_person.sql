-- Store.Insert: Create, and the identity.account-created handler. Use Query (not QueryRow):
-- zero rows means the ID already existed and nothing was written, which is the handler's
-- idempotent "row exists" (publish nothing) and, for Create, a clash on a freshly generated
-- ID that the use case turns into Internal.
-- Args: $1 id uuid, $2 playa_name text, $3 pronouns text, $4 language text, $5 legal_name
-- text, $6 phone text, $7 contact_email text ('' or NULL: none), $8 created_by uuid (NULL:
-- the handler, a command, a sign-up), $9 now timestamptz (created_at and updated_at).
-- The handler passes '' for the five text fields and NULLs.
-- Columns, in order: as get_person.sql.
INSERT INTO core.person (id, playa_name, pronouns, language, legal_name, phone, contact_email,
                         created_by, created_at, updated_at)
VALUES ($1::uuid, $2::text, $3::text, $4::text, $5::text, $6::text, NULLIF($7::text, ''),
        $8::uuid, $9::timestamptz, $9::timestamptz)
ON CONFLICT (id) DO NOTHING
RETURNING id, playa_name, pronouns, language, legal_name, phone, contact_email, created_at, updated_at
