-- Store.ExportPerson: the rows addressed to one person, oldest first, in batches (keyset on
-- (created_at, id), ascending; run until fewer rows than the limit come back). Read-only; use
-- Query. Never selects address or body. The subject is NULL once the message is sent or
-- expired, so it appears only for a waiting row or a failed row not yet expired.
-- Args: $1 person_id uuid (required), $2 after_created_at timestamptz, $3 after_id uuid (the
-- last row of the previous batch; both NULL for the first batch), $4 limit integer.
-- Columns, in order: id uuid, template text, status text, language text, created_at
-- timestamptz, sent_at timestamptz (NULL unless sent), subject text (NULL once sent or expired).
-- Served by notify_message_person_id_idx. The cursor is spelled as in audit's
-- export_entries_by_subject.sql.
SELECT id, template, status, language, created_at, sent_at, subject
FROM core.notify_message
WHERE person_id = $1::uuid
  AND created_at >= COALESCE($2::timestamptz, '-infinity'::timestamptz)
  AND (created_at > COALESCE($2::timestamptz, '-infinity'::timestamptz)
       OR id > COALESCE($3::uuid, '00000000-0000-0000-0000-000000000000'::uuid))
ORDER BY created_at, id
LIMIT $4::integer
