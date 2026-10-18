-- Store.ListGrants: the grants list, newest first, keyset on (created_at, id) descending, with
-- the caller's extent inside the query (never filtered after fetching). Read-only; use Query.
-- Args:
--   $1  person_id uuid       NULL: any person
--   $2  role_id uuid         NULL: any role
--   $3  department_id uuid   NULL: any department (site-wide rows do not match a given one)
--   $4  permission text      NULL: any; else only grants of roles holding it
--   $5  include_ended boolean  false: only active grants
--   $6  now timestamptz      from clock.Clock
--   $7  whole_site boolean   true when the caller holds access.grants.read site-wide, or is
--                            looking at their own grants (then pass $1 = their person);
--                            false restricts the rows to those whose department is in $8
--   $8  departments uuid[]   the caller's department extent (an empty array, never NULL, when
--                            $7 is true)
--   $9  after_created_at timestamptz and $10 after_id uuid (the Cursor; both NULL on the first page)
--   $11 limit integer        pass Query.Limit + 1; when more rows than Limit come back, Next is
--                            the (created_at, id) of row Limit and the extra row is dropped
-- Columns, in order: id uuid, person_id uuid, role_id uuid, department_id uuid (NULL:
-- site-wide), ends_at timestamptz (NULL: no end), granted_by uuid (NULL), created_at
-- timestamptz. The same as get_grant.sql.
--
-- The cursor is spelled "created_at <= c AND (created_at < c OR id < c_id)", not as a row
-- comparison, as in core/identity's list_accounts.sql: it keeps the index range explicit when
-- the cursor is NULL ('infinity').
SELECT g.id, g.person_id, g.role_id, g.department_id, g.ends_at, g.granted_by, g.created_at
FROM core.role_grant g
WHERE ($1::uuid IS NULL OR g.person_id = $1::uuid)
  AND ($2::uuid IS NULL OR g.role_id = $2::uuid)
  AND ($3::uuid IS NULL OR g.department_id = $3::uuid)
  AND ($4::text IS NULL OR EXISTS (
        SELECT 1
        FROM core.role_permission rp
        WHERE rp.role_id = g.role_id AND rp.permission = $4::text))
  AND ($5::boolean OR g.ends_at IS NULL OR g.ends_at > $6::timestamptz)
  AND ($7::boolean OR g.department_id = ANY($8::uuid[]))
  AND g.created_at <= COALESCE($9::timestamptz, 'infinity'::timestamptz)
  AND (g.created_at < COALESCE($9::timestamptz, 'infinity'::timestamptz)
       OR g.id < COALESCE($10::uuid, 'ffffffff-ffff-ffff-ffff-ffffffffffff'::uuid))
ORDER BY g.created_at DESC, g.id DESC
LIMIT $11::integer
