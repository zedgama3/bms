-- Store.DeleteMembersByPerson: DeletePerson. Exec; count = RowsAffected(). Idempotent.
-- Args: $1 person_id uuid. Served by member_person_idx.
DELETE FROM volunteers.member
WHERE person_id = $1::uuid
