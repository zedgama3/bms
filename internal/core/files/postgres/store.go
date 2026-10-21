// Package postgres is files' storage: the only package with SQL and the driver. It keeps
// metadata only (core.file and core.file_tombstone); no table holds bytes (D43). It speaks
// in the plain rows of files/rows, because files imports this package to build its store
// and so cannot be imported back.
package postgres

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/files/rows"
	"github.com/zedgama3/bms/internal/lib/errs"
)

//go:embed migrations/*.sql
var embedded embed.FS

//go:embed sql/*.sql
var sqlFiles embed.FS

// Migrations is the migration set of files, rooted at the files (db.md: NNNN_name.sql at the
// set root): 0001_file.sql creates core.file and core.file_tombstone. It is
// fs.Sub(embedded, "migrations") resolved in init; that Sub failing on an embedded directory
// is impossible, so init panics (go-conventions).
var Migrations fs.FS

func init() {
	sub, err := fs.Sub(embedded, "migrations")
	if err != nil {
		panic("files/postgres: embedded migrations missing: " + err.Error())
	}
	Migrations = sub
}

// The statements, loaded once. A missing file is impossible: the embed directive fails the
// build.
var (
	sqlInsert         = mustSQL("insert_file.sql")
	sqlGet            = mustSQL("get_file.sql")
	sqlByRecord       = mustSQL("list_files_by_record.sql")
	sqlDelete         = mustSQL("delete_file.sql")
	sqlDeleteByRecord = mustSQL("delete_files_by_record.sql")
	sqlDeleteTomb     = mustSQL("delete_tombstone.sql")
	sqlBumpTomb       = mustSQL("bump_tombstone_attempts.sql")
	sqlTombstones     = mustSQL("list_tombstones.sql")
	sqlCountTombs     = mustSQL("count_tombstones.sql")
	sqlExists         = mustSQL("file_exists_by_key.sql")
	sqlUsage          = mustSQL("file_usage.sql")
	sqlBuckets        = mustSQL("list_buckets.sql")
	sqlKnownKeys      = mustSQL("known_object_keys.sql")
	sqlVerifyPage     = mustSQL("list_files_for_verify.sql")
	sqlExportAbout    = mustSQL("export_files_about_person.sql")
	sqlExportUploaded = mustSQL("export_files_uploaded_by_person.sql")
	sqlAnonymise      = mustSQL("anonymise_uploaded_by.sql")
	sqlAboutPerson    = mustSQL("count_files_about_person.sql")
)

func mustSQL(name string) string {
	b, err := sqlFiles.ReadFile("sql/" + name)
	if err != nil {
		panic("files/postgres: embedded SQL missing: " + name)
	}
	return string(b)
}

// Constraint names a caller maps to its own error (files.md Attach, step 4).
const (
	// ConstraintSubject: the subject person does not exist (23503).
	ConstraintSubject = "file_subject_person_id_fkey"
	// ConstraintStoreKey: the same object attached twice (23505).
	ConstraintStoreKey = "file_store_bucket_key_uidx"
	// ConstraintPrimary: a duplicate file ID (23505).
	ConstraintPrimary = "file_pkey"
)

// ErrUnknownSubject marks an Insert refused because the subject person does not exist
// (errs.Invalid).
var ErrUnknownSubject = errors.New("files: no such subject person")

// ErrDuplicate marks an Insert refused because the object or the ID is already attached
// (errs.Conflict): a Staged used twice.
var ErrDuplicate = errors.New("files: the object is already attached")

// Store keeps core.file and core.file_tombstone in PostgreSQL. It holds a db.DB and no
// state; safe for concurrent use.
type Store struct {
	db db.DB
}

// NewStore returns the store over d. Fails errs.Invalid when d is nil.
func NewStore(d db.DB) (*Store, error) {
	if d == nil {
		return nil, errs.E("files.postgres.NewStore", errs.Invalid, errors.New("nil database"))
	}
	return &Store{db: d}, nil
}

func nilIfZero(id [16]byte) any {
	if id == ([16]byte{}) {
		return nil
	}
	return id
}

func nilIfTimeZero(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}

// fileCols is the scan of the "file columns" every file-returning statement shares.
func scanFile(row pgx.Row, f *rows.File, extra ...any) error {
	var (
		subject, uploaded *[16]byte
		sum               []byte
	)
	dest := append([]any{&f.ID, &f.Owner, &f.RecordKind, &f.RecordID, &subject, &uploaded, &f.Level, &f.Name,
		&f.ContentType, &f.Size, &sum, &f.Store, &f.Bucket, &f.ObjectKey, &f.Stripped, &f.CreatedAt}, extra...)
	if err := row.Scan(dest...); err != nil {
		return err //nolint:wrapcheck // the callers wrap with db.Wrap
	}
	f.Subject, f.UploadedBy = [16]byte{}, [16]byte{}
	if subject != nil {
		f.Subject = *subject
	}
	if uploaded != nil {
		f.UploadedBy = *uploaded
	}
	f.SHA256 = [32]byte{}
	copy(f.SHA256[:], sum)
	f.CreatedAt = f.CreatedAt.UTC()
	return nil
}

// Insert binds a file to its record in the caller's Write transaction. The per-record count
// and the quota are checked in the statement; when a limit stops it the result says which
// and nothing was inserted. Fails errs.Invalid wrapping ErrUnknownSubject for a missing
// subject, errs.Conflict wrapping ErrDuplicate for an object or ID attached twice,
// errs.Unavailable wrapping db.ErrReadOnly on a read-only copy, and db's kinds otherwise.
func (s *Store) Insert(ctx context.Context, f rows.File, maxPerRecord int, quotaBytes int64) (rows.InsertResult, error) {
	const op = "files.postgres.Insert"
	var res rows.InsertResult
	err := s.db.QueryRow(ctx, sqlInsert,
		f.ID, f.Owner, f.RecordKind, f.RecordID, nilIfZero(f.Subject), nilIfZero(f.UploadedBy), f.Level, f.Name,
		f.ContentType, f.Size, f.SHA256[:], f.Store, f.Bucket, f.ObjectKey, f.Stripped, f.CreatedAt.UTC(),
		int32(maxPerRecord), quotaBytes, //nolint:gosec // maxPerRecord is at most files.MaxFilesPerRecordCeiling
	).Scan(&res.Inserted, &res.TooMany, &res.OverQuota)
	if err != nil {
		return rows.InsertResult{}, mapInsertErr(op, err)
	}
	return res, nil
}

func mapInsertErr(op string, err error) error {
	werr := db.Wrap(op, err)
	var dbe *db.Error
	if errors.As(werr, &dbe) {
		switch dbe.Constraint {
		case ConstraintSubject:
			return errs.E(op, errs.Invalid, errors.Join(ErrUnknownSubject, werr))
		case ConstraintStoreKey, ConstraintPrimary:
			return errs.E(op, errs.Conflict, errors.Join(ErrDuplicate, werr))
		}
	}
	return werr
}

// Get answers one file of the owner. Fails errs.NotFound for an unknown ID and for another
// owner's file alike.
func (s *Store) Get(ctx context.Context, owner string, id [16]byte) (rows.File, error) {
	const op = "files.postgres.Get"
	var f rows.File
	if err := scanFile(s.db.QueryRow(ctx, sqlGet, owner, id), &f); err != nil {
		return rows.File{}, db.Wrap(op, err)
	}
	return f, nil
}

// ByRecord lists a record's files of the owner, oldest first, up to limit.
func (s *Store) ByRecord(ctx context.Context, owner, kind string, record [16]byte, limit int) ([]rows.File, error) {
	const op = "files.postgres.ByRecord"
	rs, err := s.db.Query(ctx, sqlByRecord, owner, kind, record, int32(limit)) //nolint:gosec // limit is files.MaxFilesPerRecordCeiling
	if err != nil {
		return nil, db.Wrap(op, err)
	}
	return collectFiles(op, rs)
}

func collectFiles(op string, rs pgx.Rows) ([]rows.File, error) {
	defer rs.Close()
	out := []rows.File{}
	for rs.Next() {
		var f rows.File
		if err := scanFile(rs, &f); err != nil {
			return nil, db.Wrap(op, err)
		}
		out = append(out, f)
	}
	if err := rs.Err(); err != nil {
		return nil, db.Wrap(op, err)
	}
	return out, nil
}

func collectDeleted(op string, rs pgx.Rows) ([]rows.Deleted, error) {
	defer rs.Close()
	out := []rows.Deleted{}
	for rs.Next() {
		var d rows.Deleted
		if err := scanFile(rs, &d.File, &d.TombstoneID); err != nil {
			return nil, db.Wrap(op, err)
		}
		out = append(out, d)
	}
	if err := rs.Err(); err != nil {
		return nil, db.Wrap(op, err)
	}
	return out, nil
}

// Delete removes one of the owner's files and writes its tombstone in one statement, inside
// the caller's Write transaction. Fails errs.NotFound when the ID is unknown or another
// owner's (nothing was written).
func (s *Store) Delete(ctx context.Context, owner string, id [16]byte, now time.Time) (rows.Deleted, error) {
	const op = "files.postgres.Delete"
	rs, err := s.db.Query(ctx, sqlDelete, owner, id, now.UTC())
	if err != nil {
		return rows.Deleted{}, db.Wrap(op, err)
	}
	ds, err := collectDeleted(op, rs)
	if err != nil {
		return rows.Deleted{}, err
	}
	if len(ds) == 0 {
		return rows.Deleted{}, errs.E(op, errs.NotFound, errors.New("no such file"))
	}
	return ds[0], nil
}

// DeleteByRecord removes every one of the owner's files of a record and writes their
// tombstones, oldest first. Zero rows is not an error.
func (s *Store) DeleteByRecord(ctx context.Context, owner, kind string, record [16]byte, now time.Time) ([]rows.Deleted, error) {
	const op = "files.postgres.DeleteByRecord"
	rs, err := s.db.Query(ctx, sqlDeleteByRecord, owner, kind, record, now.UTC())
	if err != nil {
		return nil, db.Wrap(op, err)
	}
	return collectDeleted(op, rs)
}

// DeleteTombstone removes a tombstone once its object is confirmed gone; idempotent.
func (s *Store) DeleteTombstone(ctx context.Context, id [16]byte) error {
	if _, err := s.db.Exec(ctx, sqlDeleteTomb, id); err != nil {
		return db.Wrap("files.postgres.DeleteTombstone", err)
	}
	return nil
}

// BumpTombstone counts a failed delete attempt.
func (s *Store) BumpTombstone(ctx context.Context, id [16]byte) error {
	if _, err := s.db.Exec(ctx, sqlBumpTomb, id); err != nil {
		return db.Wrap("files.postgres.BumpTombstone", err)
	}
	return nil
}

// Tombstones lists the outstanding tombstones of one store, oldest first, strictly after
// (afterCreated, afterID); a zero afterCreated starts from the beginning.
func (s *Store) Tombstones(ctx context.Context, store string, afterCreated time.Time, afterID [16]byte, limit int) ([]rows.Tombstone, error) {
	const op = "files.postgres.Tombstones"
	rs, err := s.db.Query(ctx, sqlTombstones, store, nilIfTimeZero(afterCreated), nilIfZero(afterID), int32(limit)) //nolint:gosec // limit is a small constant
	if err != nil {
		return nil, db.Wrap(op, err)
	}
	defer rs.Close()
	out := []rows.Tombstone{}
	for rs.Next() {
		var t rows.Tombstone
		var attempts int32
		if err := rs.Scan(&t.ID, &t.Store, &t.Bucket, &t.ObjectKey, &t.CreatedAt, &attempts); err != nil {
			return nil, db.Wrap(op, err)
		}
		t.CreatedAt, t.Attempts = t.CreatedAt.UTC(), int(attempts)
		out = append(out, t)
	}
	if err := rs.Err(); err != nil {
		return nil, db.Wrap(op, err)
	}
	return out, nil
}

// CountTombstones answers how many tombstones are outstanding and how many of them have been
// tried at least stuckAttempts times.
func (s *Store) CountTombstones(ctx context.Context, stuckAttempts int) (outstanding, stuck int64, err error) {
	const op = "files.postgres.CountTombstones"
	if err := s.db.QueryRow(ctx, sqlCountTombs, int32(stuckAttempts)).Scan(&outstanding, &stuck); err != nil { //nolint:gosec // a small constant
		if werr := db.Wrap(op, err); !errs.Is(werr, errs.NotFound) {
			return 0, 0, werr
		}
		// An aggregate always yields a row; "no row" is a database that holds none.
		return 0, 0, nil
	}
	return outstanding, stuck, nil
}

// Attached reports whether a row refers to the object.
func (s *Store) Attached(ctx context.Context, store, bucket, key string) (bool, error) {
	const op = "files.postgres.Attached"
	var attached bool
	if err := s.db.QueryRow(ctx, sqlExists, store, bucket, key).Scan(&attached); err != nil {
		return false, db.Wrap(op, err)
	}
	return attached, nil
}

// Usage answers how many files are stored and their total size.
func (s *Store) Usage(ctx context.Context) (files, bytes int64, err error) {
	const op = "files.postgres.Usage"
	if err := s.db.QueryRow(ctx, sqlUsage).Scan(&files, &bytes); err != nil {
		if werr := db.Wrap(op, err); !errs.Is(werr, errs.NotFound) {
			return 0, 0, werr
		}
		return 0, 0, nil // an aggregate always yields a row; "no row" is a database that holds none
	}
	return files, bytes, nil
}

// Buckets lists the buckets that hold or recently held objects in a store.
func (s *Store) Buckets(ctx context.Context, store string) ([]string, error) {
	const op = "files.postgres.Buckets"
	rs, err := s.db.Query(ctx, sqlBuckets, store)
	if err != nil {
		return nil, db.Wrap(op, err)
	}
	return collectStrings(op, rs)
}

// UnknownKeys answers which of keys (at most 1000) are neither a row nor a tombstone, in the
// order given.
func (s *Store) UnknownKeys(ctx context.Context, store, bucket string, keys []string) ([]string, error) {
	const op = "files.postgres.UnknownKeys"
	rs, err := s.db.Query(ctx, sqlKnownKeys, store, bucket, keys)
	if err != nil {
		return nil, db.Wrap(op, err)
	}
	known, err := collectStrings(op, rs)
	if err != nil {
		return nil, err
	}
	have := make(map[string]struct{}, len(known))
	for _, k := range known {
		have[k] = struct{}{}
	}
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if _, ok := have[k]; !ok {
			out = append(out, k)
		}
	}
	return out, nil
}

func collectStrings(op string, rs pgx.Rows) ([]string, error) {
	defer rs.Close()
	out := []string{}
	for rs.Next() {
		var v string
		if err := rs.Scan(&v); err != nil {
			return nil, db.Wrap(op, err)
		}
		out = append(out, v)
	}
	if err := rs.Err(); err != nil {
		return nil, db.Wrap(op, err)
	}
	return out, nil
}

// VerifyPage lists up to limit rows with an ID after afterID (zero: from the start).
func (s *Store) VerifyPage(ctx context.Context, afterID [16]byte, limit int) ([]rows.VerifyRow, error) {
	const op = "files.postgres.VerifyPage"
	rs, err := s.db.Query(ctx, sqlVerifyPage, nilIfZero(afterID), int32(limit)) //nolint:gosec // limit is a small constant
	if err != nil {
		return nil, db.Wrap(op, err)
	}
	defer rs.Close()
	out := []rows.VerifyRow{}
	for rs.Next() {
		var v rows.VerifyRow
		var sum []byte
		if err := rs.Scan(&v.ID, &v.Store, &v.Bucket, &v.ObjectKey, &v.Size, &sum); err != nil {
			return nil, db.Wrap(op, err)
		}
		copy(v.SHA256[:], sum)
		out = append(out, v)
	}
	if err := rs.Err(); err != nil {
		return nil, db.Wrap(op, err)
	}
	return out, nil
}

// ExportAbout lists files whose subject is person, oldest first, strictly after
// (afterCreated, afterID).
func (s *Store) ExportAbout(ctx context.Context, person [16]byte, afterCreated time.Time, afterID [16]byte, limit int) ([]rows.File, error) {
	const op = "files.postgres.ExportAbout"
	rs, err := s.db.Query(ctx, sqlExportAbout, person, nilIfTimeZero(afterCreated), nilIfZero(afterID), int32(limit)) //nolint:gosec // a small constant
	if err != nil {
		return nil, db.Wrap(op, err)
	}
	return collectFiles(op, rs)
}

// ExportUploaded lists files person uploaded whose subject is someone else or nobody.
func (s *Store) ExportUploaded(ctx context.Context, person [16]byte, afterCreated time.Time, afterID [16]byte, limit int) ([]rows.File, error) {
	const op = "files.postgres.ExportUploaded"
	rs, err := s.db.Query(ctx, sqlExportUploaded, person, nilIfTimeZero(afterCreated), nilIfZero(afterID), int32(limit)) //nolint:gosec // a small constant
	if err != nil {
		return nil, db.Wrap(op, err)
	}
	return collectFiles(op, rs)
}

// AnonymiseUploads clears uploaded_by on every file person uploaded and counts them.
func (s *Store) AnonymiseUploads(ctx context.Context, person [16]byte) (int, error) {
	const op = "files.postgres.AnonymiseUploads"
	var n int64
	if err := s.db.QueryRow(ctx, sqlAnonymise, person).Scan(&n); err != nil {
		return 0, db.Wrap(op, err)
	}
	return int(n), nil
}

// FilesAboutPerson counts, per owner, the files whose subject is person.
func (s *Store) FilesAboutPerson(ctx context.Context, person [16]byte) ([]rows.OwnerCount, error) {
	const op = "files.postgres.FilesAboutPerson"
	rs, err := s.db.Query(ctx, sqlAboutPerson, person)
	if err != nil {
		return nil, db.Wrap(op, err)
	}
	defer rs.Close()
	out := []rows.OwnerCount{}
	for rs.Next() {
		var c rows.OwnerCount
		if err := rs.Scan(&c.Owner, &c.Files); err != nil {
			return nil, db.Wrap(op, err)
		}
		out = append(out, c)
	}
	if err := rs.Err(); err != nil {
		return nil, db.Wrap(op, err)
	}
	return out, nil
}
