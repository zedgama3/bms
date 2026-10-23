-- Store.Upsert: UpdateMe (people.md decision 2: the first UpdateMe repairs a missing row).
-- Use Query (one row back). Partial update: a NULL argument leaves the column unchanged on an
-- existing row and becomes '' (or no contact_email) on a new one; a non-NULL argument sets it
-- ('' clears). $7 '' clears contact_email (stored as NULL).
-- Args: $1 id uuid, $2 playa_name text, $3 pronouns text, $4 language text, $5 legal_name
-- text, $6 phone text, $7 contact_email text, $8 now timestamptz (created_at on insert;
-- updated_at always), $9 playa_name_key text (people.NameKey of $2; NULL exactly when $2 is
-- NULL, so the key never drifts from the name; a new row with a NULL $2 gets ''). A key
-- another person holds fails 23505 on person_playa_name_key_idx (the store re-kinds it to
-- Conflict playa_name taken, on the insert and the update arm alike). The key is a name:
-- never logged.
-- Columns, in order: as get_person.sql, then inserted boolean (true: this call created the
-- row, so the use case audits people.person.created and publishes people.person-created;
-- false: it changed an existing row, people.person.changed).
INSERT INTO core.person (id, playa_name, playa_name_key, pronouns, language, legal_name, phone, contact_email,
                         created_at, updated_at)
VALUES ($1::uuid, COALESCE($2::text, ''), COALESCE($9::text, ''), COALESCE($3::text, ''), COALESCE($4::text, ''),
        COALESCE($5::text, ''), COALESCE($6::text, ''), NULLIF($7::text, ''),
        $8::timestamptz, $8::timestamptz)
ON CONFLICT (id) DO UPDATE SET
    playa_name    = COALESCE($2::text, core.person.playa_name),
    playa_name_key = COALESCE($9::text, core.person.playa_name_key),
    pronouns      = COALESCE($3::text, core.person.pronouns),
    language      = COALESCE($4::text, core.person.language),
    legal_name    = COALESCE($5::text, core.person.legal_name),
    phone         = COALESCE($6::text, core.person.phone),
    contact_email = CASE WHEN $7::text IS NULL THEN core.person.contact_email
                         ELSE NULLIF($7::text, '') END,
    updated_at    = $8::timestamptz
RETURNING id, playa_name, pronouns, language, legal_name, phone, contact_email, created_at, updated_at,
          (xmax = 0) AS inserted
