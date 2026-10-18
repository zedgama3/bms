-- Store.CountHolders: the last-access-admin invariant (decision 10), for Revoke, UpdateRole
-- (removing the permission), DeleteRole and DeletePerson. Read-only; use QueryRow. Take
-- lock_invariants.sql first; run it BEFORE the change.
-- Args: $1 permission text ('access.grants.manage'), $2 now timestamptz, then three nullable
-- "what the change removes" filters, any of which may be NULL (pass the ones that apply):
--   $3 grant_id uuid   Revoke: the grant being deleted
--   $4 person_id uuid  DeletePerson: every grant of this person
--   $5 role_id uuid    UpdateRole/DeleteRole: every grant of the role losing the permission
-- Columns, in order: removed integer, remaining integer. Both count active, site-wide grants
-- (department_id IS NULL, not ended, on a site-scoped role) whose role holds $1: removed those the change removes,
-- remaining the others. The change is refused (Conflict last_access_admin) when
-- removed > 0 AND remaining = 0. The superuser holds no grant and is not counted.
SELECT count(*) FILTER (WHERE x.removes)::integer     AS removed,
       count(*) FILTER (WHERE NOT x.removes)::integer AS remaining
FROM (
    SELECT (g.id = $3::uuid OR g.person_id = $4::uuid OR g.role_id = $5::uuid) IS TRUE AS removes
    FROM core.role_permission rp
    JOIN core.role_grant g ON g.role_id = rp.role_id
    JOIN core.role r ON r.id = g.role_id
    WHERE rp.permission = $1::text
      AND g.department_id IS NULL
      AND r.scope = 'site'
      AND (g.ends_at IS NULL OR g.ends_at > $2::timestamptz)
) AS x
