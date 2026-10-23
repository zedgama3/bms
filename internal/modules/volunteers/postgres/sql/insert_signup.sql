-- Store.InsertSignup: SignUp (booked_by NULL, waived_by NULL, booked_as 'self', waived false)
-- and Assign (booked_by the lead; booked_as 'lead', also when the lead assigns themself;
-- waived_by the lead and waived true when Waive was needed). Use QueryRow. Write transaction, after
-- lock_person_bookings.sql and get_shift_for_update.sql and the eligibility re-check.
-- Args: $1 id uuid, $2 shift_id uuid, $3 person_id uuid, $4 booked_by uuid (NULL = the person
--       themself), $5 waived_by uuid (NULL = no waiver), $6 created_at timestamptz (also
--       updated_at), $7 booked_as text ('self' from SignUp, 'lead' from Assign: who booked as
--       a role word, kept after the lead's deletion nulls booked_by; no default, so a missing
--       value is a not-null error; anything else fails signup_booked_as_chk, 23514),
--       $8 waived boolean (NEW, D51 c: true exactly when $5 is set, i.e. Assign waived a
--       requirement; the flag outlives waived_by, which DeletePerson nulls; no default).
-- Columns, in order (the "signup columns"): id uuid, shift_id uuid, person_id uuid, status
--   text, booked_by uuid NULL, booked_as text ('self' or 'lead'), waived boolean (D51 c: right
--   after booked_as), waived_by uuid NULL, canceled_at timestamptz NULL, canceled_by uuid NULL, cancel_reason text NULL, created_at timestamptz, updated_at timestamptz.
-- Constraint errors: signup_booked_key (23505: Conflict, code duplicate);
--   signup_person_id_fkey (23503: Invalid, field person, code unknown: Assign of an unknown
--   person); signup_shift_id_fkey (23503: unknown shift).
INSERT INTO volunteers.signup (id, shift_id, person_id, status, booked_by, booked_as, waived, waived_by, created_at, updated_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'booked', $4::uuid, $7::text, $8::boolean, $5::uuid, $6::timestamptz, $6::timestamptz)
RETURNING id, shift_id, person_id, status, booked_by, booked_as, waived, waived_by, canceled_at, canceled_by,
          cancel_reason, created_at, updated_at
