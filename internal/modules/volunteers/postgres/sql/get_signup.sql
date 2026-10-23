-- Store.GetSignup: one sign-up with its shift's essentials, for CancelSignup's checks (own or
-- shifts.manage@D, started, cancel_cutoff) and the reminder job (is it still booked, has the
-- shift moved, what to say). Use QueryRow; no row = NotFound. Read-only: any copy.
-- Args: $1 id uuid.
-- Columns: the signup columns (see insert_signup.sql), then department_id uuid, position_id
--   uuid, position_name text, label text, starts timestamptz, ends timestamptz, zone text,
--   location text, shift_canceled_at timestamptz NULL. (The department name comes from
--   Deps.Departments.NamesOf; this module never reads core.)
-- Served by signup_pkey, shift_pkey and position_pkey.
SELECT su.id, su.shift_id, su.person_id, su.status, su.booked_by, su.booked_as, su.waived, su.waived_by, su.canceled_at,
       su.canceled_by, su.cancel_reason, su.created_at, su.updated_at,
       s.department_id, s.position_id, p.name AS position_name, s.label, s.starts, s.ends, s.zone,
       s.location, s.canceled_at AS shift_canceled_at
FROM volunteers.signup su
JOIN volunteers.shift s ON s.id = su.shift_id
JOIN volunteers.position p ON p.id = s.position_id
WHERE su.id = $1::uuid
