-- Store.MyRequests: a person's own requests, newest first, keyset on (created_at, id)
-- descending. Read-only; use Query. Served by data_request_person_created_at_id_idx.
-- Args: $1 person_id uuid, $2 after_created_at timestamptz and $3 after_id uuid (the cursor;
-- both NULL on the first page), $4 limit integer (pass Limit + 1; the extra row is dropped
-- and row Limit's (created_at, id) is the next cursor).
-- Columns: as insert_request.sql.
-- The cursor is spelled "created_at <= c AND (created_at < c OR id < c_id)", as in
-- core/identity's list_accounts.sql.
SELECT id, person_id, kind, status, note, created_at, decided_at, decided_by, decision_note
FROM core.data_request
WHERE person_id = $1
  AND created_at <= COALESCE($2::timestamptz, 'infinity'::timestamptz)
  AND (created_at < COALESCE($2::timestamptz, 'infinity'::timestamptz)
       OR id < COALESCE($3::uuid, 'ffffffff-ffff-ffff-ffff-ffffffffffff'::uuid))
ORDER BY created_at DESC, id DESC
LIMIT $4::integer
