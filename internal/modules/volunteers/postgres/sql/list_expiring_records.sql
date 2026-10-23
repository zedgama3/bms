-- Store.ExpiringRecords: the daily expiry scan's work list (volunteers.expiry-scan). Verified
-- records expiring in [today, today + expiry_notice_days], not yet notified, with no other
-- verified record of the same person and qualification that outlasts them (a renewal). A
-- qualification archived since is still reported (the record still counts until it expires).
-- Use Query. Read-only is fine, but the scan runs on the primary (jobs do not run on a copy).
-- Args: $1 from date (today, UTC), $2 to date (today + N days), $3 limit integer (batch, 500).
-- Columns: id uuid, person_id uuid, qualification_id uuid, qualification_name text,
--   expires_on date.
-- Order: expires_on, id. Served by record_expires_idx (partial: verified_at IS NOT NULL).
SELECT r.id, r.person_id, r.qualification_id, q.name AS qualification_name, r.expires_on
FROM volunteers.qualification_record r
JOIN volunteers.qualification q ON q.id = r.qualification_id
WHERE r.verified_at IS NOT NULL
  AND r.expires_on BETWEEN $1::date AND $2::date
  AND r.expiry_notified_on IS NULL
  AND NOT EXISTS (
        SELECT 1
        FROM volunteers.qualification_record o
        WHERE o.person_id = r.person_id
          AND o.qualification_id = r.qualification_id
          AND o.id <> r.id
          AND o.verified_at IS NOT NULL
          AND (o.expires_on IS NULL OR o.expires_on > r.expires_on))
ORDER BY r.expires_on, r.id
LIMIT $3::integer
