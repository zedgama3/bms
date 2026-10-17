-- Session-level advisory lock for one Migrate run, on a dedicated connection. Args: $1 key
-- bigint (derived in Go from "bms.migrate"). One bool row: true = acquired, false = another
-- run holds it (Conflict). Never waits.
SELECT pg_try_advisory_lock($1::bigint) AS acquired;
