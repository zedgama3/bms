-- Store.GetShiftForUpdate: the second lock of a booking (SignUp, Assign), and the first
-- statement of UpdateShift and CancelShift. Inside a Write transaction. Serialises everything
-- that changes who is booked on the shift, so slots and capacity hold under concurrency.
-- Order of locks for a booking: lock_person_bookings.sql first, then this. No row = NotFound.
-- Args: $1 id uuid. Columns: the shift columns (see insert_shift.sql).
-- FOR UPDATE (not NO KEY): the row's key columns are not changed, but a sign-up insert takes
-- FOR KEY SHARE on the shift through its foreign key, and FOR UPDATE makes a concurrent
-- booking wait for the count below to be settled.
SELECT id, department_id, position_id, event_id, label, starts, ends, zone, slots, location, notes,
       canceled_at, cancel_reason, copied_from, created_by, created_at, updated_at
FROM volunteers.shift
WHERE id = $1::uuid
FOR UPDATE
