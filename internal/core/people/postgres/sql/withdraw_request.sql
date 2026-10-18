-- Store.WithdrawRequest. Use Query. Zero rows: the request is not the person's or is not
-- open; the use case reads get_request.sql to tell NotFound from Conflict status. decided_by
-- stays NULL (the person withdrew their own).
-- Args: $1 id uuid, $2 person_id uuid, $3 decided_at timestamptz.
-- Columns: as insert_request.sql.
UPDATE core.data_request SET status = 'withdrawn', decided_at = $3::timestamptz
WHERE id = $1 AND person_id = $2 AND status = 'open'
RETURNING id, person_id, kind, status, note, created_at, decided_at, decided_by, decision_note
