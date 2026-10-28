//go:build integration

//nolint:wrapcheck // test code: errors come straight from the fake or store under test; fixtures do bounded arithmetic
package files_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"        //nolint:depguard // roDB must implement db.DB, whose Query returns pgx.Rows
	"github.com/jackc/pgx/v5/pgconn" //nolint:depguard // roDB must implement db.DB, whose Exec returns pgconn.CommandTag

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/objects"
	"github.com/zedgama3/bms/internal/core/objects/objectstest"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// irig is a rig over a real, migrated, throw-away PostgreSQL database (dbtest.TempDB reads
// BMS_TEST_DB_DSN and never prints it).
type irig struct {
	*rig
	pool *db.Pool
}

// newIRig migrates identity, people and files into a fresh database and builds the rig on it.
func newIRig(t *testing.T, opts ...rigOption) *irig {
	t.Helper()
	pool := dbtest.TempDB(t)
	mig, err := db.NewMigrator(pool, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewMigrator: %v", err)
	}
	if _, err := mig.Migrate(ctxT(t), []db.Set{identity.Set, people.Set, files.Set}); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	all := append([]rigOption{withDB(pool)}, opts...)
	return &irig{rig: newRig(t, all...), pool: pool}
}

// person inserts an invented core.person row.
func (r *irig) person() model.PersonID {
	r.t.Helper()
	id := newPerson()
	_, err := r.pool.Exec(ctxT(r.t),
		`INSERT INTO core.person (id, playa_name, legal_name, created_at, updated_at) VALUES ($1, 'Test Person', '', $2, $2)`,
		[16]byte(id), time.Now().UTC())
	if err != nil {
		r.t.Fatalf("seeding a person: %v", err)
	}
	return id
}

func (r *irig) count(sql string, args ...any) int {
	r.t.Helper()
	var n int
	if err := r.pool.QueryRow(ctxT(r.t), sql, args...).Scan(&n); err != nil {
		r.t.Fatalf("count %q: %v", sql, err)
	}
	return n
}

func (r *irig) rows() int       { return r.count(`SELECT count(*) FROM core.file`) }
func (r *irig) tombstones() int { return r.count(`SELECT count(*) FROM core.file_tombstone`) }

// attach binds st to a record in a Write transaction, as an owner's use case would.
func (r *irig) attach(ctx context.Context, store files.Store, st files.Staged, a files.Attachment) (files.File, error) {
	var f files.File
	err := r.pool.Transact(ctx, db.Write, func(ctx context.Context) error {
		var aerr error
		f, aerr = store.Attach(ctx, st, a)
		return aerr
	})
	return f, err
}

// upload is Stage then Attach, the handler's shape without the HTTP.
func (r *irig) upload(ctx context.Context, store files.Store, name string, accept files.Accept, body []byte, a files.Attachment) files.File {
	r.t.Helper()
	st, err := stageBytes(ctx, store, name, accept, body)
	if err != nil {
		r.t.Fatalf("Stage: %v", err)
	}
	f, err := r.attach(ctx, store, st, a)
	if err != nil {
		r.t.Fatalf("Attach: %v", err)
	}
	return f
}

// pdfAttachment is the common case: a PDF about subject on a fresh qualification record.
func (r *irig) uploadPDF(ctx context.Context, store files.Store, rec auditRef, subject model.PersonID, level model.Sensitivity, extra string) files.File {
	r.t.Helper()
	return r.upload(ctx, store, "cert.pdf", files.Documents, pdfDoc(extra), files.Attachment{Record: rec, Subject: subject, Level: level})
}

func (r *irig) del(ctx context.Context, store files.Store, id files.FileID) error {
	return r.pool.Transact(ctx, db.Write, func(ctx context.Context) error { return store.Delete(ctx, id) })
}

func (r *irig) delByRecord(ctx context.Context, store files.Store, rec auditRef) (int, error) {
	var n int
	err := r.pool.Transact(ctx, db.Write, func(ctx context.Context) error {
		var derr error
		n, derr = store.DeleteByRecord(ctx, rec)
		return derr
	})
	return n, err
}

// ---- a pool that behaves like a read-only copy ----

// roDB lets a Write transaction begin (so the code under test reaches its statements) and
// refuses every writing statement the way a replica does: Unavailable wrapping db.ErrReadOnly.
type roDB struct{ db.DB }

func (roDB) ReadOnly() bool { return true }

func isWrite(sql string) bool {
	u := strings.ReplaceAll(strings.ToUpper(sql), "FOR UPDATE", "")
	return strings.Contains(u, "INSERT INTO") || strings.Contains(u, "UPDATE ") || strings.Contains(u, "DELETE FROM")
}

func roErr() error { return errs.E("test.roDB", errs.Unavailable, db.ErrReadOnly) }

func (r roDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if isWrite(sql) {
		return pgconn.CommandTag{}, roErr()
	}
	return r.DB.Exec(ctx, sql, args...) //nolint:wrapcheck // test double
}

func (r roDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if isWrite(sql) {
		return nil, roErr()
	}
	return r.DB.Query(ctx, sql, args...) //nolint:wrapcheck // test double
}

type errRow struct{ err error }

func (e errRow) Scan(...any) error { return e.err }

func (r roDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if isWrite(sql) {
		return errRow{roErr()}
	}
	return r.DB.QueryRow(ctx, sql, args...)
}

// readOnlyCopy builds a second Service over the same database and store, seeing the data
// through roDB. The audit recorder is shared so a test can see spooled entries.
func (r *irig) readOnlyCopy() *files.Service {
	r.t.Helper()
	o := r.opts
	o.DB = roDB{r.pool}
	svc, err := files.New(o)
	if err != nil {
		r.t.Fatalf("files.New (read-only): %v", err)
	}
	return svc
}

// ---- stores under test ----

type storeMaker struct {
	name string
	make func(t *testing.T) objects.Store
}

// stores lists the object stores the lifecycle tests run over: the in-memory store and the
// real disk store on a t.TempDir() (D52(e)). The disk store is built in
// integration_disk_store_test.go, which carries the one depguard exception for it; s3 needs
// a server and is held to the same contract by internal/core/objects.
func stores() []storeMaker {
	return []storeMaker{
		{"mem", func(*testing.T) objects.Store { return objectstest.NewMem() }},
		{"disk", newDiskStore},
	}
}

func eachStore(t *testing.T, fn func(t *testing.T, mk func(t *testing.T) objects.Store)) {
	t.Helper()
	for _, s := range stores() {
		t.Run(s.name, func(t *testing.T) { fn(t, s.make) })
	}
}
