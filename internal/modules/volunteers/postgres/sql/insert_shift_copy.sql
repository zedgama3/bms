-- Store.InsertShiftCopy: CopyShifts, one shift. Same as insert_shift.sql but copied_from is
-- required and a shift already copied from that source into that event is skipped (the
-- idempotency of F88, race-safe because the unique index shift_copied_key decides).
-- Use QueryRow: pgx.ErrNoRows = skipped (already copied), not an error.
-- Args and columns: as insert_shift.sql ($12 copied_from must not be NULL, $4 event_id must
-- not be NULL: the target event).
INSERT INTO volunteers.shift
    (id, department_id, position_id, event_id, label, starts, ends, zone, slots, location, notes,
     copied_from, created_by, created_at, updated_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::text, $6::timestamptz, $7::timestamptz, $8::text,
        $9::integer, $10::text, $11::text, $12::uuid, $13::uuid, $14::timestamptz, $14::timestamptz)
ON CONFLICT (copied_from, event_id) WHERE copied_from IS NOT NULL DO NOTHING
RETURNING id, department_id, position_id, event_id, label, starts, ends, zone, slots, location, notes,
          canceled_at, cancel_reason, copied_from, created_by, created_at, updated_at
