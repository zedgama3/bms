-- people 0003: playa names are unique across the installation (people.md Amendment P1,
-- decisions 29 to 33, D48). Adds core.person.playa_name_key, the form in which two names are
-- compared, backfills it for existing rows, and puts a partial unique index on it.
--
-- From here on the STORE writes the key (people.NameKey, in Go, with every write of
-- playa_name); SQL never computes it again. The backfill below is the one use of its SQL
-- twin, so a key written by Go and a key backfilled here must agree. They agree on the F82
-- table and on every other Unicode space Go's strings.Fields treats as one (see the class
-- in step 3); the known differences are the case-mapping ones noted there.
--
-- Safe on a database with data: ADD COLUMN with a constant default is a catalog change (no
-- table rewrite), the UPDATE touches only rows that have a name, and the index build takes
-- one short lock on core.person, a table of at most thousands of rows (no CONCURRENTLY: db
-- decision 11). The whole file is one transaction: if any check below raises, nothing
-- applies and the set stays at version 2.

-- 1. normalize() needs a UTF-8 database. Say so here rather than store a wrong key.
DO $$
BEGIN
    IF current_setting('server_encoding') <> 'UTF8' THEN
        RAISE EXCEPTION 'people 0003: the database encoding is %, but playa name keys need UTF8 (normalize())',
            current_setting('server_encoding');
    END IF;
END
$$;

-- 2. The key column. sensitivity: internal, derived from playa_name; never shown, exported
-- or logged (a key is a name).
ALTER TABLE core.person ADD COLUMN playa_name_key text NOT NULL DEFAULT '';
COMMENT ON COLUMN core.person.playa_name_key IS 'sensitivity: internal';

-- 3. Backfill with the SQL twin of people.NameKey: NFKC, each run of whitespace to one
-- space, trim, lower case.
--   * Collapse FIRST, then btrim: btrim() with no second argument removes only U+0020, so
--     the spec's btrim-then-collapse order would leave a leading or trailing tab as a space.
--     After the collapse every edge whitespace character is a single U+0020 and btrim
--     removes it. (Results are the same as the spec's expression on every name that has no
--     edge whitespace other than spaces.)
--   * The class lists the four characters Go's unicode.IsSpace accepts that NFKC leaves
--     alone (U+0085, U+1680, U+2028, U+2029) next to \s, so the result does not depend on
--     the database's LC_CTYPE (\s in a C-locale database knows ASCII only). U+00A0, the
--     U+2000 range, U+202F, U+205F and U+3000 are already U+0020 after NFKC.
--   * lower() follows the database's locale (LC_CTYPE for the libc provider, the ICU locale
--     for the icu provider) where Go's strings.ToLower is plain Unicode mapping: U+00C9 to
--     U+00E9, U+0130 (capital I with dot) to a plain 'i', and a Greek capital sigma to a
--     plain sigma even at the end of a word (so U+0391 U+03A3 gives U+03B1 U+03C3, never the
--     final form U+03C2). They agree for every letter in en_US.UTF-8 and C.UTF-8 databases
--     (libc or builtin provider) and differ in three ways the guard below tests with the
--     characters themselves: a locale that cannot fold non-ASCII at all (the C locale), an ICU
--     locale that maps U+0130 to 'i' plus U+0307 (en-US) and the ICU final-sigma rule (any
--     ICU locale). A database that differs from Go on any of the three is refused when an
--     existing name holds a non-ASCII character, because the backfilled key would not equal
--     the key Go writes for the same name. (A database with only ASCII names is safe whatever
--     its locale; the key of a new name comes from Go.)
DO $$
BEGIN
    IF (lower(E'\u00C9') <> E'\u00E9'
        OR lower(E'\u0130') <> 'i'
        OR lower(E'\u0391\u03A3') <> E'\u03B1\u03C3')
       AND EXISTS (SELECT 1 FROM core.person WHERE octet_length(playa_name) <> char_length(playa_name)) THEN
        RAISE EXCEPTION 'people 0003: this database''s locale (LC_CTYPE %, locale provider % where c is libc, i is icu, b is builtin) lower-cases non-ASCII letters differently from people.NameKey, so playa name keys would differ for existing names; use a UTF-8 locale such as C.UTF-8 or en_US.UTF-8 with the libc provider',
            (SELECT datctype FROM pg_database WHERE datname = current_database()),
            (SELECT datlocprovider::text FROM pg_database WHERE datname = current_database());
    END IF;
END
$$;

UPDATE core.person
SET playa_name_key = lower(btrim(
        regexp_replace(normalize(playa_name, NFKC), '[\s\u0085\u1680\u2028\u2029]+', ' ', 'g')))
WHERE playa_name <> '';

-- 4. Refuse to build the index over a clash. The message carries the count and the person
-- ids, never a name (a name is the key). The installation resolves the clash on version 2,
-- where renames are still allowed, and migrates again.
DO $$
DECLARE
    clashes integer;
    ids     text;
BEGIN
    SELECT count(*), string_agg(g.ids, '; ' ORDER BY g.ids)
    INTO clashes, ids
    FROM (
        SELECT string_agg(id::text, ', ' ORDER BY id) AS ids
        FROM core.person
        WHERE playa_name_key <> ''
        GROUP BY playa_name_key
        HAVING count(*) > 1
    ) g;
    IF clashes > 0 THEN
        RAISE EXCEPTION 'people 0003: % playa name(s) are held by more than one person (person ids, one group per name: %)',
            clashes, ids;
    END IF;
END
$$;

-- 5. The decider. Rows with no name have an empty key and are outside the index, so any
-- number of new accounts may wait to choose a name.
CREATE UNIQUE INDEX person_playa_name_key_idx ON core.person (playa_name_key)
    WHERE playa_name_key <> '';
