-- Store.GetSignupForUpdate: first statement of CancelSignup inside the Write transaction, so
-- two cancellations of one booking cannot both succeed (the second gets Conflict status).
-- Locks the sign-up row only. Args and columns: as get_signup.sql.
SELECT su.id, su.shift_id, su.person_id, su.status, su.booked_by, su.booked_as, su.waived, su.waived_by, su.canceled_at,
       su.canceled_by, su.cancel_reason, su.created_at, su.updated_at,
       s.department_id, s.position_id, p.name AS position_name, s.label, s.starts, s.ends, s.zone,
       s.location, s.canceled_at AS shift_canceled_at
FROM volunteers.signup su
JOIN volunteers.shift s ON s.id = su.shift_id
JOIN volunteers.position p ON p.id = s.position_id
WHERE su.id = $1::uuid
FOR UPDATE OF su
