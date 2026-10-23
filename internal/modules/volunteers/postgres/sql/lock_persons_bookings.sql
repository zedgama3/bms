-- Store.LockPersonsBookings: UpdateShift with a time change: takes the same per-person locks
-- as lock_person_bookings.sql for everyone booked on the shift, in ascending ID order (no
-- deadlock between two such updates), BEFORE get_shift_for_update.sql, so a concurrent SignUp
-- of one of them onto an overlapping shift cannot slip in between find_overlap_for_shift_change
-- and the commit. The use case reads list_roster.sql first (unlocked) to learn who to lock and
-- re-reads it after taking the shift lock; anyone newly booked in between is checked by
-- find_overlap_for_shift_change.sql but not locked (see the report: a residual window the spec
-- does not close).
-- Use Exec. Args: $1 person_ids uuid[]. Columns: none useful.
SELECT pg_advisory_xact_lock(hashtext('volunteers.signup'), hashtext(s.p::text))
FROM (SELECT p FROM unnest($1::uuid[]) AS p ORDER BY p OFFSET 0) AS s
