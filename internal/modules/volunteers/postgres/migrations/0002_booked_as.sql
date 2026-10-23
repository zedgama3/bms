-- volunteers 0002: signup.booked_as (module-volunteers.md Amendment V1, D49) and signup.waived
-- (D51 c); "Data", signup. Neither column has shipped, so both ride in this one file.
--
-- booked_as says WHO booked a sign-up as a role word, 'self' or 'lead', so the export can
-- still say it after the lead is deleted: DeletePerson nulls booked_by (anonymise_person.sql)
-- and the role would otherwise be lost. It is not a foreign key and not a person ID; it
-- holds no personal data beyond the sign-up row it sits on (sensitivity: internal, retained
-- and deleted with the row). booked_by, waived_by and canceled_by keep their ON DELETE
-- RESTRICT foreign keys from 0001: people calls DeletePerson first, which nulls them.
--
-- Backfill, as the spec says: 'lead' where booked_by or waived_by is set, else 'self'. A
-- sign-up whose booking lead was deleted BEFORE this migration already has booked_by NULL
-- and cannot be told from a self-booking; it backfills 'self' (the export said 'self' for
-- it already).
--
-- waived says that a requirement was waived when a lead booked the sign-up (Assign with
-- Waive, D42). waived_by is the audit link to the lead and DeletePerson nulls it, so the
-- export's waived and the roster's flag could not read it without going false when the lead
-- is deleted; waived is the flag that survives. Backfill: true where waived_by is set, else
-- false (a waiver whose lead was deleted before this migration cannot be told from no waiver;
-- none exists, as the module has not shipped). Not personal data on its own (sensitivity:
-- internal, retained and deleted with the sign-up row).
--
-- Safe on a database with data: ADD COLUMN with a constant default is a catalog change (no
-- table rewrite); the UPDATEs touch only rows a lead booked or waived for; the CHECK is
-- validated by one scan of volunteers.signup, a table of at most tens of thousands of rows at
-- a regional's scale, under a short ACCESS EXCLUSIVE lock held to the end of this
-- transaction. The
-- defaults exist only to make the ADD COLUMNs cheap; they are dropped at the end so that the
-- store must say who booked and whether a requirement was waived (insert_signup.sql takes
-- both as parameters) and a forgotten value is a not-null error, not a silent 'self' or
-- false.

ALTER TABLE volunteers.signup ADD COLUMN booked_as text NOT NULL DEFAULT 'self';
ALTER TABLE volunteers.signup ADD COLUMN waived boolean NOT NULL DEFAULT false;

UPDATE volunteers.signup
SET booked_as = 'lead'
WHERE booked_by IS NOT NULL OR waived_by IS NOT NULL;

UPDATE volunteers.signup
SET waived = true
WHERE waived_by IS NOT NULL;

ALTER TABLE volunteers.signup ALTER COLUMN booked_as DROP DEFAULT;
ALTER TABLE volunteers.signup ALTER COLUMN waived DROP DEFAULT;

-- Violation: constraint "signup_booked_as_chk" (23514: Invalid; only a bug in the store).
ALTER TABLE volunteers.signup
    ADD CONSTRAINT signup_booked_as_chk CHECK (booked_as IN ('self', 'lead'));

COMMENT ON COLUMN volunteers.signup.booked_as IS 'sensitivity: internal (a role word, self or lead: who booked; survives the booking lead''s deletion)';
COMMENT ON COLUMN volunteers.signup.waived IS 'sensitivity: internal (a flag: a requirement was waived when a lead booked this sign-up; survives the waiving lead''s deletion, which nulls waived_by)';
