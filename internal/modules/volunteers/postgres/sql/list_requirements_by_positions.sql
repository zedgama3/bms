-- Store.RequirementsOf: the requirement sets of several positions at once (ListPositions,
-- ListShifts' eligibility, the roster). Use Query. Read-only.
-- Args: $1 position_ids uuid[] (at most vol.MaxIDs = 200; duplicates harmless).
-- Columns: position_id uuid, qualification_id uuid. Order: position_id, qualification_id (so a
--   position's slice is sorted, as Position.Requirements promises).
-- Served by position_requirement_pkey.
SELECT position_id, qualification_id
FROM volunteers.position_requirement
WHERE position_id = ANY($1::uuid[])
ORDER BY position_id, qualification_id
