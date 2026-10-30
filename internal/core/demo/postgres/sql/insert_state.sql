-- Store.BeginSeed, first statement: write the marker with seeded_at NULL. Run in the Write
-- transaction of Seed step 0, BEFORE has_non_superuser_account.sql: an error in that step rolls
-- the marker back, so the order loses nothing and "seeded" is still reported before "not_empty".
-- Use Query (RETURNING).
-- Args: $1 seed_version integer (demo.SeedVersion), $2 instance text, $3 started_at timestamptz
-- (the service clock).
-- Columns, in order: id smallint (always 1). ZERO rows: a marker already exists (a concurrent
-- seed commits first and this statement then waits and does nothing), so the caller returns
-- Conflict seeded. Race-safe without a prior read.
-- Fails 25006 (db.ErrReadOnly) on a read-only copy.
-- Constraint errors (*db.Error.Constraint, Invalid, each a caller bug): demo_state_seed_version_chk,
-- demo_state_instance_chk.
INSERT INTO core.demo_state (id, seed_version, instance, started_at, seeded_at)
VALUES (1, $1::integer, $2::text, $3::timestamptz, NULL)
ON CONFLICT (id) DO NOTHING
RETURNING id
