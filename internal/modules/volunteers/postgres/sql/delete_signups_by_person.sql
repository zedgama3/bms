-- Store.DeleteSignupsByPerson: DeletePerson. Exec; count = RowsAffected(). Idempotent.
-- Args: $1 person_id uuid. Served by signup_person_status_idx.
DELETE FROM volunteers.signup
WHERE person_id = $1::uuid
