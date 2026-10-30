-- Store.EnsureAccount: create a participant account, or reset an existing participant
-- account's password, in one statement (the caller runs it in its Write transaction, with
-- the signal and the audit entry). Args: $1 id uuid, $2 person_id uuid (both fresh; ignored
-- on a reset), $3 email text (as given), $4 password_hash text, $5 at timestamptz.
--
-- No account at that canonical address: inserted {superuser false, generation 0,
-- password_change_required false}. A participant account exists: password replaced,
-- disabled_at cleared, password_change_required cleared, session_generation + 1 (every
-- session ends), the stored address and IDs kept. A superuser account exists: nothing
-- changes and no row is returned.
--
-- Returns one row: id uuid, person_id uuid (the existing ones on a reset), created boolean
-- (xmax = 0: true only for a row this statement inserted). Use QueryRow. ErrNoRows
-- (db.Wrap: NotFound) can only mean "a superuser account has this address": the store
-- re-kinds it errs.Conflict.
INSERT INTO core.account AS a (id, person_id, email, password_hash, superuser,
                               password_change_required, session_generation,
                               password_changed_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, false, false, 0, $5, $5, $5)
ON CONFLICT (email_canonical) DO UPDATE
SET password_hash            = EXCLUDED.password_hash,
    password_change_required = false,
    password_changed_at      = EXCLUDED.password_changed_at,
    disabled_at              = NULL,
    session_generation       = a.session_generation + 1,
    updated_at               = EXCLUDED.updated_at
WHERE NOT a.superuser
RETURNING a.id, a.person_id, (xmax = 0) AS created
