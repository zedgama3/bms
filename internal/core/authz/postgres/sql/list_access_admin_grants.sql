-- Store.AdminGrants: the last-access-admin invariant (decision 10), for Revoke, UpdateRole
-- (removing the permission), DeleteRole, DeletePerson and CreateRule. Read-only; use Query.
-- Take lock_invariants.sql first; run it BEFORE the change (CreateRule: before and after).
-- Args: $1 permission text ('access.grants.manage'), $2 now timestamptz, then three nullable
-- "what the change removes" filters, any of which may be NULL (pass the ones that apply):
--   $3 grant_id uuid   Revoke: the grant being deleted
--   $4 person_id uuid  DeletePerson: every grant of this person
--   $5 role_id uuid    UpdateRole/DeleteRole: every grant of the role losing the permission
-- Columns, in order: person_id uuid, removes boolean. One row per active, site-wide grant
-- (department_id IS NULL, not ended, on a site-scoped role) whose role holds $1; removes is
-- true for the grants the change removes. The service drops the rows of people a
-- separation-of-duties rule denies $1 (the rules are worked out in Go from the catalogue),
-- then refuses the change (Conflict last_access_admin) when some removed row is left and no
-- other row is. The superuser holds no grant and is not counted.
SELECT g.person_id,
       (g.id = $3::uuid OR g.person_id = $4::uuid OR g.role_id = $5::uuid) IS TRUE AS removes
FROM core.role_permission rp
JOIN core.role_grant g ON g.role_id = rp.role_id
JOIN core.role r ON r.id = g.role_id
WHERE rp.permission = $1::text
  AND g.department_id IS NULL
  AND r.scope = 'site'
  AND (g.ends_at IS NULL OR g.ends_at > $2::timestamptz)
