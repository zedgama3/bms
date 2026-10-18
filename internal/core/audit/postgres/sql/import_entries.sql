-- Store batch insert (Importer, spool lines). Same $1 shape and normalization as
-- insert_entries.sql, but spooled = true (at is the original time; created_at is now()) and
-- ON CONFLICT (id) DO NOTHING, so a re-run after a crash drops what is already in.
-- Exec; RowsAffected() is the number inserted, so duplicates = len(batch) - RowsAffected().
-- Run in a Write transaction of at most audit.MaxBatch entries.
--
-- Args: $1 jsonb: an array of objects (see insert_entries.sql for the keys).
INSERT INTO core.audit_entry (
    id, at, instance, trace_id, spooled, action, outcome, actor_kind, actor_person_id,
    actor_superuser, actor_titles, subject_person_id, record_kind, record_id, fields, level,
    reason)
SELECT
    e.id, e.at, e.instance, NULLIF(e.trace_id, ''), true, e.action, e.outcome, e.actor_kind,
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
ON CONFLICT (id) DO NOTHING
