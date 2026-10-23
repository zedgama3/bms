-- Store.Listed: Listed (and the export's "listed"): true unless the person has an unlisted
-- row. Use QueryRow. Read-only. Own record.
-- Args: $1 person_id uuid. Columns: listed boolean. Served by unlisted_pkey.
SELECT NOT EXISTS (SELECT 1 FROM volunteers.unlisted WHERE person_id = $1::uuid) AS listed
