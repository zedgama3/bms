-- Store.State: the demo marker. Used by bms check's "demo:" line (a Read), by Seed's and
-- Reset's pre-checks, and by Reset's first step (Conflict not_demo when there is no row,
-- checked before anything is touched). Read-only; use Query (zero or one row).
-- Args: none.
-- Columns, in order: seed_version integer (scan into int32), instance text, started_at
-- timestamptz, seeded_at timestamptz (NULL: seeding, or a failed seed). ZERO rows: no marker,
-- so this database is not a demo (or was never seeded).
-- Served by demo_state_pkey (one row at most).
SELECT seed_version, instance, started_at, seeded_at
FROM core.demo_state
WHERE id = 1
