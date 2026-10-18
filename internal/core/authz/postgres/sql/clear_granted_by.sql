-- Store.ClearGrantedBy: DeletePerson, step 2: grants the person made keep existing with no
-- granter. Use Exec; RowsAffected is not part of the count. Idempotent. Args: $1 person_id
-- uuid. Served by the partial index role_grant_granted_by_idx (migration 0002), which also
-- serves the foreign key's ON DELETE SET NULL action.
UPDATE core.role_grant
SET granted_by = NULL
WHERE granted_by = $1
