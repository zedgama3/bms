-- Store.CountPendingRecords: the admin screen's "pending certificates" badge. Same filter as
-- list_pending_records.sql. Use QueryRow. Read-only.
-- Args: $1 department_id uuid (NULL = all), $2 all_departments boolean, $3 departments uuid[],
--       $4 actor uuid.
-- Columns: n bigint (scan into int64).
SELECT count(*) AS n
FROM volunteers.qualification_record r
JOIN volunteers.qualification q ON q.id = r.qualification_id
WHERE r.verified_at IS NULL
  AND r.person_id <> $4::uuid
  AND (q.department_id IS NULL
       OR (($2::boolean OR q.department_id = ANY($3::uuid[]))
           AND ($1::uuid IS NULL OR q.department_id = $1::uuid)))
