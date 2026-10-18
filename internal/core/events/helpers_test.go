package events_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"        //nolint:depguard // failDB must implement db.DB, whose Query returns pgx.Rows
	"github.com/jackc/pgx/v5/pgconn" //nolint:depguard // failDB must implement db.DB, whose Exec returns pgconn.CommandTag

	"github.com/zedgama3/bms/internal/contracts/eventsetup"
	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/audit/audittest"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/authz/authztest"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/events"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/signals/signalstest"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// Every event, person and address in this package is invented.
const testTimeout = 90 * time.Second

var epoch = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

func ctxT(t testing.TB) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	t.Cleanup(cancel)
	return ctx
}

func newPerson() model.PersonID     { return model.NewID[model.PersonID]() }
func newEvent() model.EventID       { return model.NewID[model.EventID]() }
func str(s string) *string          { return &s }
func repeat(s string, n int) string { return strings.Repeat(s, n) }
func keep(err error) error          { return err }

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

func wantReadOnly(t testing.TB, err error) {
	t.Helper()
	wantKind(t, err, errs.Unavailable)
	if !errors.Is(err, db.ErrReadOnly) {
		t.Fatalf("error does not wrap db.ErrReadOnly: %v", err)
	}
}

func hasField(err error, field, code string) bool {
	for _, fe := range validate.Fields(err) {
		if (field == "" || fe.Field == field) && (code == "" || fe.Code == code) {
			return true
		}
	}
	return false
}

// wantField checks Invalid carrying a field error for field and code.
func wantField(t testing.TB, err error, field, code string) {
	t.Helper()
	wantKind(t, err, errs.Invalid)
	if !hasField(err, field, code) {
		t.Fatalf("no field error %q/%q in %v", field, code, validate.Fields(err))
	}
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

func sessionOf(p model.PersonID, super, restricted bool) identity.Session {
	return identity.Session{
		ID:                     model.NewID[identity.SessionID](),
		Account:                model.NewID[model.AccountID](),
		Person:                 p,
		Superuser:              super,
		PasswordChangeRequired: restricted,
		IssuedAt:               epoch,
		ExpiresAt:              epoch.Add(time.Hour),
	}
}

func asPerson(ctx context.Context, p model.PersonID) context.Context {
	return identity.WithSession(ctx, sessionOf(p, false, false))
}

func asSuper(ctx context.Context, p model.PersonID) context.Context {
	return identity.WithSession(ctx, sessionOf(p, true, false))
}

func asRestricted(ctx context.Context, p model.PersonID) context.Context {
	return identity.WithSession(ctx, sessionOf(p, false, true))
}

// actorOf is what app builds: identity.PersonFrom plus the superuser flag.
func actorOf(ctx context.Context) (authz.Actor, bool) {
	p, ok := identity.PersonFrom(ctx)
	if !ok {
		return authz.Actor{}, false
	}
	s, _ := identity.SessionFrom(ctx)
	return authz.Actor{Person: p, Superuser: s.Superuser}, true
}

// inDelivery returns a ctx a signal handler would see.
func inDelivery(t testing.TB) context.Context {
	t.Helper()
	probe, err := signalstest.NewFake("probe", nil, []signals.Name{identity.SignalAccountCreated})
	wantNoErr(t, err)
	var got context.Context
	wantNoErr(t, probe.Subscribe(identity.SignalAccountCreated, func(ctx context.Context, _ signals.Signal) error {
		got = ctx
		return nil
	}))
	sig := signals.Signal{
		ID:   model.NewID[signals.SignalID](),
		Name: identity.SignalAccountCreated,
		At:   epoch,
		Payload: signals.NewPayload(
			signals.Ref(identity.PayloadAccount, model.NewID[model.AccountID]()),
			signals.Ref(identity.PayloadPerson, newPerson()),
		),
	}
	wantNoErr(t, probe.Deliver(ctxT(t), sig))
	if got == nil {
		t.Fatal("the probe handler did not run")
	}
	return got
}

// ---- wrappers that inject failures ------------------------------------------------------

// gateRec fails Record for entries whose action is in failOn (all when failOn is empty and
// failAll is set), and otherwise passes through.
type gateRec struct {
	audit.Recorder
	mu     sync.Mutex
	err    error
	failOn map[audit.Action]bool
}

func (g *gateRec) Record(ctx context.Context, entries ...audit.Entry) error {
	g.mu.Lock()
	err, on := g.err, g.failOn
	g.mu.Unlock()
	for _, e := range entries {
		if on[e.Action] {
			return err
		}
	}
	return g.Recorder.Record(ctx, entries...) //nolint:wrapcheck // test double
}

// failBus makes Publish fail.
type failBus struct {
	signals.Bus
	mu  sync.Mutex
	err error
}

func (b *failBus) Publish(ctx context.Context, n signals.Name, p signals.Payload) error {
	b.mu.Lock()
	err := b.err
	b.mu.Unlock()
	if err != nil {
		return err
	}
	return b.Bus.Publish(ctx, n, p) //nolint:wrapcheck // test double
}

// failDB fails every statement and transaction with err.
type failDB struct{ err error }

func (f failDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, f.err
}
func (f failDB) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, f.err }
func (f failDB) QueryRow(context.Context, string, ...any) pgx.Row        { return failRow(f) }
func (f failDB) Transact(context.Context, db.Mode, func(context.Context) error) error {
	return f.err
}
func (failDB) ReadOnly() bool { return false }

type failRow struct{ err error }

func (r failRow) Scan(...any) error { return r.err }

// ---- the rig ---------------------------------------------------------------------------

func published() []signals.Name { return events.PublishedSignals }

// rig is a events.Service over a DB with every seam faked.
type rig struct {
	t    *testing.T
	svc  *events.Service
	opts events.Options
	clk  *clock.Fake
	rec  *audittest.Fake
	gate *gateRec
	bus  *signalstest.Fake
	fbus *failBus
	az   *authztest.Fake
	cap  *telemetrytest.Capture
}

func validOptions(t *testing.T, d db.DB) (events.Options, *rig) {
	t.Helper()
	c := telemetrytest.New(t)
	clk := clock.NewFake(epoch)
	rec := audittest.NewFake("events")
	gate := &gateRec{Recorder: rec}
	bus, err := signalstest.NewFake("events", published(), nil)
	wantNoErr(t, err)
	fbus := &failBus{Bus: bus}
	az := authztest.NewFake()
	az.SetActor(actorOf)
	r := &rig{t: t, clk: clk, rec: rec, gate: gate, bus: bus, fbus: fbus, az: az, cap: c}
	o := events.Options{
		DB:      d,
		Clock:   clk,
		Logger:  c.Logger(),
		Tracer:  c.Tracer("events"),
		Meter:   c.Meter("events"),
		Audit:   gate,
		Signals: fbus,
		Authz:   az,
		Actor:   actorOf,
	}
	return o, r
}

func newRig(t *testing.T, d db.DB, mods ...func(*events.Options)) *rig {
	t.Helper()
	o, r := validOptions(t, d)
	for _, m := range mods {
		m(&o)
	}
	svc, err := events.New(o)
	if err != nil {
		t.Fatalf("events.New: %v", err)
	}
	r.svc, r.opts = svc, o
	return r
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

func (f *failRec) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.errs)
}

// mux mounts every route under /api/events, as app does.
func (r *rig) mux() *http.ServeMux {
	r.t.Helper()
	m := http.NewServeMux()
	for _, rt := range r.svc.Routes() {
		pattern := rt.Pattern
		if pattern == "/" {
			pattern = "/{$}"
		}
		m.Handle(rt.Method+" /api/events"+pattern, rt.Handler)
	}
	return m
}

func (r *rig) do(ctx context.Context, method, path, body string) (*httptest.ResponseRecorder, *failRec) {
	r.t.Helper()
	f := &failRec{}
	ctx = httpx.WithFailer(ctx, f)
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequestWithContext(ctx, method, "/api/events"+path, rd)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.mux().ServeHTTP(w, req)
	return w, f
}

// ---- events-specific helpers ------------------------------------------------------------

// link hands the service its providers (none by default).
func (r *rig) link(ps ...events.SetupProvider) {
	r.t.Helper()
	if ps == nil {
		ps = []events.SetupProvider{}
	}
	wantNoErr(r.t, r.svc.Link(ps))
}

// validDates is a three-day event starting 30 days after the epoch.
func validDates() model.TimeRange {
	start := epoch.AddDate(0, 0, 30)
	return model.TimeRange{Start: start, End: start.AddDate(0, 0, 3)}
}

const validZone = "America/Los_Angeles"

func validCreate(name string) events.CreateInput {
	return events.CreateInput{Name: name, Kind: events.KindBurn, Dates: validDates(), Zone: validZone}
}

// callLog records the order of provider calls.
type callLog struct {
	mu    sync.Mutex
	calls []string
}

func (l *callLog) add(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, s)
}

// fakeProvider is an eventsetup.Provider that records its calls and runs fn.
type fakeProvider struct {
	module string
	log    *callLog
	mu     sync.Mutex
	reqs   []eventsetup.Request
	ctxs   []context.Context
	fn     func(ctx context.Context, req eventsetup.Request) (eventsetup.Result, error)
}

func (p *fakeProvider) SetUp(ctx context.Context, req eventsetup.Request) (eventsetup.Result, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	p.ctxs = append(p.ctxs, ctx)
	p.mu.Unlock()
	if p.log != nil {
		p.log.add(p.module)
	}
	if p.fn == nil {
		return eventsetup.Result{Items: 1}, nil
	}
	return p.fn(ctx, req)
}

func (p *fakeProvider) sp() events.SetupProvider {
	return events.SetupProvider{Module: p.module, Provider: p}
}

func provider(module string, log *callLog, items int) *fakeProvider {
	return &fakeProvider{module: module, log: log, fn: func(context.Context, eventsetup.Request) (eventsetup.Result, error) {
		return eventsetup.Result{Items: items}, nil
	}}
}

func newDept() model.DepartmentID { return model.NewID[model.DepartmentID]() }

func validateFieldsOf(err error) []*validate.FieldError { return validate.Fields(err) }
