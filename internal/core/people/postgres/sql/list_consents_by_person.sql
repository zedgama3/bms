-- Store.Consents: a person's consents for the profile (self or people.profiles.manage).
-- Read-only; use Query. Bounded by the purposes modules declare, so no paging. Ordered by
-- purpose (byte order). Args: $1 person_id uuid.
-- Columns, in order: person_id uuid, purpose text, granted boolean, recorded_by uuid (NULL),
-- changed_at timestamptz. Served by person_consent_pkey.
SELECT person_id, purpose, granted, recorded_by, changed_at
FROM core.person_consent
WHERE person_id = $1
ORDER BY purpose COLLATE "C"
