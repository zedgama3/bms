-- Store.Insert. A new account (SignUp). Exec; no rows returned. Args, in the column order of
-- account.Record:
--   $1 id uuid, $2 person_id uuid, $3 email text (as entered), $4 password_hash text,
--   $5 superuser boolean, $6 password_change_required boolean, $7 session_generation integer,
--   $8 disabled_at timestamptz (NULL), $9 last_login_at timestamptz (NULL),
--   $10 password_changed_at timestamptz, $11 created_at timestamptz, $12 updated_at timestamptz.
-- email_canonical is generated: never supplied. A duplicate address fails 23505 on
-- account_email_canonical_key (db.Wrap: Conflict, Constraint set); a second account for one
-- person fails on account_person_id_key.
INSERT INTO core.account (id, person_id, email, password_hash, superuser,
                          password_change_required, session_generation, disabled_at,
                          last_login_at, password_changed_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
