-- Store.ListByPerson. Args: $1 person_id uuid. Columns, in order: id uuid, person_id uuid,
-- greeting text, created_at timestamptz. Oldest first, id breaks ties (stable order).
-- Not paginated: one person's rows, and the person export must be complete.
SELECT id, person_id, greeting, created_at
FROM hello.greeting
WHERE person_id = $1
ORDER BY created_at, id
