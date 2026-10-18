-- Store.DeleteGrantsByPerson: DeletePerson, step 1 (ended grants too). Use Query; the rows
-- returned are the deleted grants, the count DeletePerson returns and the payloads of the
-- access.grant-revoked signals it publishes. Idempotent: no row, no error. Inside the Write
-- transaction, after lock_invariants.sql and the last-access-admin check
-- (list_access_admin_grants.sql with $4 = the person). Args: $1 person_id uuid.
-- Columns: as get_grant.sql.
DELETE FROM core.role_grant
WHERE person_id = $1
RETURNING id, person_id, role_id, department_id, ends_at, granted_by, created_at
