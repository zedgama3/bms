-- Store.InsertRequest: RequestDeletion. Use Query (one row back). A second open request for
-- the person fails 23505 on data_request_open_idx (db.Wrap: Conflict, Constraint set):
-- Conflict duplicate. An unknown person fails 23503 on data_request_person_id_fkey.
-- Args: $1 id uuid, $2 person_id uuid, $3 note text, $4 created_at timestamptz.
-- kind is 'delete' and status 'open' (this statement writes both).
-- Columns, in order: id uuid, person_id uuid, kind text, status text, note text, created_at
-- timestamptz, decided_at timestamptz (NULL), decided_by uuid (NULL), decision_note text.
-- The same as every other request query.
INSERT INTO core.data_request (id, person_id, kind, status, note, created_at)
VALUES ($1::uuid, $2::uuid, 'delete', 'open', $3::text, $4::timestamptz)
RETURNING id, person_id, kind, status, note, created_at, decided_at, decided_by, decision_note
