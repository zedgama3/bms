-- Store.CountVerifiedBy: ExportPerson's "verified_for_others" (a count). Use QueryRow.
-- Args: $1 person_id uuid. Columns: n bigint. Served by record_verified_by_idx.
SELECT count(*) AS n
FROM volunteers.qualification_record
WHERE verified_by = $1::uuid
