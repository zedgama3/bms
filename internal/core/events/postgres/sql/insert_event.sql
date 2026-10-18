-- Store.Insert: Create (transaction 1 of Create). Use Query. Inside the Write transaction.
-- Args: $1 id uuid, $2 name text, $3 kind text, $4 starts_at timestamptz, $5 ends_at
-- timestamptz, $6 zone text, $7 created_at timestamptz (also updated_at).
-- Columns, in order: as get_event.sql.
-- Constraint errors (*db.Error.Constraint): event_name_key (Conflict, name taken);
-- event_kind_fkey (23503; Invalid, kind unknown); event_dates_chk (Invalid, dates.end range);
-- event_name_length_chk, event_name_chars_chk (Invalid, name); event_zone_length_chk
-- (Invalid, zone).
INSERT INTO core.event (id, name, kind, starts_at, ends_at, zone, closed_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, NULL, $7, $7)
RETURNING id, name, kind, starts_at, ends_at, zone, closed_at, created_at, updated_at
