-- Store.CancelShift: CancelShift (reason shift_canceled), after get_shift_for_update.sql; then
-- cancel_signups_by_shifts.sql with the same reason. Use QueryRow; zero rows = already
-- canceled or gone (Conflict canceled).
-- Args: $1 id uuid, $2 reason text, $3 at timestamptz (canceled_at and updated_at).
-- Columns: the shift columns.
UPDATE volunteers.shift
SET canceled_at = $3::timestamptz, cancel_reason = $2::text, updated_at = $3::timestamptz
WHERE id = $1::uuid
  AND canceled_at IS NULL
RETURNING id, department_id, position_id, event_id, label, starts, ends, zone, slots, location, notes,
          canceled_at, cancel_reason, copied_from, created_by, created_at, updated_at
