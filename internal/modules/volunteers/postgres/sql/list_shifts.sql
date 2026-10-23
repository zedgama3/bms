-- Store.ListShifts: shifts the caller may see, with Booked and Mine; Eligibility is computed
-- in Go from the requirements (list_requirements_by_positions.sql), the caller's records
-- (list_records_by_person.sql) and bookings (list_booked_windows.sql). Use Query. Read-only.
-- The visibility rule is in the query, not applied after fetching:
--   * a shift in the caller's privileged extent (shifts.manage or rosters.read: $7, $8) is
--     visible whatever its state (canceled, past) and its position's access;
--   * any other shift is visible only when not canceled and its position is open-access or of
--     a department the caller is a member of ($9).
-- $6 (include_canceled) narrows further: canceled shifts appear only when asked for (the use
-- case answers Permission when it is asked without an extent). Departments archived and events
-- closed have had their future shifts canceled by the signal handlers, so they drop out here.
-- Args: $1 department_id uuid (NULL = any), $2 event_id uuid (NULL = any, standing work
--       included), $3 position_id uuid (NULL = any),
--       $4 window_start timestamptz (shifts that end after it; the use case passes now when
--          Within is zero), $5 window_end timestamptz (NULL = unbounded; shifts that start
--          before it),
--       $6 include_canceled boolean,
--       $7 privileged_all boolean (site-wide shifts.manage or rosters.read),
--       $8 privileged_departments uuid[] ('{}' for none),
--       $9 member_departments uuid[] (the caller's memberships; '{}' for none),
--       $10 actor uuid (for "mine"),
--       $11 after_starts timestamptz and $12 after_id uuid (keyset cursor; both NULL on the
--          first page), $13 limit integer (ShiftQuery.Limit, 1..vol.MaxList; pass Limit + 1 to
--          know whether there is a next page).
-- Columns: the shift columns (see insert_shift.sql), then position_access text, hide_shift_mates
--   boolean, booked bigint, mine uuid NULL, position_name text (as get_shift_summary.sql; the
--   last column is ShiftRow.PositionName, V1.4 b).
-- Order: starts, id (stable; the cursor is (starts, id) of the last row).
-- Served by shift_department_starts_idx (department filter), shift_event_starts_idx (event
-- filter) or shift_starts_idx (no filter, stops after the first LIMIT rows in order). The
-- bound "starts > window_start - 48 hours" is implied by shift_times_chk (a shift is at most
-- 48 hours long, so one that ends after window_start started after window_start - 48h); it
-- lets those indexes be range-scanned from the window instead of filtered. booked comes from
-- signup_booked_key (index-only), mine from signup_booked_key.
-- Shape for robustness: the position columns are scalar subqueries and the open-access test
-- is an uncorrelated IN, not a join with the OR-ed visibility test, so the plan stays cheap
-- even if PostgreSQL caches a GENERIC plan for the statement (the optional-filter pattern
-- "$n IS NULL OR ..." makes the generic row estimate 1; a join here then became a 7-million-row
-- nested loop, 437 ms; this shape is 16 ms generic and 2.5 ms custom on 60,000 shifts).
SELECT s.id, s.department_id, s.position_id, s.event_id, s.label, s.starts, s.ends, s.zone, s.slots,
       s.location, s.notes, s.canceled_at, s.cancel_reason, s.copied_from, s.created_by,
       s.created_at, s.updated_at,
       (SELECT p.access FROM volunteers.position p WHERE p.id = s.position_id) AS position_access,
       (SELECT p.hide_shift_mates FROM volunteers.position p WHERE p.id = s.position_id) AS hide_shift_mates,
       (SELECT count(*) FROM volunteers.signup b
         WHERE b.shift_id = s.id AND b.status = 'booked') AS booked,
       (SELECT m.id FROM volunteers.signup m
         WHERE m.shift_id = s.id AND m.person_id = $10::uuid AND m.status = 'booked') AS mine,
       (SELECT p.name FROM volunteers.position p WHERE p.id = s.position_id) AS position_name
FROM volunteers.shift s
WHERE s.ends > $4::timestamptz
  AND s.starts > $4::timestamptz - interval '48 hours'
  AND ($5::timestamptz IS NULL OR s.starts < $5::timestamptz)
  AND ($1::uuid IS NULL OR s.department_id = $1::uuid)
  AND ($2::uuid IS NULL OR s.event_id = $2::uuid)
  AND ($3::uuid IS NULL OR s.position_id = $3::uuid)
  AND ($6::boolean OR s.canceled_at IS NULL)
  AND ($7::boolean
       OR s.department_id = ANY($8::uuid[])
       OR (s.canceled_at IS NULL
           AND (s.department_id = ANY($9::uuid[])
                OR s.position_id IN (SELECT o.id FROM volunteers.position o WHERE o.access = 'open'))))
  AND ($11::timestamptz IS NULL OR (s.starts, s.id) > ($11::timestamptz, $12::uuid))
ORDER BY s.starts, s.id
LIMIT $13::integer
