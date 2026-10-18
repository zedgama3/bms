-- Store.EffectivePermissions: step 3a of every decision for PermissionsOf, and the caller's
-- and target's set for Grant (no-escalation check, separation-of-duties check). Read-only;
-- runs on a read-only copy. Use Query.
-- Args: $1 person_id uuid, $2 now timestamptz (from clock.Clock; "active" means
-- ends_at IS NULL OR ends_at > $2).
-- Columns, in order: permission text, department_id uuid (NULL: held site-wide).
-- Rows are distinct and sorted by permission (byte order), then NULL (site) first, then
-- department_id. They are the person's active grants joined to their roles' permissions, plus
-- the base role's permissions as site-wide. The rules (held_rules.sql) are NOT applied here.
--
-- A grant whose department does not match its role's scope (a site role with a department, a
-- department role without one) is a row the service never writes; if one exists anyway it
-- grants nothing (fail closed).
SELECT permission, department_id
FROM (
    SELECT rp.permission, g.department_id
    FROM core.role_grant g
    JOIN core.role r ON r.id = g.role_id
    JOIN core.role_permission rp ON rp.role_id = g.role_id
    WHERE g.person_id = $1
      AND (g.ends_at IS NULL OR g.ends_at > $2::timestamptz)
      AND (r.scope = 'site') = (g.department_id IS NULL)
    UNION
    SELECT rp.permission, NULL::uuid
    FROM core.role b
    JOIN core.role_permission rp ON rp.role_id = b.id
    WHERE b.base
) AS e
ORDER BY permission COLLATE "C", department_id NULLS FIRST
