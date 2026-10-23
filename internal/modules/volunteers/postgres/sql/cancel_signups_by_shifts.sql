-- Store.CancelSignupsByShifts: cancels the booked sign-ups of shifts just canceled
-- (CancelShift: one shift; the two signal handlers: many), with the details the notification
-- and the volunteers.signup-canceled signal need. Use Query. Idempotent on state. canceled_by
-- is NULL: the system acted, or the lead is recorded in the audit entry.
-- Args: $1 shift_ids uuid[] (the handlers pass the IDs returned by cancel_shifts_by_*; chunk
--       at vol.MaxIDs), $2 reason text ('shift_canceled', 'department_archived',
--       'event_closed'), $3 at timestamptz (canceled_at and updated_at).
-- Columns: id uuid (signup), shift_id uuid, person_id uuid, department_id uuid, label text,
--   starts timestamptz, zone text. Order: starts, id.
-- Served by signup_shift_status_idx.
WITH canceled AS (
    UPDATE volunteers.signup su
    SET status = 'canceled', canceled_at = $3::timestamptz, canceled_by = NULL,
        cancel_reason = $2::text, updated_at = $3::timestamptz
    FROM volunteers.shift sh
    WHERE su.shift_id = sh.id
      AND su.shift_id = ANY($1::uuid[])
      AND su.status = 'booked'
    RETURNING su.id, su.shift_id, su.person_id, sh.department_id, sh.label, sh.starts, sh.zone
)
SELECT id, shift_id, person_id, department_id, label, starts, zone
FROM canceled
ORDER BY starts, id
