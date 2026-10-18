-- Store.BumpGeneration (EndOtherSessions): end every session of the account. The caller
-- then issues a new token with the returned generation.
-- Args: $1 id uuid, $2 at timestamptz. Returns the new session_generation integer (one row;
-- QueryRow: ErrNoRows -> NotFound).
UPDATE core.account
SET session_generation = session_generation + 1,
    updated_at         = $2
WHERE id = $1
RETURNING session_generation
