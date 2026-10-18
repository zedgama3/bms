-- Store.LockPerson: serialises writes to one person's contacts (the count limit and the
-- positions) and answers "does the person exist" (zero rows: NotFound). Inside a Write
-- transaction, first statement of AddContact, UpdateContact and RemoveContact. FOR NO KEY
-- UPDATE does not block other tables' foreign-key checks on the row.
-- Args: $1 person_id uuid. Columns, in order: id uuid.
SELECT id
FROM core.person
WHERE id = $1
FOR NO KEY UPDATE
