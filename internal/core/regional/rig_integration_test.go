//go:build integration

package regional_test

// The integration rig: a regional.Service and a real settings.Service over one migrated,
// throw-away PostgreSQL database (dbtest.TempDB), with a files store that is either
// filestest.Fake behind a spy (fast, cannot roll back) or the real files.Service over an
// in-memory object store (rolls back; proves the discard).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"runtime"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit/audittest"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/identity" //nolint:depguard // only its migration set: files and people tables reference core.account/person
	"github.com/zedgama3/bms/internal/core/jobs/jobstest"
	"github.com/zedgama3/bms/internal/core/objects/objectstest"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/core/regional"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/core/settings/settingstest"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

const testTimeout = 90 * time.Second

func ctxT(t testing.TB) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	t.Cleanup(cancel)
	return ctx
}

// poll is the settings refresh interval of the replica tests.
const poll = 5 * time.Second

// irig is a regional rig over a real database.
type irig struct {
	*rig
	pool *db.Pool
}

func migrate(t testing.TB, pool *db.Pool) {
	t.Helper()
	mig, err := db.NewMigrator(pool, slog.New(slog.DiscardHandler))
	wantNoErr(t, err)
	_, err = mig.Migrate(ctxT(t), []db.Set{identity.Set, people.Set, files.Set, settings.Set})
	wantNoErr(t, err)
}

func newIRig(t *testing.T, mods ...func(*regional.Options)) *irig {
	t.Helper()
	pool := dbtest.TempDB(t)
	migrate(t, pool)
	return newIRigOn(t, pool, pool, 0, mods...)
}

// newIRigOn builds another rig over an existing database (a second process, a wrapper that
// injects faults); refresh is the settings poll interval.
func newIRigOn(t *testing.T, d db.DB, pool *db.Pool, refresh time.Duration, mods ...func(*regional.Options)) *irig {
	t.Helper()
	o, r := baseOptionsOn(t, d, nil, refresh)
	for _, m := range mods {
		m(&o)
	}
	ir := &irig{rig: buildRig(t, o, r), pool: pool}
	// files' uploaded_by references core.person.
	ir.exec(`INSERT INTO core.person (id, playa_name, legal_name, created_at, updated_at) VALUES ($1, 'Test Person', '', $2, $2)`,
		[16]byte(ir.admin), time.Now().UTC())
	return ir
}

func (r *irig) exec(sql string, args ...any) {
	r.t.Helper()
	if _, err := r.pool.Exec(ctxT(r.t), sql, args...); err != nil {
		r.t.Fatalf("exec %q: %v", sql, err)
	}
}

func (r *irig) count(sql string, args ...any) int {
	r.t.Helper()
	var n int
	if err := r.pool.QueryRow(ctxT(r.t), sql, args...).Scan(&n); err != nil {
		r.t.Fatalf("count %q: %v", sql, err)
	}
	return n
}

// dbRevision reads the stored settings revision straight from the table.
func (r *irig) dbRevision() int64 {
	r.t.Helper()
	var rev int64
	if err := r.pool.QueryRow(ctxT(r.t), "SELECT revision FROM core.setting_revision WHERE id = 1").Scan(&rev); err != nil {
		r.t.Fatalf("revision: %v", err)
	}
	return rev
}

// mustSet changes regional.* values through the real settings.Service (as the settings screen does).
func (r *irig) mustSet(kv ...string) settings.Revision {
	r.t.Helper()
	c := settings.Changes{Values: map[string]*string{}}
	for i := 0; i+1 < len(kv); i += 2 {
		v := kv[i+1]
		c.Values[kv[i]] = &v
	}
	rev, err := r.stg.Set(r.actx(), c)
	if err != nil {
		r.t.Fatalf("Set(%v): %v", kv, err)
	}
	return rev
}

// eventually yields the processor until cond holds or the test's deadline passes.
func eventually(t testing.TB, what string, cond func() bool) {
	t.Helper()
	ctx := ctxT(t)
	for !cond() {
		select {
		case <-ctx.Done():
			t.Fatalf("gave up waiting for %s", what)
		default:
			runtime.Gosched()
		}
	}
}

// ---- Run of the settings service (the replica poll) ---------------------------------------

type running struct {
	ended chan struct{}
	err   error
	stop  context.CancelFunc
}

// startRun runs r's settings.Service poll loop until the test ends.
func startRun(t *testing.T, r *irig) *running {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	run := &running{ended: make(chan struct{}), stop: cancel}
	go func() {
		run.err = r.stg.Run(ctx)
		close(run.ended)
	}()
	bctx, bcancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer bcancel()
	wantNoErr(t, r.clk.BlockUntil(bctx, 1))
	t.Cleanup(func() {
		cancel()
		<-run.ended
	})
	return run
}

func (run *running) shutdown(t *testing.T) {
	t.Helper()
	run.stop()
	select {
	case <-run.ended:
		wantNoErr(t, run.err)
	case <-time.After(30 * time.Second):
		t.Fatal("Run did not return after its context ended")
	}
}

// ---- the logo rigs -------------------------------------------------------------------------

// logoI is an integration rig with a files store as Options.Files.
type logoI struct {
	*irig
	fsvc *files.Service // set on the real rig
	mem  *objectstest.Mem
}

// newLogoI is a rig over filestest.Fake("settings") behind the spy.
func newLogoI(t *testing.T, mods ...func(*regional.Options)) *logoI {
	t.Helper()
	return &logoI{irig: newIRig(t, mods...)}
}

// newLogoIPoll is newLogoI with the settings poll interval at poll.
func newLogoIPoll(t *testing.T) *logoI {
	t.Helper()
	pool := dbtest.TempDB(t)
	migrate(t, pool)
	return &logoI{irig: newIRigOn(t, pool, pool, poll)}
}

// newRealLogoI is a rig over the real files service on the rig's database. policy is applied
// through the files.* settings, as a regional would set them.
func newRealLogoI(t *testing.T, policy files.Policy) *logoI {
	t.Helper()
	pool := dbtest.TempDB(t)
	migrate(t, pool)

	mem := objectstest.NewMem()
	fset := settingstest.NewFake("files", files.Settings)
	fset.Set("max_bytes", itoa(policy.MaxBytes))
	fset.Set("quota_bytes", itoa(policy.QuotaBytes))
	fset.Set("max_per_record", itoa(int64(policy.MaxPerRecord)))
	fsvc, err := files.New(files.Options{
		DB: pool, Clock: clock.NewFake(time.Now().UTC().Truncate(time.Microsecond)),
		Logger: slog.New(slog.DiscardHandler), Tracer: telemetrytest.New(t).Tracer("files"), Audit: audittest.NewFake("files"),
		Actor: actorOf, Objects: mem, TempDir: t.TempDir(), Settings: fset, Jobs: jobstest.NewFake("files"),
	})
	wantNoErr(t, err)
	store, err := fsvc.Scoped("settings")
	wantNoErr(t, err)
	spy := &spyStore{Store: store}
	ir := newIRigOn(t, pool, pool, 0, func(o *regional.Options) { o.Files = spy })
	ir.spy = spy
	return &logoI{irig: ir, mem: mem, fsvc: fsvc}
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}

func (r *logoI) fileRows() int { return r.count(`SELECT count(*) FROM core.file`) }

func (r *logoI) set(name string, data []byte) (regional.LogoResult, error) {
	return r.svc.SetLogo(r.actx(), name, bytes.NewReader(data)) //nolint:wrapcheck // the error under test
}

func (r *logoI) mustSetLogo(name string, data []byte) regional.LogoResult {
	r.t.Helper()
	res, err := r.set(name, data)
	if err != nil {
		r.t.Fatalf("SetLogo(%q): %v", name, err)
	}
	return res
}

func (r *logoI) publicLogo() string {
	r.t.Helper()
	p, err := r.svc.Public(r.actx())
	wantNoErr(r.t, err)
	return p.Logo
}

// publicETag fetches GET /public and returns its ETag and the document's logo.
func (r *logoI) publicETag(ifNoneMatch string) (code int, etag, logo string) {
	r.t.Helper()
	var hdr []string
	if ifNoneMatch != "" {
		hdr = []string{"If-None-Match", ifNoneMatch}
	}
	w, f := r.send(context.Background(), http.MethodGet, "/public", nil, "", hdr...)
	if w.Code == http.StatusOK {
		var doc struct {
			Logo string `json:"logo"`
		}
		wantNoErr(r.t, json.Unmarshal(w.Body.Bytes(), &doc))
		logo = doc.Logo
	} else if w.Code != http.StatusNotModified {
		r.t.Fatalf("GET /public = %d (%v)", w.Code, f.last())
	}
	return w.Code, w.Header().Get("ETag"), logo
}

func (r *logoI) onlyFile() files.File {
	r.t.Helper()
	got, err := r.spy.ByRecord(r.actx(), regional.LogoRef)
	wantNoErr(r.t, err)
	if len(got) != 1 {
		r.t.Fatalf("%d logo files, want 1", len(got))
	}
	return got[0]
}

func filesDefault() files.Policy { return files.DefaultPolicy() }

// readOnlyDB reports ReadOnly() and refuses Write transactions once ro is set.
type readOnlyDB struct {
	db.DB
	ro atomic.Bool
}

func (d *readOnlyDB) ReadOnly() bool { return d.ro.Load() || d.DB.ReadOnly() }

func (d *readOnlyDB) Transact(ctx context.Context, mode db.Mode, fn func(context.Context) error) error {
	if mode == db.Write && d.ro.Load() {
		return errs.E("test.readOnlyDB", errs.Unavailable, db.ErrReadOnly)
	}
	return d.DB.Transact(ctx, mode, fn) //nolint:wrapcheck // test double
}

func wantReadOnly(t testing.TB, err error) {
	t.Helper()
	wantKind(t, err, errs.Unavailable)
	if !errors.Is(err, db.ErrReadOnly) {
		t.Fatalf("error does not wrap db.ErrReadOnly: %v", err)
	}
}

// ---- helpers only the integration tests use ----

func describe(err error) []string {
	var out []string
	for _, fe := range validate.Fields(err) {
		out = append(out, fe.Field+"/"+fe.Code)
	}
	return out
}

// first returns the first of two results.
func first[T, U any](v T, _ U) T { return v }

func asSuper(ctx context.Context, p model.PersonID) context.Context {
	return context.WithValue(ctx, actorKey{}, authz.Actor{Person: p, Superuser: true})
}

func hasField(err error, field, code string) bool {
	for _, fe := range validate.Fields(err) {
		if (field == "" || fe.Field == field) && (code == "" || fe.Code == code) {
			return true
		}
	}
	return false
}
