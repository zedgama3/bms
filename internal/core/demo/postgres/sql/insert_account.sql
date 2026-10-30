-- Store.AddAccount: record one demo account, right after EnsureAccount returned it (Seed step
-- 1, in key order). Use Exec (the row count is 1 or an error).
-- Args: $1 account_id uuid, $2 person_id uuid, $3 key text (a demoseed.Actor* value),
-- $4 email text ("<key>@demo.invalid"), $5 position integer (0-based, in table order).
-- Columns: none.
-- Fails 25006 (db.ErrReadOnly) on a read-only copy.
-- Constraint errors (*db.Error.Constraint): 23505 Conflict on demo_account_pkey,
-- demo_account_key_key, demo_account_position_key (the seed ran twice: a bug, the marker rule
-- stops it first); 23503 Invalid on demo_account_account_id_fkey, demo_account_person_id_fkey
-- (no such account or person); Invalid on demo_account_key_chk, demo_account_email_chk (the
-- address must be <key>@demo.invalid), demo_account_position_chk, demo_account_nil_uuid_chk.
INSERT INTO core.demo_account (account_id, person_id, key, email, position)
VALUES ($1::uuid, $2::uuid, $3::text, $4::text, $5::integer)
