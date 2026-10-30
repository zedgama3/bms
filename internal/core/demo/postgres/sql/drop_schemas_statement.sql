-- Store.DropSchemas, first half: build the one DROP statement of Reset step 3 on the server, so
-- that identifiers are quoted by quote_ident and never concatenated in Go. DDL cannot take
-- parameters, so the store runs this query, checks `matched`, and then Exec's the returned
-- statement (a Write, outside any other transaction; under migrateTimeout). Read-only; use
-- QueryRow.
-- Args: $1 schemas text[] (demo.Options.Schemas: every migration set's schema, deduplicated,
-- from app's plan, never from a request).
-- Columns, in order: statement text, matched bigint (scan into int64). statement is
-- 'DROP SCHEMA IF EXISTS "a", "b" CASCADE' over the names that match db's schema grammar
-- ([a-z][a-z0-9]{1,31}) and are not 'public' (never the database's default schema); it is NULL when none matches. The caller fails Internal unless
-- matched equals the number of names it passed (a name outside the grammar, or 'public', is refused, not
-- dropped), and unless the statement is non-NULL.
-- Fails nothing at execution. The returned statement fails with a privilege error (42501,
-- classified by db.Wrap) when the role may not drop a schema: F23.
SELECT 'DROP SCHEMA IF EXISTS ' || string_agg(quote_ident(s), ', ' ORDER BY s) || ' CASCADE' AS statement,
       count(*) AS matched
FROM (SELECT DISTINCT s FROM unnest($1::text[]) AS u(s) WHERE s ~ '^[a-z][a-z0-9]{1,31}$' AND s <> 'public') AS ok
