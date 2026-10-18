-- Store.ExportPerson: the "person" object of the export document. Read-only; use Query
-- (zero rows: NotFound). Args: $1 person_id uuid.
-- Columns, in order: id uuid, playa_name text, pronouns text, language text, legal_name text,
-- phone text, contact_email text (NULL: none), created_at timestamptz, updated_at
-- timestamptz. (created_by is not exported: it names another person.)
SELECT id, playa_name, pronouns, language, legal_name, phone, contact_email, created_at, updated_at
FROM core.person
WHERE id = $1
