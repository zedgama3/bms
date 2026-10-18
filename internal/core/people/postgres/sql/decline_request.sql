-- Store.DeclineRequest. Use Query. Zero rows: unknown or not open; the use case reads
-- get_request.sql to tell NotFound from Conflict status.
-- Args: $1 id uuid, $2 decided_at timestamptz, $3 decided_by uuid (the actor), $4
-- decision_note text. Columns: as insert_request.sql.
UPDATE core.data_request SET
    status = 'declined', decided_at = $2::timestamptz, decided_by = $3::uuid,
    decision_note = $4::text
WHERE id = $1 AND status = 'open'
RETURNING id, person_id, kind, status, note, created_at, decided_at, decided_by, decision_note
