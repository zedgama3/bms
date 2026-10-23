-- Store.SetUnlisted: SetListed(false). Exec; RowsAffected 1 = changed (audit
-- volunteers.listing.changed, Reason unlisted), 0 = already unlisted (no audit, F142).
-- Args: $1 person_id uuid, $2 at timestamptz (created_at and updated_at).
-- Constraint errors: unlisted_person_id_fkey (23503: Invalid, unknown person).
INSERT INTO volunteers.unlisted (person_id, created_at, updated_at)
VALUES ($1::uuid, $2::timestamptz, $2::timestamptz)
ON CONFLICT (person_id) DO NOTHING
