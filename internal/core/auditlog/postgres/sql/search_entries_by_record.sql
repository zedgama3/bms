-- Store search (Service.Search): a record is set (served by audit_entry_record_idx).
-- Newest first, keyset on (at, id). Every filter is optional: pass NULL for "any". Pass
-- limit = Query.Limit + 1; when limit rows come back, Page.Next is the at and id of the
-- last row *returned* (row Query.Limit), and the extra row is dropped.
--
-- Four files share one signature and differ only in the equality that opens the index
-- (search_entries.sql: none; _by_actor, _by_subject, _by_record). Service.Search picks the first
-- that applies: record, then subject, then actor, else none, and passes every filter to
-- it, so combined filters work. The key must be non-NULL in its file. They are separate so
-- the plan does not depend on the driver's plan cache: an "$n IS NULL OR col = $n" test
-- cannot use an index in a generic plan (measured: 150k rows read for a 51-row page).
--
-- Args:
--   $1 actor_person_id uuid        $2 subject_person_id uuid
--   $3 record_kind text            $4 record_id uuid       (both NULL or both set, as Ref)
--   $5 area text                   $6 action text         (exact)
--   $7 within_start timestamptz    $8 within_end timestamptz   ([start, end); NULL = open)
--   $9 superuser_only boolean      (NOT NULL; false = any)
--   $10 after_at timestamptz       $11 after_id uuid      (Cursor; both NULL on the first page)
--   $12 limit integer
-- Columns, in order: id uuid, at timestamptz, instance text, trace_id text (NULL = none),
-- spooled boolean, action text, outcome text, actor_kind text, actor_person_id uuid,
-- actor_superuser boolean, actor_titles text[], subject_person_id uuid, record_kind text,
-- record_id uuid, fields text[], level text, reason text.
--
-- The cursor is spelled "at <= c_at AND (at < c_at OR id < c_id)" and not as a row
-- comparison: a row comparison against 'infinity' hides the date range from the index and
-- the scan walks the whole table (measured: 3400 pages for a 51-row page).
SELECT id, at, instance, trace_id, spooled, action, outcome, actor_kind, actor_person_id,
       actor_superuser, actor_titles, subject_person_id, record_kind, record_id, fields, level,
       reason
FROM core.audit_entry
WHERE record_kind = $3::text AND record_id = $4::uuid
  AND ($1::uuid IS NULL OR actor_person_id = $1::uuid)
  AND ($2::uuid IS NULL OR subject_person_id = $2::uuid)
  AND ($3::text IS NULL OR (record_kind = $3::text AND record_id = $4::uuid))
  AND ($5::text IS NULL OR starts_with(action, $5::text || '.'))
  AND ($6::text IS NULL OR action = $6::text)
  AND at >= COALESCE($7::timestamptz, '-infinity'::timestamptz)
  AND at <  COALESCE($8::timestamptz, 'infinity'::timestamptz)
  AND (NOT $9::boolean OR actor_superuser)
  AND at <= COALESCE($10::timestamptz, 'infinity'::timestamptz)
  AND (at < COALESCE($10::timestamptz, 'infinity'::timestamptz)
       OR id < COALESCE($11::uuid, 'ffffffff-ffff-ffff-ffff-ffffffffffff'::uuid))
ORDER BY at DESC, id DESC
LIMIT $12::integer
