-- Store.CancelLeaveSignups: Leave cancels the person's future bookings on members-only
-- positions of the department (reason self, canceled by the person), in the same Write
-- transaction as delete_member.sql. Use Query. Idempotent on state.
-- Args: $1 person_id uuid, $2 department_id uuid, $3 now timestamptz (canceled_at and
--       updated_at; only shifts that have not started), $4 reason text ('self').
-- Columns: id uuid (signup), shift_id uuid, person_id uuid, department_id uuid, label text,
--   starts timestamptz, zone text. One row per canceled sign-up: the use case publishes
--   volunteers.signup-canceled for each. canceled_by is the person.
-- Served by signup_person_status_idx.
WITH canceled AS (
    UPDATE volunteers.signup su
    SET status = 'canceled', canceled_at = $3::timestamptz, canceled_by = $1::uuid,
        cancel_reason = $4::text, updated_at = $3::timestamptz
    FROM volunteers.shift sh
    JOIN volunteers.position p ON p.id = sh.position_id
    WHERE su.shift_id = sh.id
      AND su.person_id = $1::uuid
      AND su.status = 'booked'
      AND sh.department_id = $2::uuid
      AND p.access = 'members'
      AND sh.canceled_at IS NULL
      AND sh.starts > $3::timestamptz
    RETURNING su.id, su.shift_id, su.person_id, sh.department_id, sh.label, sh.starts, sh.zone
)
SELECT id, shift_id, person_id, department_id, label, starts, zone
FROM canceled
ORDER BY starts, id
