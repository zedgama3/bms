-- Store.ListRequests: the requests screen, oldest first, keyset on (created_at, id)
-- ascending. Read-only; use Query. Served by data_request_status_created_at_id_idx (or
-- data_request_person_created_at_id_idx when $2 is given).
-- Args: $1 status text ('open', 'withdrawn' or 'declined'; Go maps the zero RequestStatus to
-- 'open'), $2 person_id uuid (NULL: any), $3 after_created_at timestamptz and $4 after_id
-- uuid (the cursor; both NULL on the first page), $5 limit integer (pass Limit + 1).
-- Columns: as insert_request.sql.
SELECT id, person_id, kind, status, note, created_at, decided_at, decided_by, decision_note
FROM core.data_request
WHERE status = $1::text
  AND ($2::uuid IS NULL OR person_id = $2::uuid)
  AND created_at >= COALESCE($3::timestamptz, '-infinity'::timestamptz)
  AND (created_at > COALESCE($3::timestamptz, '-infinity'::timestamptz)
       OR id > COALESCE($4::uuid, '00000000-0000-0000-0000-000000000000'::uuid))
ORDER BY created_at, id
LIMIT $5::integer
