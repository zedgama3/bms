-- Store.SetListedTrue: SetListed(true). Exec; RowsAffected 1 = changed (audit Reason listed),
-- 0 = was already listed (no audit, F142).
-- Args: $1 person_id uuid.
DELETE FROM volunteers.unlisted
WHERE person_id = $1::uuid
