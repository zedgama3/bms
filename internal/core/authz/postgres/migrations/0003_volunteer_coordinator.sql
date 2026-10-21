-- authz 0003: the ninth starter role, "volunteer-coordinator" (authz.md Amendment
-- 2026-10-20, D44, Q-PRIV4; privacy-tags Proposed change 2; volunteers Proposed change 9).
--
-- Inserts one row into core.role: key, scope and the base flag only, no permissions, exactly
-- as 0001 did for the first eight. Service.Seed fills the membership (people.directory.read,
-- people.contacts.read, volunteers.rosters.read, volunteers.qualifications.read) from the
-- declarations' StarterRoles at the next "bms migrate", once per (role key, permission).
-- The volunteers permissions are applied by that Seed once the module is enabled and has
-- declared them (a pair for a key with no role is not recorded; see seed_starter_permissions.sql).
--
-- Creates no table and touches no other row except as described below, so it is safe on a
-- database with data and takes only a brief ROW EXCLUSIVE lock on core.role and core.role_seed.
-- Idempotent: ON CONFLICT (key) DO NOTHING (role_key_key) leaves an existing row alone.
--
-- Phantom ledger rows: before this migration Seed recorded a (role key, permission) pair in
-- core.role_seed even when no role had that key. No role with this key can exist before this
-- migration (only migrations write core.role.key), so any role_seed row for it is a phantom
-- that would stop the pair from ever being applied. They are deleted first, only while the
-- role does not exist. After the role row exists the ledger is never touched here, so a
-- regional's later edits and a deleted starter role stay as they are.
--
-- Nothing here is personal data. All columns are "internal" as in 0001; retention: the row's
-- lifetime. The table and column comments of 0001 cover the new row.

DELETE FROM core.role_seed s
WHERE s.role_key = 'volunteer-coordinator'
  AND NOT EXISTS (SELECT 1 FROM core.role r WHERE r.key = 'volunteer-coordinator');

INSERT INTO core.role (id, key, name, description, scope, base, created_at, updated_at)
VALUES (gen_random_uuid(), 'volunteer-coordinator', '', '', 'site', false, now(), now())
ON CONFLICT (key) DO NOTHING;
