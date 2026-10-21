-- Store.AnonymiseUploads (files.DeletePerson, step 1): clear uploaded_by on every file the
-- person uploaded, whatever its owner, so the foreign key to core.person no longer holds the
-- person. The files stay (they belong to their records). Runs in people's Write transaction,
-- before core.person is deleted, and BEFORE count_files_about_person.sql. Use QueryRow.
-- Args: $1 person_id uuid.
-- Columns, in order: anonymized bigint (scan into int64) = Deletion.Anonymized.
-- Served by file_uploaded_by_idx. Fails 25006 (db.ErrReadOnly) on a read-only copy.
-- Constraint errors: none reachable.
WITH cleared AS (
    UPDATE core.file
    SET uploaded_by = NULL
    WHERE uploaded_by = $1::uuid
    RETURNING 1
)
SELECT count(*) AS anonymized FROM cleared
