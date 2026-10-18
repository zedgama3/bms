-- Store.DeleteByPerson: DeletePerson, step 1 (ended grants too). Use Query; the number of rows
-- is the count DeletePerson returns. Idempotent: no row, no error. Inside the Write
-- transaction, after lock_invariants.sql and the last-access-admin check
-- (count_access_admins.sql with $4 = the person). Args: $1 person_id uuid.
-- Columns: id uuid (so the caller can name the deleted grants if it wants to).
DELETE FROM core.role_grant
WHERE person_id = $1
RETURNING id
