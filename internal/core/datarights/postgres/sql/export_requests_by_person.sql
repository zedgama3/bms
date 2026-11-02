-- Store.ExportRequests: the "requests" array of the export document. Read-only; use Query
-- (zero rows: []). Oldest first. Bounded in practice (one open at a time).
-- Args: $1 person_id uuid. Columns: as insert_request.sql (the document uses kind, status,
-- note, created_at, decision_note).
SELECT id, person_id, kind, status, note, created_at, decided_at, decided_by, decision_note
FROM core.data_request
WHERE person_id = $1
ORDER BY created_at, id
