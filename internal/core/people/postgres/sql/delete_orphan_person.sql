-- Store.DeleteOrphan: LinkAccount's transaction 2 (best effort). Deletes the row sign-up
-- minted for the account only when it is still empty and nothing in people's tables points
-- at it; zero rows means "kept" (OrphanDeleted false). A reference from another set (a role
-- grant, a module table) fails 23503 on that table's constraint: the store treats it as
-- "kept" too. The test is the spec's (playa_name, pronouns, legal_name, phone empty; no
-- contacts, consents or requests) plus contact_email NULL, so no personal data is dropped by
-- accident. language is a preference, not data about the person, and is not tested.
-- Use Query. Args: $1 person_id uuid. Columns: id uuid.
DELETE FROM core.person p
WHERE p.id = $1
  AND p.playa_name = '' AND p.pronouns = '' AND p.legal_name = '' AND p.phone = ''
  AND p.contact_email IS NULL
  AND NOT EXISTS (SELECT 1 FROM core.person_emergency_contact c WHERE c.person_id = p.id)
  AND NOT EXISTS (SELECT 1 FROM core.person_consent k WHERE k.person_id = p.id)
  AND NOT EXISTS (SELECT 1 FROM core.data_request r WHERE r.person_id = p.id)
RETURNING p.id
