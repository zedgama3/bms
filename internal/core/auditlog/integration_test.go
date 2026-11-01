//go:build integration

package auditlog_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn" //nolint:depguard // roDB must implement db.DB, whose Exec returns pgconn.CommandTag

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/auditlog"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/authz/authztest"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/core/jobs/jobstest"
	"github.com/zedgama3/bms/internal/core/settings/settingstest"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

var epochMicro = time.Date(2026, 10, 17, 12, 0, 0, 123456000, time.UTC) // PostgreSQL keeps microseconds

// ifx is the reader over a real, migrated, throw-away database, plus an audit.Log on the same
// table to seed it, as audit's integration rig did.
type ifx struct {
	svc      *auditlog.Service
	opts     auditlog.Options
	log      *audit.Log
	pool     *db.Pool
	clk      *clock.Fake
	dir      string
	cap      *telemetrytest.Capture
	authz    *authztest.Fake
	settings *settingstest.Fake
	jobs     *jobstest.Fake
}

// auditActor adapts the test session to audit's ActorFunc, as identity.AuditActor would.
func auditActor(ctx context.Context) (audit.Actor, bool) {
	a, ok := sessionActor(ctx)
	if !ok || a.Person.IsZero() {
		return audit.Actor{}, false
	}
	return audit.Actor{Kind: audit.ActorPerson, Person: a.Person, Superuser: a.Superuser}, true
}

func newAuditLog(t *testing.T, d db.DB, clk clock.Clock, dir string, tl audit.TitleSource) *audit.Log {
	t.Helper()
	cap := telemetrytest.New(t)
	l, err := audit.New(audit.Options{
		DB: d, Clock: clk, Logger: slog.New(slog.DiscardHandler), Tracer: cap.Tracer("audit"), Meter: cap.Meter("audit"),
		Instance: "test-copy", DataDir: dir, Actor: auditActor, Titles: tl,
	})
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
		pool: pool, clk: clock.NewFake(epochMicro), dir: t.TempDir(), cap: telemetrytest.New(t),
		authz:    authztest.NewFake(),
		settings: settingstest.NewFake("audit", auditlog.Settings),
		jobs:     jobstest.NewFake("audit"),
	}
	f.authz.SetActor(sessionActor)
	f.authz.Grant(carol, auditlog.PermissionSearch)
	f.authz.Grant(carol, auditlog.PermissionExport)
	var d db.DB = pool
	for _, w := range wrap {
		d = w(pool)
	}
	f.log = newAuditLog(t, d, f.clk, f.dir, f.authz)
	rec, err := f.log.Scoped("audit")
	wantNoErr(t, err)
	f.opts = auditlog.Options{
		DB: d, Clock: f.clk, Logger: f.cap.Logger(), Tracer: f.cap.Tracer("auditlog"),
		Authz: f.authz, Actor: sessionActor, Settings: f.settings, Jobs: f.jobs, Audit: rec,
	}
	f.svc = f.build(t, f.opts)
	return f
}

func (f *ifx) build(t *testing.T, o auditlog.Options) *auditlog.Service {
	t.Helper()
	s, err := auditlog.New(o)
	if err != nil {
		t.Fatalf("auditlog.New: %v", err)
	}
	return s
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

func (f *ifx) search(t *testing.T, q audit.Query) audit.Page {
	t.Helper()
	p, err := f.svc.Search(withSession(ctxT(t), person(carol)), q)
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

type failAfter struct {
	n   int
	err error
}

func (w *failAfter) Write(p []byte) (int, error) {
	if w.n <= 0 {
		return 0, w.err
	}
	w.n--
	return len(p), nil
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
	in := func(a authz.Actor) context.Context { return withSession(ctxT(t), a) }

	put(idRec, in(person(alice)), audit.Entry{Action: "identity.login", Outcome: audit.Succeeded})
	advance(t, f.clk, time.Hour)
	put(idRec, in(superuser(bob)), audit.Entry{Action: "identity.account.read", Outcome: audit.Succeeded, Subject: alice, Record: acct,
		Fields: []string{"email"}, Level: model.SensitivityPersonal})
	advance(t, f.clk, time.Hour)
	put(ppl, in(person(bob)), audit.Entry{Action: "people.person.read", Outcome: audit.Succeeded, Subject: alice})
	advance(t, f.clk, time.Hour)
	put(idRec, ctxT(t), audit.Entry{Action: "identity.login-failed", Outcome: audit.Failed, Actor: audit.Actor{Kind: audit.ActorAnonymous}})

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
	_, err := f.svc.Search(withSession(ctxT(t), person(carol)), audit.Query{})
	wantKind(t, err, errs.Unavailable)
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
	n, err := f.svc.Export(ctx, bob, w)
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
		_, err := f.svc.Export(withSession(ctxT(t), person(carol)), bob, w)
		wantKind(t, err, errs.Canceled)
		if !errors.Is(err, boom) {
			t.Errorf("after %d lines: the write error is not wrapped", after)
		}
	}
	if n := f.count(t, "action = 'audit.exported'"); n != 3 {
		t.Errorf("audit.exported rows = %d: a failed export is still on record", n)
	}
}

// --- CountAbout and Purge ---------------------------------------------------------------------

func TestCountAbout_ActorOrSubject(t *testing.T) {
	f := newIfx(t)
	idRec := f.scoped(t, "identity")
	in := func(a authz.Actor) context.Context { return withSession(ctxT(t), a) }
	wantNoErr(t, idRec.Record(in(person(alice)), loginEntry()))                                                                           // actor only
	wantNoErr(t, idRec.Record(in(person(bob)), audit.Entry{Action: "identity.account.read", Outcome: audit.Succeeded, Subject: alice}))   // subject only
	wantNoErr(t, idRec.Record(in(person(alice)), audit.Entry{Action: "identity.account.read", Outcome: audit.Succeeded, Subject: alice})) // both: one row
	wantNoErr(t, idRec.Record(in(person(bob)), loginEntry()))                                                                             // neither
	n, err := f.svc.CountAbout(ctxT(t), alice)
	wantNoErr(t, err)
	if n != 3 {
		t.Errorf("CountAbout(alice) = %d, want 3", n)
	}
	n, err = f.svc.CountAbout(ctxT(t), carol)
	wantNoErr(t, err)
	if n != 0 {
		t.Errorf("CountAbout(carol) = %d, want 0", n)
	}
}

// --- Export on a read-only copy, through the recorder's spool ---------------------------

func TestExport_SpoolUnwritable_WritesNothing(t *testing.T) {
	f := newIfx(t)
	wantNoErr(t, f.scoped(t, "people").Record(ctxT(t), audit.Entry{Action: "people.person.read", Outcome: audit.Succeeded, Subject: bob}))
	// Unwritable spool on a read-only copy: the entry for the export cannot be recorded.
	blocker := filepath.Join(t.TempDir(), "file")
	wantNoErr(t, os.WriteFile(blocker, []byte("x"), 0o600))
	ro := roDB{DB: f.pool}
	rec, err := newAuditLog(t, ro, f.clk, blocker, f.authz).Scoped("audit")
	wantNoErr(t, err)
	o := f.opts
	o.DB = ro
	o.Audit = rec
	var out bytes.Buffer
	_, err = f.build(t, o).Export(withSession(ctxT(t), person(carol)), bob, &out)
	wantKind(t, err, errs.Unavailable)
	if out.Len() != 0 {
		t.Error("export wrote lines though its entry could not be recorded")
	}
}

func TestExport_ReadOnlyCopy_WorksAndSpoolsItsEntry(t *testing.T) {
	f := newIfx(t)
	wantNoErr(t, f.scoped(t, "people").Record(ctxT(t), audit.Entry{Action: "people.person.read", Outcome: audit.Succeeded, Subject: bob}))
	ro := roDB{DB: f.pool}
	spool := t.TempDir()
	rec, err := newAuditLog(t, ro, f.clk, spool, f.authz).Scoped("audit")
	wantNoErr(t, err)
	o := f.opts
	o.DB = ro
	o.Audit = rec
	var out bytes.Buffer
	n, err := f.build(t, o).Export(withSession(ctxT(t), person(carol)), bob, &out)
	wantNoErr(t, err)
	if n != 1 {
		t.Errorf("exported %d, want 1", n)
	}
	files, gerr := filepath.Glob(filepath.Join(spool, filepath.FromSlash(audit.SpoolDirName), "*.jsonl"))
	wantNoErr(t, gerr)
	if len(files) != 1 {
		t.Errorf("spool files = %v: the export's own entry should be spooled", files)
	}
}

// --- the purge job, end to end -------------------------------------------------------------

// seedDays records two logins on each of three days (day 0 is epochMicro) and leaves the clock
// at day 2 plus the time given.
func (f *ifx) seedDays(t *testing.T) {
	t.Helper()
	rec := f.scoped(t, "identity")
	for i := range 3 {
		wantNoErr(t, rec.Record(ctxT(t), loginEntry(), loginEntry()))
		if i < 2 {
			advance(t, f.clk, 24*time.Hour)
		}
	}
}

func loginEntry() audit.Entry {
	return audit.Entry{Action: "identity.login", Outcome: audit.Succeeded}
}

func runPurgeJob(t *testing.T, f *ifx) error {
	t.Helper()
	wantNoErr(t, f.svc.Start(ctxT(t)))
	return f.jobs.Run(ctxT(t), "audit.purge", jobs.Job{})
}

func TestPurge_DeletesOlderRows_RecordsAuditPurged(t *testing.T) {
	f := newIfx(t)
	f.seedDays(t) // rows at day 0 (2), day 1 (2), day 2 (2); the clock is at day 2
	// Retention 8760h: put the clock so that "now - retention" falls between day 0 and day 1.
	const keep = 8760 * time.Hour
	advance(t, f.clk, keep+23*time.Hour-48*time.Hour)
	f.settings.Set("retention", "8760h")
	// The clock now reads day0 + keep + 23h: the cut-off is day0 + 23h, after day 0, before day 1.
	wantNoErr(t, runPurgeJob(t, f))
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
	// The guard is closed again afterwards.
	if _, err := f.pool.Exec(ctxT(t), "DELETE FROM core.audit_entry"); err == nil {
		t.Error("a plain DELETE succeeded after the purge")
	}
	// A second run deletes nothing more and records another entry.
	wantNoErr(t, f.jobs.Run(ctxT(t), "audit.purge", jobs.Job{}))
	if left := f.count(t, "action = 'identity.login'"); left != 4 {
		t.Errorf("second run: identity rows left = %d, want 4", left)
	}
	if c := f.count(t, "action = 'audit.purged'"); c != 2 {
		t.Errorf("audit.purged rows = %d, want 2", c)
	}
}

func TestPurge_RetentionZero_KeepsEverything(t *testing.T) {
	f := newIfx(t)
	f.seedDays(t)
	advance(t, f.clk, 1000*24*time.Hour)
	wantNoErr(t, runPurgeJob(t, f))
	if n := f.count(t, ""); n != 6 {
		t.Errorf("rows = %d, want all 6 kept (retention 0 keeps forever)", n)
	}
}

func TestPurge_ReadOnlyPool_ReturnsUnavailableWrappingErrReadOnly(t *testing.T) {
	f := newIfx(t, func(p *db.Pool) db.DB { return roDB{DB: p} })
	f.settings.Set("retention", "8760h")
	advance(t, f.clk, 9000*time.Hour)
	wantReadOnly(t, runPurgeJob(t, f))
}

func TestGuard_ThroughTheLogsPool_RefusesUpdateDeleteTruncate_AfterReads(t *testing.T) {
	f := newIfx(t)
	wantNoErr(t, f.scoped(t, "identity").Record(ctxT(t), loginEntry()))
	f.search(t, audit.Query{})
	if _, err := f.pool.Exec(ctxT(t), "DELETE FROM core.audit_entry"); err == nil {
		t.Error("a plain DELETE succeeded after a search")
	}
}

// --- the stored row carries the superuser mark of the real recorder ------------------------

func (f *ifx) superuserOf(t *testing.T, action string) bool {
	t.Helper()
	var su bool
	if err := f.pool.QueryRow(ctxT(t), "SELECT actor_superuser FROM core.audit_entry WHERE action = $1", action).Scan(&su); err != nil {
		t.Fatalf("no %s row: %v", action, err)
	}
	return su
}

func TestSearch_SuperuserActor_StoredRowIsMarked(t *testing.T) {
	f := newIfx(t)
	f.authz.Superuser(bob)
	_, err := f.svc.Search(withSession(ctxT(t), superuser(bob)), audit.Query{})
	wantNoErr(t, err)
	if !f.superuserOf(t, "audit.searched") {
		t.Error("a superuser's audit.searched row has actor_superuser false")
	}
}

func TestSearch_PlainPerson_StoredRowIsNotMarked(t *testing.T) {
	f := newIfx(t)
	f.search(t, audit.Query{})
	if f.superuserOf(t, "audit.searched") {
		t.Error("a plain person's audit.searched row has actor_superuser true")
	}
}

func TestExport_SuperuserActor_StoredRowIsMarked(t *testing.T) {
	f := newIfx(t)
	f.authz.Superuser(bob)
	_, err := f.svc.Export(withSession(ctxT(t), superuser(bob)), alice, &bytes.Buffer{})
	wantNoErr(t, err)
	if !f.superuserOf(t, "audit.exported") {
		t.Error("a superuser's audit.exported row has actor_superuser false")
	}
}

func TestExport_PlainPerson_StoredRowIsNotMarked(t *testing.T) {
	f := newIfx(t)
	_, err := f.svc.Export(withSession(ctxT(t), person(carol)), alice, &bytes.Buffer{})
	wantNoErr(t, err)
	if f.superuserOf(t, "audit.exported") {
		t.Error("a plain person's audit.exported row has actor_superuser true")
	}
}

// --- Search on a read-only copy, through the recorder's spool ------------------------------

func TestSearch_ReadOnlyCopy_Works_AndSpoolsItsOwnEntry(t *testing.T) {
	f := newIfx(t)
	ro := roDB{DB: f.pool}
	spool := t.TempDir()
	rec, err := newAuditLog(t, ro, f.clk, spool, f.authz).Scoped("audit")
	wantNoErr(t, err)
	o := f.opts
	o.DB = ro
	o.Audit = rec
	_, err = f.build(t, o).Search(withSession(ctxT(t), person(carol)), audit.Query{})
	wantNoErr(t, err)
	files, gerr := filepath.Glob(filepath.Join(spool, filepath.FromSlash(audit.SpoolDirName), "*.jsonl"))
	wantNoErr(t, gerr)
	if len(files) != 1 {
		t.Errorf("spool files = %v: the search's own entry should be spooled", files)
	}
	if n := f.count(t, "action = 'audit.searched'"); n != 0 {
		t.Errorf("audit.searched rows = %d on a read-only copy, want 0 (spooled instead)", n)
	}
}

func TestSearch_SpoolUnwritable_FailsTheSearch(t *testing.T) {
	f := newIfx(t)
	blocker := filepath.Join(t.TempDir(), "file")
	wantNoErr(t, os.WriteFile(blocker, []byte("x"), 0o600))
	ro := roDB{DB: f.pool}
	rec, err := newAuditLog(t, ro, f.clk, blocker, f.authz).Scoped("audit")
	wantNoErr(t, err)
	o := f.opts
	o.DB = ro
	o.Audit = rec
	_, err = f.build(t, o).Search(withSession(ctxT(t), person(carol)), audit.Query{})
	wantKind(t, err, errs.Unavailable)
}
