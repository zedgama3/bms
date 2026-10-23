-- Store.InsertRequirements: SetRequirements, second half. Exec; RowsAffected is the new count.
-- Go has already checked: at most vol.MaxRequirements (20), no duplicates, each qualification
-- active and org-wide or of the position's department (list_qualifications_by_ids.sql).
-- Args: $1 position_id uuid, $2 qualification_ids uuid[], $3 at timestamptz (created_at and
--       updated_at).
-- Constraint errors: position_requirement_qualification_id_fkey (23503: Invalid, code unknown);
--   position_requirement_pkey (23505: a duplicate in $2, Go rejects first).
INSERT INTO volunteers.position_requirement (position_id, qualification_id, created_at, updated_at)
SELECT $1::uuid, q, $3::timestamptz, $3::timestamptz
FROM unnest($2::uuid[]) AS q
