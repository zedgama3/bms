-- Store.TitlesOf: audit's actor titles. One statement; read-only; may run inside the caller's
-- Write transaction. Use Query.
-- Args: $1 person_id uuid, $2 now timestamptz, $3 limit integer (audit.MaxTitles, 16).
-- Columns, in order: title text. The key of each non-base role the person holds now, or its
-- name when it has been renamed; distinct, sorted in byte order, at most $3. Zero rows: none.
SELECT title
FROM (
    SELECT DISTINCT coalesce(nullif(r.name, ''), r.key) AS title
    FROM core.role_grant g
    JOIN core.role r ON r.id = g.role_id
    WHERE g.person_id = $1
      AND (g.ends_at IS NULL OR g.ends_at > $2::timestamptz)
      AND NOT r.base
) AS t
ORDER BY title COLLATE "C"
LIMIT $3::integer
