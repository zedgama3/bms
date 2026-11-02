-- Store.Request: one request by ID, for the status check after a zero-row decision (NotFound
-- versus Conflict status) and for the response. Read-only; use Query (zero rows: NotFound).
-- Args: $1 id uuid. Columns: as insert_request.sql.
SELECT id, person_id, kind, status, note, created_at, decided_at, decided_by, decision_note
FROM core.data_request
WHERE id = $1
