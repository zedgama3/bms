-- Store.Schedule, upcoming mode (ScheduleFilter.Past false): MySchedule. The person's bookings
-- whose shift has not ended, soonest first, plus the sign-ups SOMEONE ELSE canceled (a lead,
-- or the system: reasons lead, shift_canceled, department_archived, event_closed) until the
-- shift ends, so a removal is seen even without e-mail (Q-VOL9, F153). The person's own
-- cancellations (reason self, including Leave) are not listed. Use Query. Read-only. Own
-- records: no permission filter. Department names come from Deps.Departments.NamesOf.
-- Args: $1 person_id uuid,
--       $2 now timestamptz (the shift must end after it),
--       $3 within_start timestamptz (NULL = no lower bound; shifts that end after it),
--       $4 within_end timestamptz (NULL = unbounded; shifts that start before it),
--       $5 after_starts timestamptz and $6 after_id uuid (keyset cursor: the shift start and
--          the SIGN-UP id of the last row of the previous page; both NULL on the first page),
--       $7 limit integer (pass Limit + 1; the extra row says there is a next page).
-- Columns: the signup columns (see insert_signup.sql, including status, cancel_reason and
--   canceled_by so the screen can strike a removed booking through), then the shift columns
--   (see insert_shift.sql, 17 of them), then position_name text.
-- Order: shift starts, sign-up id (ascending; the cursor is that pair of the last row).
-- Served by signup_person_status_idx (a person's sign-ups), then shift_pkey and position_pkey
-- by nested loop; the sort is over one person's open or removed bookings, which is small.
-- "starts > now - 48 hours" is implied by shift_times_chk (a shift is at most 48 hours long)
-- and lets the planner see that only recent and future shifts qualify.
SELECT su.id, su.shift_id, su.person_id, su.status, su.booked_by, su.booked_as, su.waived, su.waived_by,
       su.canceled_at, su.canceled_by, su.cancel_reason, su.created_at, su.updated_at,
       s.id, s.department_id, s.position_id, s.event_id, s.label, s.starts, s.ends, s.zone, s.slots,
       s.location, s.notes, s.canceled_at, s.cancel_reason, s.copied_from, s.created_by,
       s.created_at, s.updated_at,
       p.name AS position_name
FROM volunteers.signup su
JOIN volunteers.shift s ON s.id = su.shift_id
JOIN volunteers.position p ON p.id = s.position_id
WHERE su.person_id = $1::uuid
  AND (su.status = 'booked' OR (su.status = 'canceled' AND su.cancel_reason <> 'self'))
  AND s.ends > $2::timestamptz
  AND s.starts > $2::timestamptz - interval '48 hours'
  AND ($3::timestamptz IS NULL OR s.ends > $3::timestamptz)
  AND ($4::timestamptz IS NULL OR s.starts < $4::timestamptz)
  AND ($5::timestamptz IS NULL OR (s.starts, su.id) > ($5::timestamptz, $6::uuid))
ORDER BY s.starts, su.id
LIMIT $7::integer
