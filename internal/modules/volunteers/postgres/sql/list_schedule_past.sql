-- Store.Schedule, past mode (ScheduleFilter.Past true): MySchedule. The person's BOOKED
-- sign-ups whose shift has ended, NEWEST first, so the latest work is on the first page
-- (F149b). Canceled sign-ups are not listed in this mode. Use Query. Read-only. Own records:
-- no permission filter.
-- Args: $1 person_id uuid,
--       $2 now timestamptz (the shift must have ended: ends <= now),
--       $3 within_start timestamptz (NULL = no lower bound; shifts that end after it),
--       $4 within_end timestamptz (NULL = unbounded; shifts that start before it),
--       $5 after_starts timestamptz and $6 after_id uuid (keyset cursor: the shift start and
--          the SIGN-UP id of the last row of the previous page; both NULL on the first page;
--          the next page holds rows BEFORE it in this order),
--       $7 limit integer (pass Limit + 1).
-- Columns: as list_schedule.sql.
-- Order: shift starts DESC, sign-up id DESC.
-- Served by signup_person_status_idx (person_id, status = 'booked'), then shift_pkey and
-- position_pkey by nested loop; the sort is over one person's booked sign-ups.
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
  AND su.status = 'booked'
  AND s.ends <= $2::timestamptz
  AND ($3::timestamptz IS NULL OR s.ends > $3::timestamptz)
  AND ($4::timestamptz IS NULL OR s.starts < $4::timestamptz)
  AND ($5::timestamptz IS NULL OR (s.starts, su.id) < ($5::timestamptz, $6::uuid))
ORDER BY s.starts DESC, su.id DESC
LIMIT $7::integer
