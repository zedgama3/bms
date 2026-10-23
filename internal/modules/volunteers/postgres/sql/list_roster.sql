-- Store.Roster / BookedSignups: a shift's booked sign-ups (the lead's roster, UpdateShift's
-- reminder re-enqueue, CancelShift's notifications). Use Query. Read-only: any copy. The use
-- case checked rosters.read@D. At most shift.slots (<= 200) rows, so unpaginated by
-- construction. Eligibility is computed in Go (list_valid_qualifications.sql); playa and legal
-- names come from people.
-- Args: $1 shift_id uuid. Columns: the signup columns (see insert_signup.sql); the roster's
-- waiver flag is the waived column (D51 c), not waived_by.
-- Order: created_at, id. Served by signup_shift_status_idx.
SELECT id, shift_id, person_id, status, booked_by, booked_as, waived, waived_by, canceled_at, canceled_by,
       cancel_reason, created_at, updated_at
FROM volunteers.signup
WHERE shift_id = $1::uuid
  AND status = 'booked'
ORDER BY created_at, id
