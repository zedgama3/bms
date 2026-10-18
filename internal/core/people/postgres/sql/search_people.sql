-- Store.Search: people.Search. Read-only; use Query. Keyset paging, stable order.
-- Args:
--   $1 q text               the trimmed query (Go enforces MinSearchBytes..MaxSearchBytes);
--                           matched case-insensitively, as a plain substring (no LIKE
--                           metacharacters: strpos, so "%" and "_" are ordinary characters)
--   $2 match_legal_name boolean    substring of legal_name; pass true only when the viewer
--   $3 match_phone boolean         holds people.contacts.read (any extent) and the field's
--   $4 match_contact_email boolean mode is not hidden. phone: digits only, and only when q
--                                  is made of phone characters (digits, space, + ( ) . -)
--                                  holding at least two digits. contact_email: exact, ignoring
--                                  case, never a substring.
--   $5 after_key text, $6 after_id uuid   the cursor (both NULL on the first page): the
--                           sort_key and id of the last row of the previous page
--   $7 limit integer        pass Limit + 1; when more rows than Limit come back, the cursor
--                           is the (sort_key, id) of row Limit and the extra row is dropped
-- Columns, in order: id uuid, playa_name text, pronouns text, matched_legal_name boolean,
-- matched_phone boolean, matched_contact_email boolean, sort_key text.
-- sort_key is lower(playa_name) in byte order: the cursor's first half. The matched_* flags
-- say which PERSONAL field matched, so the use case records people.search.read (Fields
-- legal_name, phone, contact_email) per result; they are always false for a viewer without
-- the permission, and a person who matched on playa name only has all three false.
--
-- No leak by ordering or by timing. The order is (lower(playa_name), id) and nothing else;
-- the personal columns are never in ORDER BY or in the cursor. Each personal arm sits in a
-- CASE WHEN <flag>, which PostgreSQL evaluates in order: with the flag false the legal_name,
-- phone and contact_email columns are never read, so a viewer without the permission runs
-- exactly the playa-name query whatever the personal data holds. (With literal flags the
-- arms are removed at plan time; with a generic plan the CASE still short-circuits.)
SELECT p.id, p.playa_name, p.pronouns,
       CASE WHEN $2::boolean THEN strpos(lower(p.legal_name), n.q) > 0 ELSE false END
           AS matched_legal_name,
       CASE WHEN $3::boolean THEN n.digits <> '' AND strpos(regexp_replace(p.phone, '[^0-9]', '', 'g'), n.digits) > 0
            ELSE false END AS matched_phone,
       CASE WHEN $4::boolean THEN COALESCE(lower(p.contact_email) = n.q, false) ELSE false END
           AS matched_contact_email,
       lower(p.playa_name) COLLATE "C" AS sort_key
FROM core.person p
CROSS JOIN (
    SELECT lower($1::text) AS q,
           CASE WHEN $1::text ~ '^[0-9 ()+.-]+$' AND length(regexp_replace($1::text, '[^0-9]', '', 'g')) >= 2
                THEN regexp_replace($1::text, '[^0-9]', '', 'g') ELSE '' END AS digits
) n
WHERE n.q <> ''
  AND (strpos(lower(p.playa_name), n.q) > 0
       OR CASE WHEN $2::boolean THEN strpos(lower(p.legal_name), n.q) > 0 ELSE false END
       OR CASE WHEN $3::boolean THEN n.digits <> '' AND strpos(regexp_replace(p.phone, '[^0-9]', '', 'g'), n.digits) > 0
               ELSE false END
       OR CASE WHEN $4::boolean
               THEN p.id IN (SELECT c.id FROM core.person c WHERE lower(c.contact_email) = lower($1::text))
               ELSE false END)
  AND lower(p.playa_name) COLLATE "C" >= COALESCE($5::text, '') COLLATE "C"
  AND (lower(p.playa_name) COLLATE "C" > COALESCE($5::text, '') COLLATE "C"
       OR p.id > COALESCE($6::uuid, '00000000-0000-0000-0000-000000000000'::uuid))
ORDER BY lower(p.playa_name) COLLATE "C", p.id
LIMIT $7::integer
