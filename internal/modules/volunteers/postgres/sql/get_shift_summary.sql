-- Store.GetShiftSummary: GetShift. One shift with the figures ShiftSummary carries. Use
-- QueryRow; no row = NotFound. Read-only: any copy. The use case applies the visibility rule
-- (position access, membership, extents) in Go from position_access and the caller's
-- memberships, answering NotFound for a shift the caller may not see.
-- Args: $1 id uuid, $2 actor uuid (for "mine").
-- Columns: the shift columns (see insert_shift.sql), then position_access text ('open' or
--   'members'), hide_shift_mates boolean, booked bigint (booked sign-ups; scan into int64),
--   mine uuid NULL (the actor's booked sign-up on this shift), position_name text (the last
--   column; ShiftRow.PositionName, ShiftSummary.PositionName).
-- Served by shift_pkey, position_pkey, signup_shift_status_idx and signup_booked_key.
SELECT s.id, s.department_id, s.position_id, s.event_id, s.label, s.starts, s.ends, s.zone, s.slots,
       s.location, s.notes, s.canceled_at, s.cancel_reason, s.copied_from, s.created_by,
       s.created_at, s.updated_at,
       p.access AS position_access,
       p.hide_shift_mates,
       (SELECT count(*) FROM volunteers.signup b
         WHERE b.shift_id = s.id AND b.status = 'booked') AS booked,
       (SELECT m.id FROM volunteers.signup m
         WHERE m.shift_id = s.id AND m.person_id = $2::uuid AND m.status = 'booked') AS mine,
       p.name AS position_name
FROM volunteers.shift s
JOIN volunteers.position p ON p.id = s.position_id
WHERE s.id = $1::uuid
