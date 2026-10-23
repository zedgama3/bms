-- Store.ExpiredRecords: the purge's records leg (volunteers.purge): records whose expires_on is
-- before the retention cutoff. The purge then calls files.DeleteByRecord per record and
-- delete_records.sql for the IDs. Use Query.
-- Args: $1 cutoff date (today minus volunteers.retention), $2 limit integer (batch, 500).
-- Columns: id uuid, certificate_file_id uuid NULL. Order: expires_on, id.
-- Served by record_expires_idx (pending expired records included).
SELECT id, certificate_file_id
FROM volunteers.qualification_record
WHERE expires_on < $1::date
ORDER BY expires_on, id
LIMIT $2::integer
