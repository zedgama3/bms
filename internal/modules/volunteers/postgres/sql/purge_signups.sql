-- Store.PurgeSignups: volunteers.purge, sign-up leg: deletes sign-ups (booked or canceled)
-- whose shift ended before the cutoff, one batch. Exec; the count is RowsAffected(); the job
-- loops until it returns less than the batch size.
-- Args: $1 cutoff timestamptz (now minus volunteers.retention), $2 limit integer (batch, 500).
-- No ORDER BY: any 500 qualifying rows will do and the job loops until none are left.
-- Walks shift by (starts) via shift_starts_idx for the ended shifts, then signup_shift_status_idx.
DELETE FROM volunteers.signup
WHERE id IN (
    SELECT su.id
    FROM volunteers.signup su
    JOIN volunteers.shift s ON s.id = su.shift_id
    WHERE s.ends < $1::timestamptz
    LIMIT $2::integer)
