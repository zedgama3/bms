-- Store.DeleteByPerson. Args: $1 person_id uuid. Exec; the count is CommandTag.RowsAffected().
DELETE FROM hello.greeting
WHERE person_id = $1
