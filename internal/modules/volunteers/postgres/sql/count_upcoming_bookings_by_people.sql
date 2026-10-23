-- Store.UpcomingCounts: the person-page section's "Upcoming shifts" count, for the viewer's
-- extent. Use Query. Read-only. A person with no upcoming booking yields no row (count 0).
-- Args: $1 person_ids uuid[] (at most vol.MaxIDs), $2 now timestamptz (shifts that end after
--       it), $3 all_departments boolean (site-wide rosters.read, or the person themself),
--       $4 departments uuid[] (the viewer's rosters.read extent).
-- Columns: person_id uuid, n bigint (scan into int64). No ORDER BY: the result is a map.
-- Served by signup_person_status_idx.
SELECT su.person_id, count(*) AS n
FROM volunteers.signup su
JOIN volunteers.shift s ON s.id = su.shift_id
WHERE su.person_id = ANY($1::uuid[])
  AND su.status = 'booked'
  AND s.canceled_at IS NULL
  AND s.ends > $2::timestamptz
  AND ($3::boolean OR s.department_id = ANY($4::uuid[]))
GROUP BY su.person_id
