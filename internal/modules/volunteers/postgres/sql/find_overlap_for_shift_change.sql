-- Store.FindOverlapForShiftChange: UpdateShift with a time change: would any person booked on
-- this shift now overlap another of their booked shifts with the new times? (F80.) Use
-- QueryRow; no row = no conflict. Run after get_shift_for_update.sql in the Write transaction.
-- Args: $1 shift_id uuid, $2 new_starts timestamptz, $3 new_ends timestamptz.
-- Columns: person_id uuid (not shown to the caller), shift_id uuid (the other shift: the
--   error's {shift}).
-- Served by signup_shift_status_idx, signup_person_status_idx and shift_pkey.
SELECT o.person_id, os.id AS shift_id
FROM volunteers.signup su
JOIN volunteers.signup o ON o.person_id = su.person_id AND o.status = 'booked' AND o.shift_id <> su.shift_id
JOIN volunteers.shift os ON os.id = o.shift_id
WHERE su.shift_id = $1::uuid
  AND su.status = 'booked'
  AND os.canceled_at IS NULL
  AND os.starts < $3::timestamptz
  AND os.ends > $2::timestamptz
ORDER BY os.starts, os.id
LIMIT 1
