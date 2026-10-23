-- Store.CountShiftsCreatedBy: ExportPerson's "shifts_created" (a count). Use QueryRow.
-- Args: $1 person_id uuid. Columns: n bigint. Served by shift_created_by_idx.
SELECT count(*) AS n
FROM volunteers.shift
WHERE created_by = $1::uuid
