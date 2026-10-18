-- Store.DeleteByPerson (DeletePerson). Args: $1 person_id uuid. Returns the deleted row's id
-- (zero or one row; the count is the row count) so the caller can record the audit entry
-- identity.account.deleted with Record: account. Use Query, not Exec. Idempotent: no row,
-- no error.
DELETE FROM core.account
WHERE person_id = $1
RETURNING id
