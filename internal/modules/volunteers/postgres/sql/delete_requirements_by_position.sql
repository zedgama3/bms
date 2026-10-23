-- Store.DeleteRequirements: SetRequirements, first half (replace the set), in the Write
-- transaction after get_position_for_update.sql. Exec; RowsAffected is the old count.
-- Args: $1 position_id uuid.
DELETE FROM volunteers.position_requirement
WHERE position_id = $1::uuid
