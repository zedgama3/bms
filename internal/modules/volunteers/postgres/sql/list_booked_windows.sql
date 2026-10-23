-- Store.BookedWindows: ListShifts' overlap check for the actor: the time windows of their
-- booked, non-canceled shifts that end after $2 (and start before $3). The use case compares
-- each listed shift with these in Go (Times().Overlaps) to set Eligibility.Reason "overlap".
-- Use Query. Read-only. Own records.
-- Args: $1 person_id uuid, $2 window_start timestamptz, $3 window_end timestamptz (NULL =
--       unbounded), $4 limit integer (vol.MaxList).
-- Columns: shift_id uuid, starts timestamptz, ends timestamptz. Order: starts, shift_id.
-- Served by signup_person_status_idx.
SELECT s.id AS shift_id, s.starts, s.ends
FROM volunteers.signup su
JOIN volunteers.shift s ON s.id = su.shift_id
WHERE su.person_id = $1::uuid
  AND su.status = 'booked'
  AND s.canceled_at IS NULL
  AND s.ends > $2::timestamptz
  AND ($3::timestamptz IS NULL OR s.starts < $3::timestamptz)
ORDER BY s.starts, s.id
LIMIT $4::integer
