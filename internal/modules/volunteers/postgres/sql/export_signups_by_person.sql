-- Store.ExportSignups: ExportPerson's "signups". Use Query. Read-only. Complete (not
-- paginated): the export must be whole. Who booked or canceled is given as a role word,
-- never a name or an ID: 'self' (the person), 'lead', or for cancellation also 'system'.
-- waived comes from the waived column (D51 c), which survives the waiving lead's deletion
-- (waived_by does not). Both roles come from columns that survive a lead's deletion (F154): booked_by_role from
-- booked_as, canceled_by_role from cancel_reason ('self' -> self, 'lead' -> lead, the three
-- system reasons shift_canceled, department_archived, event_closed -> system), never from
-- booked_by / canceled_by, which DeletePerson nulls.
-- Args: $1 person_id uuid.
-- Columns: label text, department_id uuid (the service adds the name), position_name text,
--   starts timestamptz, ends timestamptz, zone text, status text, booked_by_role text,
--   waived boolean, canceled_at timestamptz NULL, canceled_by_role text NULL, cancel_reason
--   text NULL, created_at timestamptz.
-- Order: starts, signup id. Served by signup_person_status_idx.
SELECT s.label, s.department_id, p.name AS position_name, s.starts, s.ends, s.zone, su.status,
       su.booked_as AS booked_by_role,
       su.waived,
       su.canceled_at,
       CASE WHEN su.status <> 'canceled' THEN NULL
            WHEN su.cancel_reason = 'self' THEN 'self'
            WHEN su.cancel_reason = 'lead' THEN 'lead'
            ELSE 'system' END AS canceled_by_role,
       su.cancel_reason, su.created_at
FROM volunteers.signup su
JOIN volunteers.shift s ON s.id = su.shift_id
JOIN volunteers.position p ON p.id = s.position_id
WHERE su.person_id = $1::uuid
ORDER BY s.starts, su.id
