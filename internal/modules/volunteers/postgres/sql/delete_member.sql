-- Store.DeleteMember: RemoveMember and Leave. Use QueryRow; no row = NotFound (not a member,
-- F63, F65). Bookings stay (RemoveMember) or are canceled by cancel_leave_signups.sql (Leave).
-- Args: $1 department_id uuid, $2 person_id uuid. Columns: the member columns.
DELETE FROM volunteers.member
WHERE department_id = $1::uuid
  AND person_id = $2::uuid
RETURNING id, department_id, person_id, added_by, created_at, updated_at
