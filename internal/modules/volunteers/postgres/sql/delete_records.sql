-- Store.DeleteRecords: the purge, after files.DeleteByRecord for each. Exec; the count is
-- CommandTag.RowsAffected().
-- Args: $1 ids uuid[] (at most the batch size from list_expired_records.sql).
DELETE FROM volunteers.qualification_record
WHERE id = ANY($1::uuid[])
