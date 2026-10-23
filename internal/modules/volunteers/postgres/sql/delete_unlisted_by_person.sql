-- Store.DeleteUnlistedByPerson: DeletePerson. Exec; count = RowsAffected(). Idempotent.
-- Args: $1 person_id uuid. Served by unlisted_pkey.
DELETE FROM volunteers.unlisted
WHERE person_id = $1::uuid
