-- Store.ListMembers: ListMembers (MemberQuery). Use Query. Read-only. The use case checked
-- rosters.read@D. Keyset paging on (created_at, id).
-- Args: $1 department_id uuid,
--       $2 after_created timestamptz and $3 after_id uuid (keyset cursor: created_at and id of
--          the last row of the previous page; both NULL on the first page),
--       $4 limit integer (pass Limit + 1; the extra row says there is a next page).
-- Columns: the member columns (see insert_member.sql). Order: created_at, id (ascending; the
-- cursor is that pair of the last row).
-- Served by member_department_person_key (leading column department_id), then a sort of one
-- department's members (a department has tens to a few hundred).
SELECT id, department_id, person_id, added_by, created_at, updated_at
FROM volunteers.member
WHERE department_id = $1::uuid
  AND ($2::timestamptz IS NULL OR (created_at, id) > ($2::timestamptz, $3::uuid))
ORDER BY created_at, id
LIMIT $4::integer
