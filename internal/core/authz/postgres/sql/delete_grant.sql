-- Store.DeleteGrant: Revoke. Use Query (zero rows: NotFound). Inside the Write transaction,
-- after lock_invariants.sql and the checks. Args: $1 id uuid.
-- Columns: as get_grant.sql (the deleted row, for the audit entry and the signal payload).
DELETE FROM core.role_grant
WHERE id = $1
RETURNING id, person_id, role_id, department_id, ends_at, granted_by, created_at
