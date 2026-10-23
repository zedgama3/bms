-- Store.DeleteRecord: RejectRecord and RemoveRecord, after the use case read the record FOR
-- UPDATE and checked the permission and, for RejectRecord, that it is pending. The use case
-- then calls files.DeleteByRecord(Ref{Kind: "volunteers.qualification-record", ID: id}) in the
-- same Write transaction. Use QueryRow; zero rows = NotFound (already gone).
-- Args: $1 id uuid.
-- Columns: id uuid, person_id uuid, qualification_id uuid, certificate_file_id uuid NULL.
DELETE FROM volunteers.qualification_record
WHERE id = $1::uuid
RETURNING id, person_id, qualification_id, certificate_file_id
