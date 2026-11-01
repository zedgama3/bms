-- Store export (Service.Export): every entry whose subject is one person, oldest first, in
-- batches (keyset on (at, id), ascending; run it with limit = audit.MaxPageSize until fewer
-- rows than the limit come back). Served by audit_entry_subject_idx scanned backwards.
--
-- Args: $1 subject_person_id uuid (required)
--       $2 after_at timestamptz, $3 after_id uuid  (the last row of the previous batch;
--          both NULL for the first batch)
--       $4 limit integer
-- The cursor is spelled as in search_entries.sql (no row comparison).
-- Columns: the same, in the same order, as search_entries.sql.
SELECT id, at, instance, trace_id, spooled, action, outcome, actor_kind, actor_person_id,
       actor_superuser, actor_titles, subject_person_id, record_kind, record_id, fields, level,
       reason
FROM core.audit_entry
WHERE subject_person_id = $1::uuid
  AND at >= COALESCE($2::timestamptz, '-infinity'::timestamptz)
  AND (at > COALESCE($2::timestamptz, '-infinity'::timestamptz)
       OR id > COALESCE($3::uuid, '00000000-0000-0000-0000-000000000000'::uuid))
ORDER BY at, id
LIMIT $4::integer
