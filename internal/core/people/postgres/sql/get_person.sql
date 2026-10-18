-- Store.Get: one profile by ID. Read-only; use Query or QueryRow (zero rows: NotFound).
-- Args: $1 person_id uuid.
-- Columns, in order: id uuid, playa_name text, pronouns text, language text, legal_name text,
-- phone text, contact_email text (NULL: none), created_at timestamptz, updated_at timestamptz.
-- All columns are returned; the use case zeroes the personal ones the viewer may not see
-- (the SQL does not know the viewer). has_account is not a column (identity's EmailOf).
SELECT id, playa_name, pronouns, language, legal_name, phone, contact_email, created_at, updated_at
FROM core.person
WHERE id = $1
