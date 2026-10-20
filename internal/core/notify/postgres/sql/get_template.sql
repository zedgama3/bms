-- Store.GetOverride: one override with its text, for rendering (read once per sender tick) and
-- for GetTemplate. Read-only; use QueryRow.
-- Args: $1 name text, $2 language text.
-- Columns, in order: name text, language text, subject text, body text, updated_by uuid (NULL
-- when unknown or deleted), updated_at timestamptz. pgx.ErrNoRows (NotFound, meaning "no
-- override; use the shipped default") when there is none.
SELECT name, language, subject, body, updated_by, updated_at
FROM core.notify_template
WHERE name = $1::text
  AND language = $2::text
