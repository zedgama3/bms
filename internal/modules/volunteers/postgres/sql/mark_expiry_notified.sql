-- Store.MarkExpiryNotified: the expiry scan, after notify accepted the message, in its own
-- small transaction. Exec; RowsAffected 0 = the record is gone (not an error). updated_at is
-- left alone: this is bookkeeping, not a change to the record.
-- Args: $1 id uuid, $2 on date (today, UTC).
UPDATE volunteers.qualification_record
SET expiry_notified_on = $2::date
WHERE id = $1::uuid
  AND expiry_notified_on IS NULL
