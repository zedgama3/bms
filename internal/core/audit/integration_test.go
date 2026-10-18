//go:build integration

package audit_test

import (
	"bytes"
	"context"
	"encoding/json"
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
	"github.com/zedgama3/bms/internal/core/authz"
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
	perms  *fakePerms
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
		perms:  &fakePerms{allow: map[authz.Permission]bool{audit.PermissionSearch: true, audit.PermissionExport: true}},
	}
	f.opts = audit.Options{
		DB: pool, Clock: f.clk, Logger: f.cap.Logger(), Tracer: f.cap.Tracer("audit"), Meter: f.cap.Meter("audit"),
		Instance: "test-copy", DataDir: f.dir, Actor: sessionActor, Titles: f.titles, Permissions: f.perms,
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

func (f *ifx) search(t *testing.T, q audit.Query) audit.Page {
	t.Helper()
	p, err := f.log.Search(withSession(ctxT(t), person(carol)), q)
	wantNoErr(t, err)
	return p
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

	page := f.search(t, audit.Query{Area: "identity"})
	if len(page.Records) != 2 {
		t.Fatalf("records = %d, want 2", len(page.Records))
	}
	var got audit.Record
	for _, r := range page.Records {
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
	for _, r := range page.Records {
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

func TestSearch_KeysetOrderAndCursorContinuity_250RowsWithEqualAt(t *testing.T) {
	f := newIfx(t)
	rec := f.scoped(t, "identity")
	batch := make([]audit.Entry, 250)
	for i := range batch {
		batch[i] = loginEntry()
	}
	wantNoErr(t, rec.Record(ctxT(t), batch...)) // one instant for every row
	q := audit.Query{Area: "identity", Limit: 100}
	seen := map[audit.RecordID]bool{}
	var order []audit.Record
	pages := 0
	for {
		p := f.search(t, q)
		pages++
		if pages > 5 {
			t.Fatal("cursor never ends")
		}
		for _, r := range p.Records {
			if seen[r.ID] {
				t.Fatalf("record %s appears on two pages", r.ID)
			}
			seen[r.ID] = true
			order = append(order, r)
		}
		if p.Next == "" {
			break
		}
		if len(p.Records) != 100 {
			t.Fatalf("a full page has %d records", len(p.Records))
		}
		q.After = p.Next
	}
	if len(order) != 250 || pages != 3 {
		t.Fatalf("got %d records in %d pages, want 250 in 3", len(order), pages)
	}
	for i := 1; i < len(order); i++ {
		a, b := order[i-1], order[i]
		if a.At.Before(b.At) || (a.At.Equal(b.At) && bytes.Compare(a.ID[:], b.ID[:]) <= 0) {
			t.Fatalf("not newest first at %d: (%v %s) then (%v %s)", i, a.At, a.ID, b.At, b.ID)
		}
	}
}

func TestSearch_LimitDefaultsAndLastPageHasNoCursor(t *testing.T) {
	f := newIfx(t)
	rec := f.scoped(t, "identity")
	batch := make([]audit.Entry, audit.DefaultPageSize+1)
	for i := range batch {
		batch[i] = loginEntry()
	}
	wantNoErr(t, rec.Record(ctxT(t), batch...))
	p := f.search(t, audit.Query{Area: "identity"})
	if len(p.Records) != audit.DefaultPageSize || p.Next == "" {
		t.Errorf("default page: %d records, next %q", len(p.Records), p.Next)
	}
	exact := f.search(t, audit.Query{Area: "identity", Limit: audit.DefaultPageSize + 1})
	if len(exact.Records) != audit.DefaultPageSize+1 || exact.Next != "" {
		t.Errorf("exactly-full page must have no cursor: %d records, next %q", len(exact.Records), exact.Next)
	}
	full := f.search(t, audit.Query{Area: "identity", Limit: audit.MaxPageSize})
	if len(full.Records) != audit.DefaultPageSize+1 || full.Next != "" {
		t.Errorf("max limit page: %d / %q", len(full.Records), full.Next)
	}
}

func TestSearch_Filters(t *testing.T) {
	f := newIfx(t)
	idRec := f.scoped(t, "identity")
	ppl := f.scoped(t, "people")
	acct := audit.RefOf("identity.account", model.AccountID{8})

	put := func(r audit.Recorder, ctx context.Context, es ...audit.Entry) {
		t.Helper()
		wantNoErr(t, r.Record(ctx, es...))
	}
	in := func(a audit.Actor) context.Context { return withSession(ctxT(t), a) }

	put(idRec, in(person(alice)), audit.Entry{Action: "identity.login", Outcome: audit.Succeeded})
	advance(t, f.clk, time.Hour)
	put(idRec, in(superuser(bob)), audit.Entry{Action: "identity.account.read", Outcome: audit.Succeeded, Subject: alice, Record: acct,
		Fields: []string{"email"}, Level: model.SensitivityPersonal})
	advance(t, f.clk, time.Hour)
	put(ppl, in(person(bob)), audit.Entry{Action: "people.person.read", Outcome: audit.Succeeded, Subject: alice})
	advance(t, f.clk, time.Hour)
	put(idRec, in(audit.Actor{}), audit.Entry{Action: "identity.login-failed", Outcome: audit.Failed, Actor: audit.Actor{Kind: audit.ActorAnonymous}})

	t0 := epochMicro
	tests := []struct {
		name string
		q    audit.Query
		want int
	}{
		{"area identity", audit.Query{Area: "identity"}, 3},
		{"area people", audit.Query{Area: "people"}, 1},
		{"area prefix does not match", audit.Query{Area: "ident"}, 0},
		{"action exact", audit.Query{Action: "identity.login"}, 1},
		{"action no prefix match", audit.Query{Action: "identity.account"}, 0},
		{"actor", audit.Query{Area: "identity", Actor: bob}, 1},
		{"actor across areas", audit.Query{Actor: bob}, 2},
		{"subject", audit.Query{Area: "identity", Subject: alice}, 1},
		{"subject across areas", audit.Query{Subject: alice}, 2},
		{"record", audit.Query{Record: acct}, 1},
		{"superuser only", audit.Query{Area: "identity", SuperuserOnly: true}, 1},
		{"within includes start", audit.Query{Area: "identity", Within: model.TimeRange{Start: t0, End: t0.Add(time.Hour)}}, 1},
		{"within excludes end", audit.Query{Area: "identity", Within: model.TimeRange{Start: t0.Add(time.Hour), End: t0.Add(2 * time.Hour)}}, 1},
		{"within the whole", audit.Query{Area: "identity", Within: model.TimeRange{Start: t0, End: t0.Add(4 * time.Hour)}}, 3},
		{"within empty window", audit.Query{Area: "identity", Within: model.TimeRange{Start: t0.Add(10 * time.Hour), End: t0.Add(11 * time.Hour)}}, 0},
		{"combined", audit.Query{Area: "identity", Actor: bob, Subject: alice, SuperuserOnly: true}, 1},
		{"combined, none", audit.Query{Area: "people", SuperuserOnly: true}, 0},
		{"anonymous actor is reachable by action", audit.Query{Action: "identity.login-failed"}, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Search records audit.searched in area audit, so area-less queries must not
			// count it: filter the page rather than the query where the filter is empty.
			p := f.search(t, tc.q)
			n := 0
			for _, r := range p.Records {
				if r.Action.Area() != "audit" {
					n++
				}
			}
			if n != tc.want {
				t.Errorf("matched %d, want %d", n, tc.want)
			}
		})
	}
}

func TestSearch_RecordsItselfInTheTable(t *testing.T) {
	f := newIfx(t)
	q := audit.Query{Subject: bob, Record: audit.RefOf("identity.account", model.AccountID{3})}
	f.search(t, q)
	if n := f.count(t, "action = 'audit.searched' AND actor_person_id = $1::uuid AND subject_person_id = $2::uuid AND record_id = $3::uuid",
		carol.String(), bob.String(), (model.AccountID{3}).String()); n != 1 {
		t.Errorf("audit.searched rows = %d, want 1", n)
	}
	f.search(t, audit.Query{})
	if n := f.count(t, "action = 'audit.searched'"); n != 2 {
		t.Errorf("audit.searched rows = %d, want 2", n)
	}
}

func TestSearch_PoolClosed_ReturnsUnavailable(t *testing.T) {
	f := newIfx(t)
	if err := f.pool.Close(ctxT(t)); err != nil {
		t.Fatal(err)
	}
	_, err := f.log.Search(withSession(ctxT(t), person(carol)), audit.Query{})
	wantKind(t, err, errs.Unavailable)
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
	page, err := w.Search(withSession(ctxT(t), person(carol)), audit.Query{Area: "identity"})
	wantNoErr(t, err)
	for _, r := range page.Records {
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

func TestExport_StreamsOldestFirst_InBatches_AndRecordsFirst(t *testing.T) {
	f := newIfx(t)
	rec := f.scoped(t, "people")
	total := 0
	for round := 0; round < 3; round++ {
		batch := make([]audit.Entry, 150) // 450 rows: more than one MaxPageSize batch
		for i := range batch {
			batch[i] = audit.Entry{Action: "people.person.read", Outcome: audit.Succeeded, Subject: bob}
		}
		wantNoErr(t, rec.Record(ctxT(t), batch...))
		total += len(batch)
		advance(t, f.clk, time.Minute)
	}
	// Rows about someone else must not appear.
	wantNoErr(t, rec.Record(ctxT(t), audit.Entry{Action: "people.person.read", Outcome: audit.Succeeded, Subject: alice}))

	var out bytes.Buffer
	ctx := withSession(ctxT(t), person(carol))
	checked := false
	w := writerFunc(func(p []byte) (int, error) {
		if !checked {
			checked = true
			// The first line is about to be written: the export is already on record.
			if n := f.count(t, "action = 'audit.exported' AND subject_person_id = $1::uuid", bob.String()); n != 1 {
				t.Errorf("audit.exported rows at the first write = %d, want 1", n)
			}
		}
		return out.Write(p) //nolint:wrapcheck // test
	})
	n, err := f.log.Export(ctx, bob, w)
	wantNoErr(t, err)
	// The export's own audit.exported entry (Subject bob) is recorded first, so it is part of
	// what the export streams.
	total++
	if n != total {
		t.Fatalf("Export returned %d, want %d", n, total)
	}
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) != total {
		t.Fatalf("lines = %d, want %d", len(lines), total)
	}
	var prev time.Time
	ids := map[string]bool{}
	for _, l := range lines {
		var r struct {
			ID string    `json:"id"`
			At time.Time `json:"at"`
		}
		if err := json.Unmarshal([]byte(l), &r); err != nil {
			t.Fatalf("line is not JSON: %v", err)
		}
		if r.At.Before(prev) {
			t.Fatal("export is not oldest first")
		}
		prev = r.At
		ids[r.ID] = true
		if strings.Contains(l, alice.String()+`"`) && strings.Contains(l, `"subject":"`+alice.String()) {
			t.Fatal("an entry about another person was exported")
		}
	}
	if len(ids) != total {
		t.Errorf("distinct ids = %d", len(ids))
	}
	if n := f.count(t, "action = 'audit.exported'"); n != 1 {
		t.Errorf("audit.exported rows = %d, want exactly 1", n)
	}
}

type writerFunc func([]byte) (int, error)

func (w writerFunc) Write(p []byte) (int, error) { return w(p) }

func TestExport_WriterFails_ReturnsCanceled(t *testing.T) {
	f := newIfx(t)
	rec := f.scoped(t, "people")
	batch := make([]audit.Entry, 300)
	for i := range batch {
		batch[i] = audit.Entry{Action: "people.person.read", Outcome: audit.Succeeded, Subject: bob}
	}
	wantNoErr(t, rec.Record(ctxT(t), batch...))
	boom := errors.New("client went away")
	for _, after := range []int{0, 5, 250} { // first line, mid-batch, in the second batch
		w := &failAfter{n: after, err: boom}
		_, err := f.log.Export(withSession(ctxT(t), person(carol)), bob, w)
		wantKind(t, err, errs.Canceled)
		if !errors.Is(err, boom) {
			t.Errorf("after %d lines: the write error is not wrapped", after)
		}
	}
	if n := f.count(t, "action = 'audit.exported'"); n != 3 {
		t.Errorf("audit.exported rows = %d: a failed export is still on record", n)
	}
}

func TestExport_SpoolUnwritable_WritesNothing(t *testing.T) {
	f := newIfx(t)
	rec := f.scoped(t, "people")
	wantNoErr(t, rec.Record(ctxT(t), audit.Entry{Action: "people.person.read", Outcome: audit.Succeeded, Subject: bob}))
	// Unwritable spool on a read-only copy: the entry for the export cannot be recorded.
	blocker := filepath.Join(t.TempDir(), "file")
	wantNoErr(t, os.WriteFile(blocker, []byte("x"), 0o600))
	o := f.opts
	o.DB = roDB{DB: f.pool}
	o.DataDir = blocker
	l := f.build(t, o)
	var out bytes.Buffer
	_, err := l.Export(withSession(ctxT(t), person(carol)), bob, &out)
	wantKind(t, err, errs.Unavailable)
	if out.Len() != 0 {
		t.Error("export wrote lines though its entry could not be recorded")
	}
}

func TestExport_ReadOnlyCopy_WorksAndSpoolsItsEntry(t *testing.T) {
	f := newIfx(t)
	wantNoErr(t, f.scoped(t, "people").Record(ctxT(t), audit.Entry{Action: "people.person.read", Outcome: audit.Succeeded, Subject: bob}))
	o := f.opts
	o.DB = roDB{DB: f.pool}
	l := f.build(t, o)
	var out bytes.Buffer
	n, err := l.Export(withSession(ctxT(t), person(carol)), bob, &out)
	wantNoErr(t, err)
	if n != 1 {
		t.Errorf("exported %d, want 1", n)
	}
	files := globSorted(t, filepath.Join(f.spoolDir(), "*.jsonl"))
	if len(files) != 1 {
		t.Errorf("spool files = %v: the export's own entry should be spooled", files)
	}
}

// --- CountAbout and Purge ---------------------------------------------------------------------

func TestCountAbout_ActorOrSubject(t *testing.T) {
	f := newIfx(t)
	idRec := f.scoped(t, "identity")
	in := func(a audit.Actor) context.Context { return withSession(ctxT(t), a) }
	wantNoErr(t, idRec.Record(in(person(alice)), loginEntry()))                                                                           // actor only
	wantNoErr(t, idRec.Record(in(person(bob)), audit.Entry{Action: "identity.account.read", Outcome: audit.Succeeded, Subject: alice}))   // subject only
	wantNoErr(t, idRec.Record(in(person(alice)), audit.Entry{Action: "identity.account.read", Outcome: audit.Succeeded, Subject: alice})) // both: one row
	wantNoErr(t, idRec.Record(in(person(bob)), loginEntry()))                                                                             // neither
	n, err := f.log.CountAbout(ctxT(t), alice)
	wantNoErr(t, err)
	if n != 3 {
		t.Errorf("CountAbout(alice) = %d, want 3", n)
	}
	n, err = f.log.CountAbout(ctxT(t), carol)
	wantNoErr(t, err)
	if n != 0 {
		t.Errorf("CountAbout(carol) = %d, want 0", n)
	}
}

func TestPurge_DeletesOlderRows_RecordsAuditPurged(t *testing.T) {
	f := newIfx(t)
	rec := f.scoped(t, "identity")
	for i := range 3 {
		wantNoErr(t, rec.Record(ctxT(t), loginEntry(), loginEntry()))
		if i < 2 {
			advance(t, f.clk, 24*time.Hour)
		}
	}
	// rows at day 0 (2), day 1 (2), day 2 (2). Purge everything before day 1 00:00.
	advance(t, f.clk, time.Hour)
	before := epochMicro.Add(24 * time.Hour)
	n, err := f.log.Purge(ctxT(t), before)
	wantNoErr(t, err)
	if n != 2 {
		t.Errorf("Purge returned %d, want 2", n)
	}
	if left := f.count(t, "action = 'identity.login'"); left != 4 {
		t.Errorf("identity rows left = %d, want 4", left)
	}
	var kind string
	var superuser bool
	if err := f.pool.QueryRow(ctxT(t), "SELECT actor_kind, actor_superuser FROM core.audit_entry WHERE action = 'audit.purged'").Scan(&kind, &superuser); err != nil {
		t.Fatalf("no audit.purged row: %v", err)
	}
	if kind != "system" || superuser {
		t.Errorf("audit.purged actor = %s superuser %v", kind, superuser)
	}
	// Purging again records another entry; the guard is closed again afterwards.
	if _, err := f.pool.Exec(ctxT(t), "DELETE FROM core.audit_entry"); err == nil {
		t.Error("a plain DELETE succeeded after Purge")
	}
	n, err = f.log.Purge(ctxT(t), before)
	wantNoErr(t, err)
	if n != 0 {
		t.Errorf("second Purge deleted %d", n)
	}
	if c := f.count(t, "action = 'audit.purged'"); c != 2 {
		t.Errorf("audit.purged rows = %d, want 2", c)
	}
}

func TestPurge_FutureOrZero_ReturnsInvalid_NothingDeleted(t *testing.T) {
	f := newIfx(t)
	wantNoErr(t, f.scoped(t, "identity").Record(ctxT(t), loginEntry()))
	_, err := f.log.Purge(ctxT(t), time.Time{})
	wantKind(t, err, errs.Invalid)
	_, err = f.log.Purge(ctxT(t), epochMicro.Add(time.Hour))
	wantKind(t, err, errs.Invalid)
	if n := f.count(t, ""); n != 1 {
		t.Errorf("rows = %d", n)
	}
}

func TestPurge_ReadOnlyPool_ReturnsErrReadOnly(t *testing.T) {
	f := newIfx(t, func(p *db.Pool) db.DB { return roDB{DB: p} })
	_, err := f.log.Purge(ctxT(t), epochMicro.Add(-time.Hour))
	wantReadOnly(t, err)
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
