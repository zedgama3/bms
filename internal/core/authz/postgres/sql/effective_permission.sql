-- Store.EffectiveFor: step 3a of Allow, AllowIn and ExtentOf when only one permission is asked
-- about. Same rows as effective_permissions.sql restricted to one name; read-only; use Query.
-- Args: $1 person_id uuid, $2 now timestamptz, $3 permission text.
-- Columns, in order: permission text, department_id uuid (NULL: held site-wide). Distinct;
-- site (NULL) first, then department_id. Zero rows: not held. A rule that forbids the name
-- (held_rules.sql, step 3b) still has to be applied by the caller.
SELECT permission, department_id
FROM (
    SELECT rp.permission, g.department_id
    FROM core.role_grant g
    JOIN core.role r ON r.id = g.role_id
    JOIN core.role_permission rp ON rp.role_id = g.role_id AND rp.permission = $3::text
    WHERE g.person_id = $1
      AND (g.ends_at IS NULL OR g.ends_at > $2::timestamptz)
      AND (r.scope = 'site') = (g.department_id IS NULL)
    UNION
    SELECT rp.permission, NULL::uuid
    FROM core.role b
    JOIN core.role_permission rp ON rp.role_id = b.id AND rp.permission = $3::text
    WHERE b.base
) AS e
ORDER BY department_id NULLS FIRST
