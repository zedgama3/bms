-- Store.RemoveContact, step 2: close the gap a delete left, so positions stay 1..n and
-- insert_contact.sql's "max + 1" stays right. Use Exec. One statement; the unique key on
-- (person_id, position) is deferrable and checked at statement end, so the shift does not
-- collide with itself. updated_at is not touched (the content did not change).
-- Args: $1 person_id uuid.
UPDATE core.person_emergency_contact c SET position = r.n
FROM (
    SELECT id, row_number() OVER (ORDER BY position) AS n
    FROM core.person_emergency_contact
    WHERE person_id = $1
) r
WHERE c.id = r.id AND c.position <> r.n
