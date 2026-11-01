-- Store count-about (Service.CountAbout): entries that name one person as actor or subject. A
-- row naming the person as both counts once. Args: $1 person_id uuid. One row, one column:
-- n bigint (scan into int64). Served by audit_entry_actor_idx and audit_entry_subject_idx
-- (a bitmap OR).
SELECT count(*) AS n
FROM core.audit_entry
WHERE actor_person_id = $1::uuid
   OR subject_person_id = $1::uuid
