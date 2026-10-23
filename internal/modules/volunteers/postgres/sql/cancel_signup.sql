-- Store.CancelSignup: CancelSignup (reason self or lead). Use QueryRow; zero rows = not booked
-- any more (Conflict status; the use case read it FOR UPDATE first).
-- Args: $1 id uuid, $2 canceled_by uuid (the actor), $3 reason text ('self' or 'lead'),
--       $4 at timestamptz (canceled_at and updated_at).
-- Columns: the signup columns (see insert_signup.sql).
UPDATE volunteers.signup
SET status = 'canceled', canceled_at = $4::timestamptz, canceled_by = $2::uuid,
    cancel_reason = $3::text, updated_at = $4::timestamptz
WHERE id = $1::uuid
  AND status = 'booked'
RETURNING id, shift_id, person_id, status, booked_by, booked_as, waived, waived_by, canceled_at, canceled_by,
          cancel_reason, created_at, updated_at
