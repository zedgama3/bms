-- Store.GetMember: "is this person a member of this department" (members-only eligibility,
-- Leave, RemoveMember). Use QueryRow; no row = not a member. Read-only: any copy.
-- Args: $1 department_id uuid, $2 person_id uuid. Columns: the member columns (see
-- insert_member.sql). Served by member_department_person_key.
SELECT id, department_id, person_id, added_by, created_at, updated_at
FROM volunteers.member
WHERE department_id = $1::uuid
  AND person_id = $2::uuid
