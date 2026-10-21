-- Store.SeedStarterPermissions: Seed, in one statement. Use Query. Inside the Write
-- transaction, after lock_invariants.sql. Idempotent.
-- Args: $1 role_keys text[] and $2 permissions text[], two parallel arrays of the same length:
-- one element per (starter role key, permission) pair taken from the declarations'
-- StarterRoles (repeats are fine); $3 seeded_at timestamptz (from clock.Clock).
-- Columns: role_id uuid, permission text = one row per role_permission row inserted.
--
-- Amended for D44 (authz.md, Seed; F58): a pair is acted on only when a role with that key
-- exists. A pair whose role exists and that is not yet in core.role_seed is recorded in it
-- and inserted into core.role_permission (a pair already there is left alone). A pair whose
-- role does not exist is skipped and NOT recorded, so a later "bms migrate" retries it once
-- the role exists (for example volunteers lists volunteer-coordinator before authz 0003 ran).
-- A pair already recorded is skipped, so a regional's later edits are never undone, and a
-- starter role the regional deleted never returns: its pairs were recorded while it existed,
-- and a pair first declared after the deletion finds no role and is skipped.
--
-- Because the ledger insert and the role_permission insert are driven by the same join, a
-- pair is recorded if and only if its role exists. The statement runs under lock_invariants
-- in the Write transaction, so a role cannot be deleted between the join and the inserts.
WITH wanted AS (
    SELECT DISTINCT r.id AS role_id, w.role_key, w.permission
    FROM unnest($1::text[], $2::text[]) AS w (role_key, permission)
    JOIN core.role r ON r.key = w.role_key
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
    SELECT wd.role_id, f.permission
    FROM fresh f
    JOIN wanted wd ON wd.role_key = f.role_key AND wd.permission = f.permission
    ON CONFLICT (role_id, permission) DO NOTHING
    RETURNING role_id, permission
)
SELECT role_id, permission
FROM added
ORDER BY role_id, permission
