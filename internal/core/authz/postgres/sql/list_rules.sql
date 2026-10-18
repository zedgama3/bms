-- Store.ListRules (and CreateRule's read-back): the rules with Affected, sorted by role name.
-- Read-only; use Query. Rules are few, so this is not paginated.
-- Args: $1 now timestamptz; $2 declared_names text[] and $3 declared_reaches text[], two
-- parallel arrays of the same length from the in-memory catalogue (each declared permission
-- and its Reaches: public, internal, personal or sensitive; the order is free); $4 rule_id
-- uuid (NULL: every rule; else just that rule).
-- Columns, in order: id uuid, role_id uuid, permission text (NULL for a level rule), reaches
-- text (NULL for a permission rule), note text, created_at timestamptz, affected integer.
-- Sorted by role name (or key for an unrenamed starter role, byte order), created_at, id.
--
-- affected = the people holding the rule's role now (an active grant) who also hold a
-- forbidden permission now, through any role (an active grant of any role, or the base role),
-- in any extent. Forbidden = the rule's permission, or every declared name reaching at least
-- the rule's level; a name that is not declared has no effect and is not counted. A rule on
-- the base role counts only people with at least one grant: authz does not know the others.
WITH declared AS (
    SELECT d.name,
           CASE d.reaches WHEN 'public' THEN 1 WHEN 'internal' THEN 2
                          WHEN 'personal' THEN 3 WHEN 'sensitive' THEN 4 END AS rank
    FROM unnest($2::text[], $3::text[]) AS d (name, reaches)
),
rules AS (
    SELECT s.id, s.role_id, s.permission, s.reaches, s.note, s.created_at, r.base,
           CASE s.reaches WHEN 'public' THEN 1 WHEN 'internal' THEN 2
                          WHEN 'personal' THEN 3 WHEN 'sensitive' THEN 4 END AS rank,
           lower(coalesce(nullif(r.name, ''), r.key)) AS role_sort
    FROM core.sod_rule s
    JOIN core.role r ON r.id = s.role_id
    WHERE $4::uuid IS NULL OR s.id = $4::uuid
),
forbidden AS (
    SELECT s.id AS rule_id, d.name
    FROM rules s
    JOIN declared d ON d.name = s.permission OR d.rank >= s.rank
),
holders AS (
    SELECT s.id AS rule_id, g.person_id
    FROM rules s
    JOIN core.role_grant g ON g.role_id = s.role_id
    WHERE NOT s.base
      AND (g.ends_at IS NULL OR g.ends_at > $1::timestamptz)
    UNION
    SELECT s.id, g.person_id
    FROM rules s
    CROSS JOIN (
        SELECT DISTINCT person_id
        FROM core.role_grant
        WHERE ends_at IS NULL OR ends_at > $1::timestamptz
    ) AS g
    WHERE s.base
),
forbidden_holders AS (
    SELECT f.rule_id, g.person_id
    FROM forbidden f
    JOIN core.role_permission rp ON rp.permission = f.name
    JOIN core.role_grant g ON g.role_id = rp.role_id
    WHERE g.ends_at IS NULL OR g.ends_at > $1::timestamptz
    UNION
    SELECT h.rule_id, h.person_id
    FROM holders h
    WHERE EXISTS (
        SELECT 1
        FROM forbidden f
        JOIN core.role_permission rp ON rp.permission = f.name
        JOIN core.role b ON b.id = rp.role_id AND b.base
        WHERE f.rule_id = h.rule_id)
),
affected AS (
    SELECT rule_id, count(*)::integer AS n
    FROM (
        SELECT rule_id, person_id FROM holders
        INTERSECT
        SELECT rule_id, person_id FROM forbidden_holders
    ) AS both_sets
    GROUP BY rule_id
)
SELECT s.id, s.role_id, s.permission, s.reaches, s.note, s.created_at,
       coalesce(a.n, 0) AS affected
FROM rules s
LEFT JOIN affected a ON a.rule_id = s.id
ORDER BY s.role_sort COLLATE "C", s.created_at, s.id
