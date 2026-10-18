-- Store.InsertScheduleItem: AddScheduleItem. Use Query. Inside the Write transaction, after
-- get_event_for_update.sql (the event lock makes the position computation race-free).
-- position is the next number for the event (1, 2, ...): insertion order.
-- Args: $1 id uuid, $2 event_id uuid, $3 key text ('' = none), $4 label text, $5 starts_at
-- timestamptz, $6 ends_at timestamptz (NULL = a moment), $7 created_at timestamptz (also
-- updated_at).
-- Columns, in order: as list_schedule_items.sql.
-- Constraint errors (*db.Error.Constraint): event_schedule_key (Conflict, key duplicate);
-- event_schedule_item_key_chk (Invalid, key malformed); event_schedule_item_ends_chk (Invalid,
-- ends_at range); event_schedule_item_label_length_chk, event_schedule_item_label_chars_chk
-- (Invalid, label); event_schedule_item_event_id_fkey (Invalid/NotFound: no such event).
INSERT INTO core.event_schedule_item
    (id, event_id, key, label, starts_at, ends_at, position, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6,
        (SELECT COALESCE(max(position), 0) + 1 FROM core.event_schedule_item WHERE event_id = $2),
        $7, $7)
RETURNING id, event_id, key, label, starts_at, ends_at, position
