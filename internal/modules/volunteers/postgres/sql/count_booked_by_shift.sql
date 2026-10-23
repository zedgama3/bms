-- Store.CountBooked: slots check inside the booking transaction, after get_shift_for_update.sql
-- holds the shift row (so the count cannot change under it), and UpdateShift's "slots below
-- booked" (F79). Use QueryRow.
-- Args: $1 shift_id uuid. Columns: n bigint (scan into int64).
-- Served by signup_shift_status_idx.
SELECT count(*) AS n
FROM volunteers.signup
WHERE shift_id = $1::uuid
  AND status = 'booked'
