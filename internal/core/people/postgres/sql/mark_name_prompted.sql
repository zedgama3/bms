-- Store.MarkNamePrompted: record that the person was asked for a playa name in the session
-- issued at $2. One conditional statement decides under the row lock: the row is updated
-- only while the person has no name and was not yet prompted in this session (column NULL
-- or before $2), so two reads of one session get one row between them. The column is set to
-- GREATEST($3, $2), never earlier than the session's issue time whatever this copy's clock
-- says. updated_at is untouched: the mark is not a profile change. Not audited (internal).
-- Write only; use Query. Zero rows: not due (named, or already prompted this session), or
-- no such row.
-- Args: $1 person_id uuid, $2 issued_at timestamptz, $3 now timestamptz.
-- Columns: id uuid.
UPDATE core.person
SET name_prompted_at = GREATEST($3::timestamptz, $2::timestamptz)
WHERE id = $1
  AND playa_name = ''
  AND (name_prompted_at IS NULL OR name_prompted_at < $2::timestamptz)
RETURNING id
