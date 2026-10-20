-- Store.ResetOverride: remove an override (reset to the shipped default). Use Query
-- (RETURNING) inside the Write transaction that also records notify.template.reset.
-- Args: $1 name text, $2 language text.
-- Columns, in order: name text. ZERO rows means no override existed: nil, idempotent (F44),
-- and the caller records no audit entry.
-- Fails 25006 (db.ErrReadOnly) on a read-only copy.
DELETE FROM core.notify_template
WHERE name = $1::text
  AND language = $2::text
RETURNING name
