-- Store.PermissionHolders: the first step of Service.HoldersOf (D39). The persons who hold one
-- permission site-wide through an explicit grant now: never the base role (everyone holds it;
-- authz.md, "HoldersOf and the base role"). Read-only; runs on a read-only copy. Use Query.
-- Args: $1 permission text, $2 now timestamptz (from clock.Clock; "active" means
-- ends_at IS NULL OR ends_at > $2).
-- Columns, in order: person_id uuid. Distinct, in person_id order. A grant counts when it is
-- active, site-wide (department_id IS NULL) and on a site-scoped, non-base role that holds $1.
-- The separation-of-duties rules (held_rules.sql) are NOT applied here; the service drops the
-- persons a rule denies $1.
SELECT DISTINCT g.person_id
FROM core.role_permission rp
JOIN core.role r ON r.id = rp.role_id
JOIN core.role_grant g ON g.role_id = r.id
WHERE rp.permission = $1::text
  AND NOT r.base
  AND r.scope = 'site'
  AND g.department_id IS NULL
  AND (g.ends_at IS NULL OR g.ends_at > $2::timestamptz)
ORDER BY g.person_id
