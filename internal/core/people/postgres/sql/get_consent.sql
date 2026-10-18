-- Store.Consent: Directory.ConsentOf. Read-only; use Query (zero rows: not granted, nil
-- error; "unknown" and "never asked" are both false). Args: $1 person_id uuid, $2 purpose text.
-- Columns, in order: granted boolean.
SELECT granted
FROM core.person_consent
WHERE person_id = $1 AND purpose = $2
