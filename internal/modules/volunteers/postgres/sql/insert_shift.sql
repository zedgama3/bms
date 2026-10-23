-- Store.InsertShift: CreateShift (copied_from NULL). Use QueryRow. Write transaction.
-- Args: $1 id uuid, $2 department_id uuid, $3 position_id uuid, $4 event_id uuid (NULL =
--       standing work), $5 label text, $6 starts timestamptz, $7 ends timestamptz, $8 zone text
--       (the event's zone when event_id is set; Go decides), $9 slots integer, $10 location
--       text, $11 notes text, $12 copied_from uuid (NULL), $13 created_by uuid (NULL = system),
--       $14 created_at timestamptz (also updated_at).
-- Columns, in order (the "shift columns", used by every shift query): id uuid, department_id
--   uuid, position_id uuid, event_id uuid NULL, label text, starts timestamptz, ends
--   timestamptz, zone text, slots integer, location text, notes text, canceled_at timestamptz
--   NULL, cancel_reason text NULL, copied_from uuid NULL, created_by uuid NULL, created_at
--   timestamptz, updated_at timestamptz.
-- Constraint errors: shift_position_department_fkey (23503: the position is unknown or of
--   another department: Invalid, field position; Go checks first and says which);
--   shift_event_id_fkey (23503: Invalid, field event, code unknown); shift_times_chk (23514:
--   Invalid range: Ends <= Starts or longer than 48h); shift_slots_chk (range);
--   shift_label_length_chk, shift_label_chars_chk, shift_zone_length_chk,
--   shift_location_length_chk, shift_notes_length_chk (Invalid).
INSERT INTO volunteers.shift
    (id, department_id, position_id, event_id, label, starts, ends, zone, slots, location, notes,
     copied_from, created_by, created_at, updated_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::text, $6::timestamptz, $7::timestamptz, $8::text,
        $9::integer, $10::text, $11::text, $12::uuid, $13::uuid, $14::timestamptz, $14::timestamptz)
RETURNING id, department_id, position_id, event_id, label, starts, ends, zone, slots, location, notes,
          canceled_at, cancel_reason, copied_from, created_by, created_at, updated_at
