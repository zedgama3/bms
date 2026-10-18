-- Store.PlayaNameOf: identity.NameSource. Read-only; use Query or QueryRow (zero rows:
-- NotFound, which identity shows as ""). Args: $1 person_id uuid.
-- Columns, in order: playa_name text ('' until the person fills it in).
SELECT playa_name
FROM core.person
WHERE id = $1
