-- Store.MembershipsOf: MyDepartments, the members-only visibility of ListShifts, the export's
-- memberships. Use Query. Read-only. Own records: no filter.
-- Args: $1 person_id uuid. Columns: the member columns. Order: created_at, id.
-- Served by member_person_idx.
SELECT id, department_id, person_id, added_by, created_at, updated_at
FROM volunteers.member
WHERE person_id = $1::uuid
ORDER BY created_at, id
