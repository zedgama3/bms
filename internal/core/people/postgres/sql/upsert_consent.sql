-- Store.SetConsent. Use Query (one row back). A person that does not exist fails 23503 on
-- person_consent_person_id_fkey (the store re-kinds it to NotFound). Overwrites the current
-- state; history is the audit log (people.consent.changed).
-- Args: $1 person_id uuid, $2 purpose text, $3 granted boolean, $4 recorded_by uuid (the
-- actor; NULL for a command), $5 changed_at timestamptz.
-- Columns, in order: person_id uuid, purpose text, granted boolean, recorded_by uuid (NULL),
-- changed_at timestamptz.
INSERT INTO core.person_consent (person_id, purpose, granted, recorded_by, changed_at)
VALUES ($1::uuid, $2::text, $3::boolean, $4::uuid, $5::timestamptz)
ON CONFLICT (person_id, purpose) DO UPDATE SET
    granted = EXCLUDED.granted, recorded_by = EXCLUDED.recorded_by, changed_at = EXCLUDED.changed_at
RETURNING person_id, purpose, granted, recorded_by, changed_at
