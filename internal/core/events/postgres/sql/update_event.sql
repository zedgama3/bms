-- Store.Update: a partial update of an open event (the use case has locked the row and checked
-- closed_at). Use Query; zero rows: NotFound.
-- Args: $1 id uuid, $2 name text, $3 kind text, $4 zone text, $5 starts_at timestamptz,
-- $6 ends_at timestamptz, $7 updated_at timestamptz. NULL leaves a column unchanged. Dates
-- travel together: $5 and $6 are both NULL or both set (one TimeRange); a half-set pair would
-- still be checked by event_dates_chk.
-- Columns, in order: as get_event.sql.
-- Constraint errors: event_name_key (Conflict, name taken); event_kind_fkey (Invalid, kind
-- unknown); event_dates_chk (Invalid, dates.end range); the length and chars checks (Invalid).
UPDATE core.event SET
    name       = COALESCE($2::text, name),
    kind       = COALESCE($3::text, kind),
    zone       = COALESCE($4::text, zone),
    starts_at  = COALESCE($5::timestamptz, starts_at),
    ends_at    = COALESCE($6::timestamptz, ends_at),
    updated_at = $7::timestamptz
WHERE id = $1
RETURNING id, name, kind, starts_at, ends_at, zone, closed_at, created_at, updated_at
