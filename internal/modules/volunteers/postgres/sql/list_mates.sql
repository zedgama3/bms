-- Store.Mates: GetShift's shift-mates source: the OTHER booked people on a shift, and whether
-- each is listed (has no row in volunteers.unlisted). The use case sends the listed IDs to
-- Deps.People.NamesOf and counts the rest (and anyone with no playa name) in Unlisted. Use
-- Query. Read-only. Only called when the shift_mates policy shows the list to this viewer
-- and the position does not hide it. The IDs never leave the service (json:"-").
-- Args: $1 shift_id uuid, $2 actor uuid (left out).
-- Columns: person_id uuid, listed boolean. At most shift.slots - 1 rows.
-- Order: person_id (NOT booking order, which would leak who booked first). Served by
-- signup_shift_status_idx and unlisted_pkey.
SELECT su.person_id, (u.person_id IS NULL) AS listed
FROM volunteers.signup su
LEFT JOIN volunteers.unlisted u ON u.person_id = su.person_id
WHERE su.shift_id = $1::uuid
  AND su.status = 'booked'
  AND su.person_id <> $2::uuid
ORDER BY su.person_id
