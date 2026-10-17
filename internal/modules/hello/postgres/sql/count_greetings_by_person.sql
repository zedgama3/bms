-- Store.CountByPerson. Args: $1 person_id uuid. One row, one column: n bigint (scan into int64).
SELECT count(*) AS n
FROM hello.greeting
WHERE person_id = $1
