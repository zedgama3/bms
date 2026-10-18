-- Store.SeedStarterPermissions: Seed, in one statement. Use Query. Inside the Write
-- transaction, after lock_invariants.sql. Idempotent.
-- Args: $1 role_keys text[] and $2 permissions text[], two parallel arrays of the same length:
-- one element per (starter role key, permission) pair taken from the declarations'
-- StarterRoles (repeats are fine); $3 seeded_at timestamptz (from clock.Clock).
-- Columns: role_id uuid, permission text = one row per role_permission row inserted. Every pair not yet in
-- core.role_seed is recorded in it, whether or not a role with that key exists; a pair whose
-- role exists is also inserted into core.role_permission (a pair already there is left alone).
-- A pair already recorded is skipped, so a regional's later edits are never undone and a
-- deleted starter role never returns.
WITH wanted AS (
    SELECT DISTINCT w.role_key, w.permission
    FROM unnest($1::text[], $2::text[]) AS w (role_key, permission)
),
fresh AS (
    INSERT INTO core.role_seed (role_key, permission, seeded_at)
    SELECT role_key, permission, $3::timestamptz
    FROM wanted
    ON CONFLICT (role_key, permission) DO NOTHING
    RETURNING role_key, permission
),
added AS (
    INSERT INTO core.role_permission (role_id, permission)
    SELECT r.id, f.permission
    FROM fresh f
    JOIN core.role r ON r.key = f.role_key
    ON CONFLICT (role_id, permission) DO NOTHING
    RETURNING role_id, permission
)
SELECT role_id, permission
FROM added
ORDER BY role_id, permission
