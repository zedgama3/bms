-- Store.MarkSeeded: Seed's last step; the seed worked. Use Query (RETURNING).
-- Args: $1 seeded_at timestamptz (the service clock; must not be before started_at).
-- Columns, in order: seeded_at timestamptz. ZERO rows: there is no marker, or it is already
-- seeded (a second Done); the caller treats it as Internal (the seed owns the marker).
-- Fails 25006 (db.ErrReadOnly) on a read-only copy.
-- Constraint errors (Invalid): demo_state_seeded_at_chk (seeded_at before started_at).
UPDATE core.demo_state
SET seeded_at = $1::timestamptz
WHERE id = 1
  AND seeded_at IS NULL
RETURNING seeded_at
