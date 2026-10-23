-- Store.ValidQualifications: which of these people hold which of these qualifications on a
-- date, verified and not expired (Record.ValidOn). Used for one shift at a time (the roster's
-- Eligible flag, ListMembers' eligible positions, SignUp/Assign's check): $2 is the shift's
-- start date in the shift's zone. For ListShifts the use case loads the actor's records once
-- (list_records_by_person.sql) and evaluates each shift in Go, since the date differs per shift.
-- Use Query. Read-only.
-- Args: $1 person_ids uuid[] (at most vol.MaxIDs = 200), $2 on_date date,
--       $3 qualification_ids uuid[] (the requirements to look for).
-- Columns: person_id uuid, qualification_id uuid (one row per held pair; no ORDER BY, the
--   result is a set).
-- Served by record_person_idx.
SELECT DISTINCT r.person_id, r.qualification_id
FROM volunteers.qualification_record r
WHERE r.person_id = ANY($1::uuid[])
  AND r.qualification_id = ANY($3::uuid[])
  AND r.verified_at IS NOT NULL
  AND (r.expires_on IS NULL OR r.expires_on >= $2::date)
