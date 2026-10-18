-- Store.ExportConsents: the "consents" array of the export document. Read-only; use Query
-- (zero rows: []). Args: $1 person_id uuid.
-- Columns, in order: as list_consents_by_person.sql (the document uses purpose, granted,
-- changed_at). Ordered by purpose.
SELECT person_id, purpose, granted, recorded_by, changed_at
FROM core.person_consent
WHERE person_id = $1
ORDER BY purpose COLLATE "C"
