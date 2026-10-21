-- Store.FilesAboutPerson (files.DeletePerson, step 2): which owners still hold a file whose
-- subject is the person. Any row means a module forgot to delete its files before
-- people's DeletePerson reached files: the caller answers Conflict in_use naming the owners
-- and counts ("volunteers: 2"), which rolls back people's whole deletion. Use Query, in
-- people's Write transaction (the deletes of the owners' DeleteByRecord, earlier in it, are
-- visible). Owners and counts only; never a name or an ID.
-- Args: $1 person_id uuid.
-- Columns, in order, sorted by owner: owner text, files bigint. Zero rows: nothing remains.
-- Served by file_subject_idx. Constraint errors: none reachable.
SELECT owner, count(*)::bigint AS files
FROM core.file
WHERE subject_person_id = $1::uuid
GROUP BY owner
ORDER BY owner
