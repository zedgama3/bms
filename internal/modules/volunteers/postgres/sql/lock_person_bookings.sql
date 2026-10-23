-- Store.LockPersonBookings: the FIRST lock of SignUp and Assign (decision 7: serialise per
-- person, then per shift), so one person cannot be booked onto two overlapping shifts by two
-- requests at once. A transaction-scoped advisory lock: released at commit or rollback; a
-- concurrent booking of the same person waits. Lock order is always person, then shift
-- (get_shift_for_update.sql). The first key namespaces the lock to this module so it cannot
-- collide with another module's advisory locks.
-- Use Exec. Args: $1 person_id uuid. Columns: none useful (one void row).
SELECT pg_advisory_xact_lock(hashtext('volunteers.signup'), hashtext($1::uuid::text))
