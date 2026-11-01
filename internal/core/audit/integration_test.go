//go:build integration

package audit_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn" //nolint:depguard // roDB must implement db.DB, whose Exec returns pgconn.CommandTag

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

var epochMicro = time.Date(2026, 10, 17, 12, 0, 0, 123456000, time.UTC) // PostgreSQL keeps microseconds

// ifx is a Log over a real, migrated, throw-away database.
type ifx struct {
	log    *audit.Log
	opts   audit.Options
	pool   *db.Pool
	clk    *clock.Fake
	dir    string
	cap    *telemetrytest.Capture
	titles *fakeTitles
}

func newIfx(t *testing.T, wrap ...func(*db.Pool) db.DB) *ifx {
	t.Helper()
	pool := dbtest.TempDB(t)
	mig, err := db.NewMigrator(pool, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewMigrator: %v", err)
	}
	if _, err := mig.Migrate(ctxT(t), []db.Set{audit.Set}); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	f := &ifx{
		pool:   pool,
		clk:    clock.NewFake(epochMicro),
		dir:    t.TempDir(),
		cap:    telemetrytest.New(t),
		titles: &fakeTitles{titles: map[model.PersonID][]string{}},
	}
	f.opts = audit.Options{
		DB: pool, Clock: f.clk, Logger: f.cap.Logger(), Tracer: f.cap.Tracer("audit"), Meter: f.cap.Meter("audit"),
		Instance: "test-copy", DataDir: f.dir, Actor: sessionActor, Titles: f.titles,
	}
	for _, w := range wrap {
		f.opts.DB = w(pool)
	}
	f.log = f.build(t, f.opts)
	return f
}

func (f *ifx) build(t *testing.T, o audit.Options) *audit.Log {
	t.Helper()
	l, err := audit.New(o)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()
		_ = l.Close(ctx) //nolint:errcheck // idempotent cleanup
	})
	return l
}

func (f *ifx) scoped(t *testing.T, area string) audit.Recorder {
	t.Helper()
	r, err := f.log.Scoped(area)
	wantNoErr(t, err)
	return r
}

func (f *ifx) count(t *testing.T, where string, args ...any) int {
	t.Helper()
	var n int
	q := "SELECT count(*) FROM core.audit_entry"
	if where != "" {
		q += " WHERE " + where
	}
	if err := f.pool.QueryRow(ctxT(t), q, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func (f *ifx) spoolDir() string { return filepath.Join(f.dir, filepath.FromSlash(audit.SpoolDirName)) }

// storedRecords reads the identity-area rows straight from the table, newest first: audit
// has no read path any more (auditlog owns it), so these tests look at the table itself.
func (f *ifx) storedRecords(t *testing.T, area string) []audit.Record {
	t.Helper()
	rows, err := f.pool.Query(ctxT(t), `SELECT at, instance, coalesce(trace_id, ''), spooled, action, outcome, actor_kind,
		coalesce(actor_person_id::text, ''), actor_superuser, actor_titles, coalesce(subject_person_id::text, ''),
		coalesce(record_kind, ''), coalesce(record_id::text, ''), fields, coalesce(level, ''), coalesce(reason, '')
		FROM core.audit_entry WHERE action LIKE $1 ORDER BY at DESC, id DESC`, area+".%")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	var out []audit.Record
	for rows.Next() {
		var r audit.Record
		var action, outcome, kind, actorID, subjectID, recKind, recID, level string
		if err := rows.Scan(&r.At, &r.Instance, &r.TraceID, &r.Spooled, &action, &outcome, &kind,
			&actorID, &r.Actor.Superuser, &r.Actor.Titles, &subjectID, &recKind, &recID, &r.Fields, &level, &r.Reason); err != nil {
			t.Fatalf("scan: %v", err)
		}
		r.Action = audit.Action(action)
		r.ID = model.NewID[audit.RecordID]() // the id is not compared; non-zero for the "found" check
		o, perr := audit.ParseOutcome(outcome)
		wantNoErr(t, perr)
		r.Outcome = o
		switch kind {
		case "person":
			r.Actor.Kind = audit.ActorPerson
		case "anonymous":
			r.Actor.Kind = audit.ActorAnonymous
		case "operator":
			r.Actor.Kind = audit.ActorOperator
		case "system":
			r.Actor.Kind = audit.ActorSystem
		}
		if actorID != "" {
			p, perr := model.ParseID[model.PersonID](actorID)
			wantNoErr(t, perr)
			r.Actor.Person = p
		}
		if subjectID != "" {
			p, perr := model.ParseID[model.PersonID](subjectID)
			wantNoErr(t, perr)
			r.Subject = p
		}
		if recKind != "" {
			id, perr := model.ParseID[model.AccountID](recID)
			wantNoErr(t, perr)
			r.Record = audit.RefOf(recKind, id)
		}
		r.Level = model.Sensitivity(0)
		switch level {
		case "personal":
			r.Level = model.SensitivityPersonal
		case "sensitive":
			r.Level = model.SensitivitySensitive
		}
		out = append(out, r)
	}
	wantNoErr(t, rows.Err())
	return out
}

// roDB makes a pool look like a read-only copy: writes are refused as db does.
type roDB struct{ db.DB }

func (roDB) ReadOnly() bool { return true }
func (roDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errs.E("test.roDB.Exec", errs.Unavailable, db.ErrReadOnly)
}
func (r roDB) Transact(ctx context.Context, m db.Mode, fn func(context.Context) error) error {
	if m == db.Write {
		return errs.E("test.roDB.Transact", errs.Unavailable, db.ErrReadOnly)
	}
	return r.DB.Transact(ctx, m, fn) //nolint:wrapcheck // test double
}

func TestMigrations_VersionAndSet(t *testing.T) {
	v, err := db.Version(audit.Migrations)
	if err != nil || v < 1 {
		t.Fatalf("Version = %d, %v", v, err)
	}
	if audit.Set.Name != "audit" || audit.Set.Schema != "core" {
		t.Errorf("Set = %+v", audit.Set)
	}
}

func TestRecord_StoresStampedAndResolvedEntry(t *testing.T) {
	f := newIfx(t)
	f.titles.titles[carol] = []string{"Audit reviewer", "Medic"}
	ctx, span := f.opts.Tracer.Start(withSession(ctxT(t), superuser(carol)), "test.request")
	defer span.End()
	e := audit.Entry{
		Action: "identity.account.read", Outcome: audit.Denied, Subject: bob,
		Record: audit.RefOf("identity.account", model.AccountID{4}),
		Fields: []string{"email", "last_login_at"}, Level: model.SensitivitySensitive,
		Reason: "lost child at the gate",
	}
	wantNoErr(t, f.scoped(t, "identity").Record(ctx, e, loginEntry()))

	records := f.storedRecords(t, "identity")
	if len(records) != 2 {
		t.Fatalf("records = %d, want 2", len(records))
	}
	var got audit.Record
	for _, r := range records {
		if r.Action == "identity.account.read" {
			got = r
		}
	}
	if got.ID.IsZero() {
		t.Fatal("record not found")
	}
	if !got.At.Equal(epochMicro) {
		t.Errorf("At = %v, want %v", got.At, epochMicro)
	}
	if got.Instance != "test-copy" || got.Spooled {
		t.Errorf("Instance %q Spooled %v", got.Instance, got.Spooled)
	}
	if len(got.TraceID) != 32 {
		t.Errorf("TraceID = %q", got.TraceID)
	}
	if got.Outcome != audit.Denied || got.Subject != bob || got.Level != model.SensitivitySensitive || got.Reason != e.Reason {
		t.Errorf("record = %+v", got)
	}
	if got.Record != e.Record {
		t.Errorf("Record ref = %+v", got.Record)
	}
	if strings.Join(got.Fields, ",") != "email,last_login_at" {
		t.Errorf("Fields = %v", got.Fields)
	}
	a := got.Actor
	if a.Kind != audit.ActorPerson || a.Person != carol || !a.Superuser || strings.Join(a.Titles, "|") != "Audit reviewer|Medic" {
		t.Errorf("Actor = %+v", a)
	}
	for _, r := range records {
		if r.Actor.Kind == 0 {
			t.Error("a returned Actor has a zero Kind")
		}
		if !r.At.Equal(epochMicro) {
			t.Errorf("batch entries must share one instant, got %v", r.At)
		}
	}
	if n := f.count(t, "actor_superuser AND actor_person_id = $1::uuid", carol.String()); n != 2 {
		t.Errorf("superuser-marked rows = %d, want 2", n)
	}
}

func TestRecord_InsideWriteTx_CommitsAndRollsBackWithTheCaller(t *testing.T) {
	f := newIfx(t)
	rec := f.scoped(t, "identity")
	boom := errors.New("caller failed")
	err := f.pool.Transact(ctxT(t), db.Write, func(ctx context.Context) error {
		if err := rec.Record(ctx, loginEntry(), loginEntry()); err != nil {
			return err //nolint:wrapcheck // test
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Transact returned a different error (kind %v)", errs.KindOf(err))
	}
	if n := f.count(t, ""); n != 0 {
		t.Fatalf("rows after rollback = %d, want 0", n)
	}
	err = f.pool.Transact(ctxT(t), db.Write, func(ctx context.Context) error {
		return rec.Record(ctx, loginEntry(), loginEntry())
	})
	wantNoErr(t, err)
	if n := f.count(t, ""); n != 2 {
		t.Fatalf("rows after commit = %d, want 2", n)
	}
}

func TestRecord_InsideWriteTx_InsertRefused_ReturnsErrReadOnly_NotSpooled(t *testing.T) {
	f := newIfx(t)
	rec := f.scoped(t, "identity")
	err := f.pool.Transact(ctxT(t), db.Write, func(ctx context.Context) error {
		if _, err := f.pool.Exec(ctx, "SET LOCAL transaction_read_only = on"); err != nil {
			return err //nolint:wrapcheck // test
		}
		return rec.Record(ctx, loginEntry())
	})
	wantKind(t, err, errs.Unavailable)
	if !errors.Is(err, db.ErrReadOnly) {
		t.Errorf("err does not wrap db.ErrReadOnly (kind %v)", errs.KindOf(err))
	}
	if n := f.count(t, ""); n != 0 {
		t.Errorf("rows = %d, want 0", n)
	}
	if entries, derr := os.ReadDir(f.spoolDir()); derr == nil && len(entries) != 0 {
		t.Errorf("spool files = %d, want none", len(entries))
	}
}

func TestRecord_InsideReadTx_ReturnsInternal(t *testing.T) {
	f := newIfx(t)
	rec := f.scoped(t, "identity")
	err := f.pool.Transact(ctxT(t), db.Read, func(ctx context.Context) error {
		return rec.Record(ctx, loginEntry())
	})
	wantKind(t, err, errs.Internal)
	if n := f.count(t, ""); n != 0 {
		t.Errorf("rows = %d", n)
	}
}

func TestRecord_DBDown_ReturnsUnavailable(t *testing.T) {
	f := newIfx(t)
	rec := f.scoped(t, "identity")
	if err := f.pool.Close(ctxT(t)); err != nil {
		t.Fatalf("close pool: %v", err)
	}
	err := rec.Record(ctxT(t), loginEntry())
	wantKind(t, err, errs.Unavailable)
	if entries, err := os.ReadDir(f.spoolDir()); err == nil && len(entries) != 0 {
		t.Error("a database failure was spooled")
	}
}

func TestRecord_Concurrent32_AllStored(t *testing.T) {
	f := newIfx(t)
	rec := f.scoped(t, "identity")
	runConcurrent(t, 32, func() error { return rec.Record(t.Context(), loginEntry(), readEntry(alice)) })
	if n := f.count(t, ""); n != 64 {
		t.Errorf("rows = %d, want 64", n)
	}
}

// --- the spool, end to end ----------------------------------------------------------------

func TestSpool_ReadOnlyCopyThenImport_StoresOnceWithOriginalTime(t *testing.T) {
	f := newIfx(t, func(p *db.Pool) db.DB { return roDB{DB: p} })
	f.titles.titles[alice] = []string{"Gate lead"}
	rec := f.scoped(t, "identity")
	wantNoErr(t, rec.Record(withSession(ctxT(t), person(alice)), readEntry(bob), loginEntry()))
	if n := f.count(t, ""); n != 0 {
		t.Fatalf("a read-only copy wrote %d rows", n)
	}
	if _, err := f.log.ImportOnce(ctxT(t)); err == nil {
		t.Fatal("import on a read-only copy succeeded")
	}
	wantNoErr(t, f.log.Close(ctxT(t)))

	// The writing copy (same data dir, say after promotion) imports the closed file.
	o := f.opts
	o.DB = f.pool
	o.Clock = clock.NewFake(epochMicro.Add(24 * time.Hour))
	w := f.build(t, o)
	got, err := w.ImportOnce(ctxT(t))
	wantNoErr(t, err)
	if got.Files != 1 || got.Entries != 2 || got.Duplicates != 0 || len(got.Failed) != 0 {
		t.Fatalf("Imported = %+v, want 1 file, 2 entries", got)
	}
	if n := f.count(t, "spooled"); n != 2 {
		t.Errorf("spooled rows = %d, want 2", n)
	}
	var at time.Time
	var created time.Time
	if err := f.pool.QueryRow(ctxT(t), "SELECT at, created_at FROM core.audit_entry LIMIT 1").Scan(&at, &created); err != nil {
		t.Fatal(err)
	}
	if !at.Equal(epochMicro) {
		t.Errorf("at = %v, want the original %v", at, epochMicro)
	}
	if !created.After(at) {
		t.Error("created_at must be the insert time, after the original at")
	}
	records := (&ifx{pool: f.pool}).storedRecords(t, "identity")
	for _, r := range records {
		if !r.Spooled || r.Actor.Person != alice || strings.Join(r.Actor.Titles, "") != "Gate lead" {
			t.Errorf("record = %+v", r)
		}
	}
	if left := globSorted(t, filepath.Join(f.spoolDir(), "*")); len(left) != 0 {
		t.Errorf("spool not empty after import: %v", left)
	}
}

func TestImportOnce_SameFileTwice_IsIdempotent(t *testing.T) {
	f := newIfx(t)
	content := ""
	ids := make([]audit.RecordID, 0)
	for range 5 {
		id := model.NewID[audit.RecordID]()
		ids = append(ids, id)
		content += fmt.Sprintf(`{"v":1,"id":%q,"at":"2026-10-17T12:00:00.5Z","instance":"other-copy","action":"identity.login","outcome":"succeeded","actor":{"kind":"person","person":%q}}`+"\n", id.String(), alice.String())
	}
	_ = ids
	put := func(name string) {
		wantNoErr(t, os.MkdirAll(f.spoolDir(), 0o700))
		wantNoErr(t, os.WriteFile(filepath.Join(f.spoolDir(), name), []byte(content), 0o600))
	}
	put("0001-1-aaaaaaaa.jsonl")
	got, err := f.log.ImportOnce(ctxT(t))
	wantNoErr(t, err)
	if got.Entries != 5 || got.Duplicates != 0 {
		t.Fatalf("first import = %+v", got)
	}
	put("0002-1-bbbbbbbb.jsonl") // the same entries again, as after a crash before the delete
	got, err = f.log.ImportOnce(ctxT(t))
	wantNoErr(t, err)
	if got.Entries != 0 || got.Duplicates != 5 || got.Files != 1 {
		t.Fatalf("second import = %+v, want 5 duplicates", got)
	}
	if n := f.count(t, ""); n != 5 {
		t.Errorf("rows = %d, want 5", n)
	}
	if n := f.count(t, "instance = 'other-copy' AND spooled"); n != 5 {
		t.Errorf("rows from the other copy = %d", n)
	}
}

func TestImportOnce_BadLine_KeepsEarlierLinesInserted(t *testing.T) {
	f := newIfx(t)
	good := func() string {
		return fmt.Sprintf(`{"v":1,"id":%q,"at":"2026-10-17T12:00:00Z","instance":"other-copy","action":"identity.login","outcome":"succeeded","actor":{"kind":"anonymous"}}`+"\n",
			model.NewID[audit.RecordID]().String())
	}
	wantNoErr(t, os.MkdirAll(f.spoolDir(), 0o700))
	p := filepath.Join(f.spoolDir(), "0001-1-aaaaaaaa.jsonl")
	wantNoErr(t, os.WriteFile(p, []byte(good()+good()+good()+"garbage\n"+good()), 0o600))
	got, err := f.log.ImportOnce(ctxT(t))
	wantKind(t, err, errs.Invalid)
	if len(got.Failed) != 1 {
		t.Fatalf("Imported = %+v", got)
	}
	if n := f.count(t, ""); n != 3 {
		t.Errorf("rows = %d, want the 3 lines before the bad one", n)
	}
	if _, err := os.Stat(p + ".failed"); err != nil {
		t.Errorf("no .failed file: %v", err)
	}
}

func TestImportOnce_BatchesLargeFile(t *testing.T) {
	f := newIfx(t)
	var b strings.Builder
	n := 2*audit.MaxBatch + 17
	for range n {
		fmt.Fprintf(&b, `{"v":1,"id":%q,"at":"2026-10-17T12:00:00Z","instance":"other-copy","action":"identity.login","outcome":"succeeded","actor":{"kind":"system"}}`+"\n",
			model.NewID[audit.RecordID]().String())
	}
	wantNoErr(t, os.MkdirAll(f.spoolDir(), 0o700))
	wantNoErr(t, os.WriteFile(filepath.Join(f.spoolDir(), "0001-1-aaaaaaaa.jsonl"), []byte(b.String()), 0o600))
	got, err := f.log.ImportOnce(ctxT(t))
	wantNoErr(t, err)
	if got.Entries != n || f.count(t, "") != n {
		t.Errorf("Entries = %d, rows = %d, want %d", got.Entries, f.count(t, ""), n)
	}
}

// refuseDB refuses, as a CHECK violation (23514), every insert whose body mentions marker.
type refuseDB struct {
	db.DB
	marker string
}

func (r refuseDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	for _, a := range args {
		if b, ok := a.([]byte); ok && strings.Contains(string(b), r.marker) {
			return pgconn.CommandTag{}, errs.E("test.refuseDB.Exec", errs.Invalid, &db.Error{Code: "23514"})
		}
	}
	return r.DB.Exec(ctx, sql, args...) //nolint:wrapcheck // test double
}

// F36b against the real database: the rows before the refused one are stored, the file is
// set aside, and the files around it import.
func TestImportOnce_InsertInvalid_StoresRowsBeforeRefused_SetsFileAside(t *testing.T) {
	const marker = "identity.poisoned"
	tests := []struct {
		name    string
		before  int
		wantRow int
	}{
		{"refused row first", 0, 0},
		{"one good line before it", 1, 1},
		{"a full batch before it", audit.MaxBatch, audit.MaxBatch},
		{"more than a batch before it", audit.MaxBatch + 5, audit.MaxBatch + 5},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newIfx(t, func(p *db.Pool) db.DB { return refuseDB{DB: p, marker: marker} })
			line := func(action string) string {
				return fmt.Sprintf(`{"v":1,"id":%q,"at":"2026-10-17T12:00:00Z","instance":"other-copy","action":%q,"outcome":"succeeded","actor":{"kind":"system"}}`+"\n",
					model.NewID[audit.RecordID]().String(), action)
			}
			wantNoErr(t, os.MkdirAll(f.spoolDir(), 0o700))
			write := func(name, content string) string {
				p := filepath.Join(f.spoolDir(), name)
				wantNoErr(t, os.WriteFile(p, []byte(content), 0o600))
				return p
			}
			write("0001-1-aaaaaaaa.jsonl", line("identity.login"))
			var b strings.Builder
			for range tc.before {
				b.WriteString(line("identity.login"))
			}
			b.WriteString(line(marker))
			bad := write("0002-1-bbbbbbbb.jsonl", b.String())
			write("0003-1-cccccccc.jsonl", line("identity.login"))
			got, err := f.log.ImportOnce(ctxT(t))
			wantKind(t, err, errs.Invalid)
			if len(got.Failed) != 1 {
				t.Fatalf("Imported = %+v", got)
			}
			if got.Entries != tc.wantRow+2 || f.count(t, "") != tc.wantRow+2 {
				t.Errorf("Entries = %d, rows = %d, want %d", got.Entries, f.count(t, ""), tc.wantRow+2)
			}
			if exists(bad) || !exists(bad+".failed") {
				t.Error("the refused file was not set aside")
			}
		})
	}
}

// --- Export ---------------------------------------------------------------------------------

type failAfter struct {
	n    int
	seen int
	err  error
}

func (w *failAfter) Write(p []byte) (int, error) {
	w.seen++
	if w.seen > w.n {
		return 0, w.err
	}
	return len(p), nil
}

func TestGuard_ThroughTheLogsPool_RefusesUpdateDeleteTruncate(t *testing.T) {
	f := newIfx(t)
	wantNoErr(t, f.scoped(t, "identity").Record(ctxT(t), loginEntry()))
	for _, stmt := range []string{
		"UPDATE core.audit_entry SET outcome = 'failed'",
		"DELETE FROM core.audit_entry",
		"TRUNCATE core.audit_entry",
	} {
		_, err := f.pool.Exec(ctxT(t), stmt)
		wantKind(t, err, errs.Internal)
	}
	if n := f.count(t, ""); n != 1 {
		t.Errorf("rows = %d", n)
	}
}
