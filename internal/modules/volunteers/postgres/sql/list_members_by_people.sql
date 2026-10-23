-- Store.MembershipsOfPeople: the person-page section (Departments), for the viewer's extent.
-- Use Query. Read-only.
-- Args: $1 person_ids uuid[] (at most vol.MaxIDs), $2 all_departments boolean (site-wide
--       rosters.read), $3 departments uuid[] (the viewer's rosters.read extent).
-- Columns: the member columns. Order: person_id, created_at, id. Served by member_person_idx.
SELECT id, department_id, person_id, added_by, created_at, updated_at
FROM volunteers.member
WHERE person_id = ANY($1::uuid[])
  AND ($2::boolean OR department_id = ANY($3::uuid[]))
ORDER BY person_id, created_at, id
