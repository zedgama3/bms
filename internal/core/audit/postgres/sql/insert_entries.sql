-- Store insert (Recorder.Record, live path). One multi-row INSERT for the whole batch, so
-- it is atomic; inside a Write transaction it joins it. Exec; the count is RowsAffected().
-- A duplicate id is a bug and fails 23505 (audit_entry_pkey), unlike import_entries.sql.
--
-- Args: $1 jsonb: an array of objects, one per entry, flat keys:
--   id uuid, at (RFC 3339), instance, trace_id, action, outcome, actor_kind,
--   actor_person_id, actor_superuser (bool), actor_titles (array of strings),
--   subject_person_id, record_kind, record_id, fields (array of strings), level, reason.
-- Omit a key or send null for "none". "" is also "none" for trace_id, record_kind, level and
-- reason; the all-zero UUID is "none" for the three uuid keys. (An "" uuid fails 22P02.)
-- spooled is false and created_at is now(): the importer's statement sets spooled = true.
INSERT INTO core.audit_entry (
    id, at, instance, trace_id, action, outcome, actor_kind, actor_person_id, actor_superuser,
    actor_titles, subject_person_id, record_kind, record_id, fields, level, reason)
SELECT
    e.id, e.at, e.instance, NULLIF(e.trace_id, ''), e.action, e.outcome, e.actor_kind,
    NULLIF(e.actor_person_id, '00000000-0000-0000-0000-000000000000'::uuid),
    COALESCE(e.actor_superuser, false),
    ARRAY(SELECT t.v FROM jsonb_array_elements_text(e.actor_titles) WITH ORDINALITY AS t(v, n) ORDER BY t.n),
    NULLIF(e.subject_person_id, '00000000-0000-0000-0000-000000000000'::uuid),
    NULLIF(e.record_kind, ''),
    NULLIF(e.record_id, '00000000-0000-0000-0000-000000000000'::uuid),
    ARRAY(SELECT f.v FROM jsonb_array_elements_text(e.fields) WITH ORDINALITY AS f(v, n) ORDER BY f.n),
    NULLIF(e.level, ''),
    NULLIF(e.reason, '')
FROM jsonb_to_recordset($1::jsonb) AS e(
    id uuid, at timestamptz, instance text, trace_id text, action text, outcome text,
    actor_kind text, actor_person_id uuid, actor_superuser boolean, actor_titles jsonb,
    subject_person_id uuid, record_kind text, record_id uuid, fields jsonb, level text,
    reason text)
