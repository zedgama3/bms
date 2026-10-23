-- Store.AnonymisePerson: DeletePerson, last step (after the delete_*_by_person statements):
-- nulls every "who did it" column that names the person: qualification_record.verified_by,
-- member.added_by, signup.booked_by / waived_by / canceled_by, shift.created_by. Use QueryRow.
-- Idempotent. One statement: the signup columns are updated by ONE UPDATE (three data-
-- modifying CTEs on the same table would not all apply). updated_at is left alone (not a
-- change to the record's meaning). signup.booked_as (self or lead, a role word, not a person)
-- is deliberately left as it is: it is what lets the export still say who booked after
-- booked_by is nulled here (F154).
-- Args: $1 person_id uuid.
-- Columns, the counts of rows changed (scan into int64): records, members, signups, shifts.
WITH r AS (
    UPDATE volunteers.qualification_record SET verified_by = NULL
    WHERE verified_by = $1::uuid RETURNING 1
), m AS (
    UPDATE volunteers.member SET added_by = NULL
    WHERE added_by = $1::uuid RETURNING 1
), su AS (
    UPDATE volunteers.signup
    SET booked_by   = CASE WHEN booked_by   = $1::uuid THEN NULL ELSE booked_by   END,
        waived_by   = CASE WHEN waived_by   = $1::uuid THEN NULL ELSE waived_by   END,
        canceled_by = CASE WHEN canceled_by = $1::uuid THEN NULL ELSE canceled_by END
    WHERE booked_by = $1::uuid OR waived_by = $1::uuid OR canceled_by = $1::uuid RETURNING 1
), sh AS (
    UPDATE volunteers.shift SET created_by = NULL
    WHERE created_by = $1::uuid RETURNING 1
)
SELECT (SELECT count(*) FROM r) AS records,
       (SELECT count(*) FROM m) AS members,
       (SELECT count(*) FROM su) AS signups,
       (SELECT count(*) FROM sh) AS shifts
