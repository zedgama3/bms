-- Store.FindOverlap: SignUp and Assign: does the person hold a booked, non-canceled shift
-- that overlaps [starts, ends)? Half-open, as model.TimeRange.Overlaps: a.starts < b.ends AND
-- b.starts < a.ends, so back-to-back shifts do not overlap. Use QueryRow; no row = no overlap.
-- Run after lock_person_bookings.sql inside the Write transaction.
-- Args: $1 person_id uuid, $2 starts timestamptz, $3 ends timestamptz, $4 exclude_shift_id
--       uuid (the shift being booked; a duplicate booking is the unique index's job).
-- Columns: shift_id uuid, label text, starts timestamptz, ends timestamptz (the earliest
--   overlapping shift; the error names its ID: Conflict overlap {shift}).
-- Served by signup_person_status_idx, then shift_pkey.
SELECT s.id AS shift_id, s.label, s.starts, s.ends
FROM volunteers.signup su
JOIN volunteers.shift s ON s.id = su.shift_id
WHERE su.person_id = $1::uuid
  AND su.status = 'booked'
  AND s.id <> $4::uuid
  AND s.canceled_at IS NULL
  AND s.starts < $3::timestamptz
  AND s.ends > $2::timestamptz
ORDER BY s.starts, s.id
LIMIT 1
