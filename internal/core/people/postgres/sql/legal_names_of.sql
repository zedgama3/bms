-- Store.LegalNamesOf: the batch lookup behind Directory.LegalNamesOf (people.md Amendment
-- 2026-10-20, D44; Proposed change 2). Read-only; use Query. PERSONAL level: legal_name is
-- 'sensitivity: personal'. The use case (not this statement) requires people.contacts.read in
-- any extent and records one people.person.read per returned row (Fields [legal_name], Level
-- personal) before returning the map. Never call this without that check and audit.
-- Args: $1 ids uuid[] (Go rejects more than MaxNames = 200 before calling; duplicates are
-- harmless: a person is returned once). Columns, in order: id uuid, legal_name text.
-- Unknown or deleted IDs and people whose legal_name is '' (never filled in) yield no row, so
-- the use case audits exactly the rows it gets back (F69). No ORDER BY: the result is a map.
-- Uses the primary key, one probe per ID.
SELECT id, legal_name
FROM core.person
WHERE id = ANY($1::uuid[])
  AND legal_name <> ''
