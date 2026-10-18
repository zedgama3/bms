-- Store.ClearGrantedBy: DeletePerson, step 2: grants the person made keep existing with no
-- granter. Use Exec; RowsAffected is not part of the count. Idempotent. Args: $1 person_id
-- uuid. Uses a sequential scan of core.role_grant (granted_by is not indexed: deleting a
-- person is rare and the table is small; add an index if a regional's table grows large).
UPDATE core.role_grant
SET granted_by = NULL
WHERE granted_by = $1
