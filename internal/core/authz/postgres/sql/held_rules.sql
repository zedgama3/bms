-- Store.RulesHeldBy: step 3b of every decision, and the Grant-time separation-of-duties check
-- (run it after the insert, inside the same transaction, so the new grant counts). The
-- separation-of-duties rules whose role the person holds now, in any extent (an active grant,
-- or the base role, which everyone holds). Read-only; use Query.
-- Args: $1 person_id uuid, $2 now timestamptz.
-- Columns, in order: id uuid, role_id uuid, permission text (NULL for a level rule),
-- reaches text (NULL for a permission rule; one of public, internal, personal, sensitive),
-- note text. Exactly one of permission and reaches is non-NULL. Sorted by created_at, id.
-- The forbidden names (a rule's permission, or every declared name whose Reaches is at least
-- the rule's level) are worked out in Go from the in-memory catalogue.
SELECT s.id, s.role_id, s.permission, s.reaches, s.note
FROM core.sod_rule s
WHERE s.role_id IN (
    SELECT g.role_id
    FROM core.role_grant g
    WHERE g.person_id = $1
      AND (g.ends_at IS NULL OR g.ends_at > $2::timestamptz)
    UNION
    SELECT b.id
    FROM core.role b
    WHERE b.base)
ORDER BY s.created_at, s.id
