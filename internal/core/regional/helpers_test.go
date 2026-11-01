package regional_test

// Shared fixtures for the regional tests (boundaries step 7; regional.md). Every person,
// address and value here is invented.
//
// The unit rigs run over dbtest.Fake with a fake RevisionSource and a settingstest reader; the
// integration rigs (rig_integration_test.go) run over a real database with a real
// settings.Service as the Reader, RevisionSource and TermSource.

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/audit/audittest"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/authz/authztest"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/files/filestest"
	"github.com/zedgama3/bms/internal/core/regional"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/core/settings/settingstest"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

var epoch = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

// testSealingKey is an invented key of the length config allows (32..1024 bytes).
var testSealingKey = secret.New("0123456789abcdef0123456789abcdef-invented")

func newPerson() model.PersonID { return model.NewID[model.PersonID]() }

// errOf returns only the error of a call's results, for table rows that test one error.
func errOf[T any](_ T, err error) error { return err }

// ignore swallows results a test does not need.
func ignore(...any) {}

// ---- error assertions ------------------------------------------------------------------

func wantKind(t testing.TB, err error, kind errs.Kind) {
	t.Helper()
	if err == nil {
		t.Fatalf("got nil error, want kind %v", kind)
	}
	if !errs.Is(err, kind) {
		t.Fatalf("kind = %v (%v), want %v", errs.KindOf(err), err, kind)
	}
}

func wantNoErr(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// wantField checks Invalid carrying a field error for field and code ("" matches any).
func wantField(t testing.TB, err error, field, code string) {
	t.Helper()
	wantKind(t, err, errs.Invalid)
	for _, fe := range validate.Fields(err) {
		if (field == "" || fe.Field == field) && (code == "" || fe.Code == code) {
			return
		}
	}
	t.Fatalf("no field error %q/%q in %v", field, code, err)
}

func paramOf(err error, field, code, key string) string {
	for _, fe := range validate.Fields(err) {
		if fe.Field == field && fe.Code == code {
			return fe.Params[key]
		}
	}
	return ""
}

// ---- sessions --------------------------------------------------------------------------

// The tests carry the signed-in actor on the context under their own key, so regional's
// tests need not import identity (regional never does).
type actorKey struct{}

func asPerson(ctx context.Context, p model.PersonID) context.Context {
	return context.WithValue(ctx, actorKey{}, authz.Actor{Person: p})
}

// asRestricted is a session that must change its password: identity reports no actor for it.
func asRestricted(ctx context.Context, _ model.PersonID) context.Context { return ctx }

// actorOf is the ActorFunc of the rigs.
func actorOf(ctx context.Context) (authz.Actor, bool) {
	a, ok := ctx.Value(actorKey{}).(authz.Actor)
	return a, ok
}

// ---- Options wiring --------------------------------------------------------------------

// revisionBox is the unit rigs' RevisionSource: a settings revision a test can move or break.
// Revise runs fn in a Write transaction of the fake database and bumps the revision when fn
// reports a change, as settings.Revise does.
type revisionBox struct {
	mu  sync.Mutex
	rev settings.Revision
	err error
	db  db.DB
}

var _ regional.RevisionSource = (*revisionBox)(nil)

func (b *revisionBox) Revision(context.Context) (settings.Revision, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.rev, b.err
}

func (b *revisionBox) Revise(ctx context.Context, fn func(context.Context) (bool, error)) (settings.Revision, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.err != nil {
		return 0, b.err
	}
	changed := false
	err := b.db.Transact(ctx, db.Write, func(ctx context.Context) error {
		var ferr error
		changed, ferr = fn(ctx)
		return ferr
	})
	if err != nil {
		return 0, err //nolint:wrapcheck // test double
	}
	if changed {
		b.rev++
	}
	return b.rev, nil
}

func (b *revisionBox) set(rev settings.Revision, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rev, b.err = rev, err
}

// termsBox is the unit rigs' TermSource: the defaults of one core term.
type termsBox struct{}

var _ regional.TermSource = termsBox{}

func (termsBox) TermsFor(context.Context, string) (map[string]settings.Forms, error) {
	return map[string]settings.Forms{"event": {Singular: "event", Plural: "events", SingularTitle: "Event", PluralTitle: "Events"}}, nil
}

// area is the "regional" core area app passes to settings.New.
func area() settings.Area {
	return settings.Area{Name: "regional", Core: true, Decls: regional.Settings}
}

// ---- the rig ---------------------------------------------------------------------------

// rig is a regional.Service over a fake database, filestest.Fake("settings") behind a spy,
// a settings.Reader fake for the "regional" area and a real settings.Service (built over the
// same fake database, never loaded) for whatever else Options takes from settings.
type rig struct {
	t     *testing.T
	svc   *regional.Service
	opts  regional.Options
	fake  *dbtest.Fake
	fs    *filestest.Fake
	spy   *spyStore
	rd    *settingstest.Fake
	stg   *settings.Service // the real settings service (the integration rigs read through it)
	rev   *revisionBox      // the unit rigs' revision; nil on a real database
	rec   *audittest.Fake
	az    *authztest.Fake
	clk   *clock.Fake
	cap   *telemetrytest.Capture
	admin model.PersonID // holds settings.manage
}

// baseOptions builds a valid regional.Options and the rig around it, without calling New.
func baseOptions(t *testing.T) (regional.Options, *rig) {
	t.Helper()
	fake := dbtest.NewFake()
	return baseOptionsOn(t, fake, fake, 0)
}

// baseOptionsOn is baseOptions over database d (fake is d when d is a *dbtest.Fake, else nil).
// With a real database (fake == nil) the Settings reader is the real settings.Service's, and
// refresh is that service's poll interval (0 = its default).
func baseOptionsOn(t *testing.T, d db.DB, fake *dbtest.Fake, refresh time.Duration) (regional.Options, *rig) {
	t.Helper()
	c := telemetrytest.New(t)
	clk := clock.NewFake(epoch)
	az := authztest.NewFake()
	az.SetActor(actorOf)
	fs := filestest.NewFake("settings")
	spy := &spyStore{Store: fs}
	rd := settingstest.NewFake("regional", regional.Settings)
	set, err := settings.New(settings.Options{
		DB: d, Clock: clk, RefreshInterval: refresh, Logger: c.Logger(), Tracer: c.Tracer("settings"), Meter: c.Meter("settings"),
		Audit: audittest.NewFake("settings"), Authz: az, Actor: actorOf,
		Areas: []settings.Area{area()}, SealingKey: testSealingKey,
	})
	if err != nil {
		t.Fatalf("settings.New: %v", err)
	}
	rec := audittest.NewFake("regional")
	r := &rig{t: t, fake: fake, fs: fs, spy: spy, rd: rd, stg: set, rec: rec, az: az, clk: clk, cap: c}
	r.admin = newPerson()
	az.Grant(r.admin, settings.PermissionManage)
	az.Grant(r.admin, settings.PermissionRead)
	var reader settings.Reader = rd
	if fake == nil {
		rr, err := set.Scoped("regional")
		if err != nil {
			t.Fatalf("settings.Scoped(regional): %v", err)
		}
		reader = rr
	}
	o := regional.Options{
		Clock: clk, Logger: c.Logger(), Tracer: c.Tracer("regional"),
		Audit: rec, Authz: az, Actor: actorOf, Settings: reader, Files: spy,
	}
	o.DB = d
	if fake == nil {
		o.Revision = set
		o.Terms = set
	} else {
		r.rev = &revisionBox{rev: 7, db: d}
		o.Revision = r.rev
		o.Terms = termsBox{}
	}
	return o, r
}

func newRig(t *testing.T, mods ...func(*regional.Options)) *rig {
	t.Helper()
	o, r := baseOptions(t)
	for _, m := range mods {
		m(&o)
	}
	return buildRig(t, o, r)
}

// buildRig calls New and finishes the rig.
func buildRig(t *testing.T, o regional.Options, r *rig) *rig {
	t.Helper()
	svc, err := regional.New(o)
	if err != nil {
		t.Fatalf("regional.New: %v", err)
	}
	r.svc, r.opts = svc, o
	return r
}

// person returns a new person holding perms site-wide.
func (r *rig) person(perms ...authz.Permission) model.PersonID {
	p := newPerson()
	for _, perm := range perms {
		r.az.Grant(p, perm)
	}
	return p
}

func (r *rig) actx() context.Context { return asPerson(context.Background(), r.admin) }

// noStatements fails when the use case ran a statement or recorded an audit entry.
func (r *rig) noStatements(t *testing.T, what string) {
	t.Helper()
	if n := len(r.fake.Calls()); n != 0 {
		t.Errorf("%d statements for %s", n, what)
	}
	if len(r.rec.Recorded()) != 0 {
		t.Errorf("audit entries %v for %s", actionsOf(r.rec.Recorded()), what)
	}
}

func actionsOf(es []audit.Entry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = string(e.Action)
	}
	return out
}

// ---- HTTP ------------------------------------------------------------------------------

type failRec struct {
	mu   sync.Mutex
	errs []error
}

func (f *failRec) Fail(w http.ResponseWriter, _ *http.Request, err error) {
	f.mu.Lock()
	f.errs = append(f.errs, err)
	f.mu.Unlock()
	w.WriteHeader(httpx.StatusOf(errs.KindOf(err)))
}

func (f *failRec) last() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.errs) == 0 {
		return nil
	}
	return f.errs[len(f.errs)-1]
}

// mux mounts every route under /api/regional, as app does.
func (r *rig) mux() *http.ServeMux {
	r.t.Helper()
	m := http.NewServeMux()
	for _, rt := range r.svc.Routes() {
		m.Handle(rt.Method+" /api/regional"+rt.Pattern, rt.Handler)
	}
	return m
}

func (r *rig) do(ctx context.Context, method, path, body string, hdr ...string) (*httptest.ResponseRecorder, *failRec) {
	r.t.Helper()
	f := &failRec{}
	ctx = httpx.WithFailer(ctx, f)
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequestWithContext(ctx, method, "/api/regional"+path, rd)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	w := httptest.NewRecorder()
	r.mux().ServeHTTP(w, req)
	return w, f
}

var _ = files.Brand // the logo helpers import files; keep the import graph obvious
