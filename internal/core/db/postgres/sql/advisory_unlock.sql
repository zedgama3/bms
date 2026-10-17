-- Release the session lock taken by advisory_try_lock.sql, on the same connection. Args: $1
-- key bigint. One bool row: true = released.
SELECT pg_advisory_unlock($1::bigint) AS released;
