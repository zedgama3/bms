package people_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/contracts/personpage"
	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/audit/audittest"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/authz/authztest"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/signals/signalstest"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// Every person, address and phone number in this package is invented. The markers below
// are what the leak checks look for in logs, spans, metrics and audit entries.
const (
	testTimeout = 90 * time.Second

	mPlaya        = "Glimmer Wasp"
	mLegal        = "Zephyrine Marlowe-Quince"
	mPhone        = "+1 555 0177"
	mContactEmail = "marker.contact@example.org"
	mECName       = "Orville Tansy"
	mECPhone      = "+1 555 0188"
	mRelationship = "cousin once removed"
	mNote         = "private-note-marker-4471"
	mDecision     = "decision-note-marker-9023"

	permDirectory authz.Permission = "people.directory.read"
	permContacts  authz.Permission = "people.contacts.read"
	permManage    authz.Permission = "people.profiles.manage"
	permExport    authz.Permission = "people.data.export"
	permDelete    authz.Permission = "people.data.delete"

	sigCreated   signals.Name = "people.person-created"
	sigDeleted   signals.Name = "people.person-deleted"
	sigRequested signals.Name = "people.deletion-requested"
)

var epoch = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

// ctxT returns a bounded context for one test.
func ctxT(t testing.TB) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	t.Cleanup(cancel)
	return ctx
}

func newPerson() model.PersonID { return model.NewID[model.PersonID]() }

func str(s string) *string { return &s }

func repeat(s string, n int) string { return strings.Repeat(s, n) }

func mail(t testing.TB, s string) model.Email {
	t.Helper()
	e, err := model.ParseEmail(s)
	if err != nil {
		t.Fatalf("ParseEmail(%q): %v", s, err)
	}
	return e
}

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

// hasField reports whether err carries a field error for field with code ("" matches any).
func hasField(err error, field, code string) bool {
	for _, fe := range validate.Fields(err) {
		if (field == "" || fe.Field == field) && (code == "" || fe.Code == code) {
			return true
		}
	}
	return false
}

// wantField checks Invalid with a field error for field and code.
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

// signedIn remembers every person a test signed in with asPerson or asSuper: a session
// implies an account, so the in-memory Accounts fake answers EmailOf for them (the service
// refuses a session whose account is gone). Person IDs are random, so tests do not clash.
var signedIn sync.Map

// asPerson returns ctx as a request from the signed-in person p.
func asPerson(ctx context.Context, p model.PersonID) context.Context {
	signedIn.Store(p, true)
	return identity.WithSession(ctx, sessionOf(p, false, false))
}

func asSuper(ctx context.Context, p model.PersonID) context.Context {
	signedIn.Store(p, true)
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

// inDelivery returns a ctx a signal handler would see, by delivering a signal to a probe
// handler on a throw-away bus.
func inDelivery(t testing.TB) context.Context {
	t.Helper()
	probe, err := signalstest.NewFake("probe", nil, []signals.Name{identity.SignalAccountCreated})
	wantNoErr(t, err)
	var got context.Context
	wantNoErr(t, probe.Subscribe(identity.SignalAccountCreated, func(ctx context.Context, _ signals.Signal) error {
		got = ctx
		return nil
	}))
	wantNoErr(t, probe.Deliver(ctxT(t), accountCreatedSignal(model.NewID[model.AccountID](), newPerson())))
	if got == nil {
		t.Fatal("the probe handler did not run")
	}
	return got
}

func accountCreatedSignal(acct model.AccountID, p model.PersonID) signals.Signal {
	return signals.Signal{
		ID:   model.NewID[signals.SignalID](),
		Name: identity.SignalAccountCreated,
		At:   epoch,
		Payload: signals.NewPayload(
			signals.Ref(identity.PayloadAccount, acct),
			signals.Ref(identity.PayloadPerson, p),
		),
	}
}

// ---- fakes for the seams ---------------------------------------------------------------

// callLog records the order of calls across fakes.
type callLog struct {
	mu    sync.Mutex
	calls []string
}

func (l *callLog) add(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, s)
}

type fakeAccount struct {
	acct  model.AccountID
	email model.Email
	super bool
}

// fakeAccounts is a people.Accounts that keeps accounts in memory.
type fakeAccounts struct {
	mu        sync.Mutex
	log       *callLog
	byPerson  map[model.PersonID]fakeAccount
	exportOut json.RawMessage
	exportErr error
	deleteN   int
	deleteErr error
	linkErr   error
	deleted   []model.PersonID
	gone      map[model.PersonID]bool // accounts deleted: no longer signed in
}

var _ people.Accounts = (*fakeAccounts)(nil)

func newFakeAccounts(log *callLog) *fakeAccounts {
	return &fakeAccounts{log: log, byPerson: map[model.PersonID]fakeAccount{}, gone: map[model.PersonID]bool{}, exportOut: json.RawMessage(`{"account":null}`), deleteN: 1}
}

func (f *fakeAccounts) EmailOf(_ context.Context, p model.PersonID) (model.Email, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.byPerson[p]
	if !ok {
		if _, signed := signedIn.Load(p); signed && !f.gone[p] {
			return model.Email("session." + p.String()[:8] + "@example.org"), nil
		}
		return "", errs.E("test.fakeAccounts.EmailOf", errs.NotFound, errors.New("no account"))
	}
	return a.email, nil
}

func (f *fakeAccounts) LinkAccount(_ context.Context, email model.Email, p model.PersonID) (model.AccountID, model.PersonID, error) {
	const op = "test.fakeAccounts.LinkAccount"
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log.add("accounts.link")
	if f.linkErr != nil {
		return model.AccountID{}, model.PersonID{}, f.linkErr
	}
	if _, has := f.byPerson[p]; has {
		return model.AccountID{}, model.PersonID{}, errs.E(op, errs.Conflict, &validate.FieldError{Field: "person", Code: "has_account", Reason: "has an account"})
	}
	for old, a := range f.byPerson {
		if a.email.Canonical() != email.Canonical() {
			continue
		}
		if a.super {
			return model.AccountID{}, model.PersonID{}, errs.E(op, errs.Conflict, &validate.FieldError{Field: "email", Code: "superuser", Reason: "superuser"})
		}
		delete(f.byPerson, old)
		f.byPerson[p] = a
		return a.acct, old, nil
	}
	return model.AccountID{}, model.PersonID{}, errs.E(op, errs.NotFound, &validate.FieldError{Field: "email", Code: "unknown", Reason: "unknown"})
}

func (f *fakeAccounts) ExportPerson(context.Context, model.PersonID) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log.add("accounts.export")
	return f.exportOut, f.exportErr
}

func (f *fakeAccounts) DeletePerson(_ context.Context, p model.PersonID) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log.add("accounts.delete")
	if f.deleteErr != nil {
		return 0, f.deleteErr
	}
	f.deleted = append(f.deleted, p)
	delete(f.byPerson, p)
	f.gone[p] = true
	return f.deleteN, nil
}

// fakeAccess is a people.Access.
type fakeAccess struct {
	mu        sync.Mutex
	log       *callLog
	exportOut json.RawMessage
	exportErr error
	deleteN   int
	deleteErr error
}

var _ people.Access = (*fakeAccess)(nil)

func newFakeAccess(log *callLog) *fakeAccess {
	return &fakeAccess{log: log, exportOut: json.RawMessage(`{"grants":[]}`), deleteN: 2}
}

func (f *fakeAccess) ExportPerson(context.Context, model.PersonID) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log.add("access.export")
	return f.exportOut, f.exportErr
}

func (f *fakeAccess) DeletePerson(context.Context, model.PersonID) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log.add("access.delete")
	if f.deleteErr != nil {
		return 0, f.deleteErr
	}
	return f.deleteN, nil
}

// fakeCounter is a people.AuditCounter.
type fakeCounter struct {
	mu  sync.Mutex
	n   int
	err error
}

func (f *fakeCounter) CountAbout(context.Context, model.PersonID) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.n, f.err
}

// policySrc is a people.PolicySource a test can change.
type policySrc struct {
	mu  sync.Mutex
	p   people.Policy
	err error
}

func newPolicySrc() *policySrc { return &policySrc{p: people.DefaultPolicy()} }

func (s *policySrc) Policy(context.Context) (people.Policy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.p, s.err
}

// fakeSource is one module's PersonData.
type fakeSource struct {
	name      string
	log       *callLog
	data      json.RawMessage
	exportErr error
	del       func(ctx context.Context, p model.PersonID) (people.ModuleDeletion, error)
}

func (s *fakeSource) ExportPerson(context.Context, model.PersonID) (people.Export, error) {
	s.log.add("module." + s.name + ".export")
	if s.exportErr != nil {
		return people.Export{}, s.exportErr
	}
	return people.Export{Data: s.data}, nil
}

func (s *fakeSource) DeletePerson(ctx context.Context, p model.PersonID) (people.ModuleDeletion, error) {
	s.log.add("module." + s.name + ".delete")
	if s.del != nil {
		return s.del(ctx, p)
	}
	return people.ModuleDeletion{}, nil
}

func (s *fakeSource) source() people.Source { return people.Source{Name: s.name, Exporter: s} }

// providerFunc adapts a function to personpage.Provider.
type providerFunc func(ctx context.Context, ids []model.PersonID) (map[model.PersonID][]personpage.Section, error)

func (f providerFunc) Sections(ctx context.Context, ids []model.PersonID) (map[model.PersonID][]personpage.Section, error) {
	return f(ctx, ids)
}

// ---- the rig ---------------------------------------------------------------------------

// rig is a people.Service over the given DB with every seam faked.
type rig struct {
	t       *testing.T
	svc     *people.Service
	opts    people.Options
	clk     *clock.Fake
	rec     *audittest.Fake
	bus     *signalstest.Fake
	az      *authztest.Fake
	cap     *telemetrytest.Capture
	log     *callLog
	accts   *fakeAccounts
	access  *fakeAccess
	counter *fakeCounter
	pol     *policySrc
}

func peopleSignals() []signals.Name { return []signals.Name{sigCreated, sigDeleted, sigRequested} }

func validOptions(t *testing.T, d db.DB) (people.Options, *rig) {
	t.Helper()
	c := telemetrytest.New(t)
	clk := clock.NewFake(epoch)
	rec := audittest.NewFake("people")
	bus, err := signalstest.NewFake("people", peopleSignals(), []signals.Name{identity.SignalAccountCreated})
	wantNoErr(t, err)
	az := authztest.NewFake()
	az.SetActor(actorOf)
	log := &callLog{}
	r := &rig{
		t: t, clk: clk, rec: rec, bus: bus, az: az, cap: c, log: log,
		accts: newFakeAccounts(log), access: newFakeAccess(log), counter: &fakeCounter{}, pol: newPolicySrc(),
	}
	o := people.Options{
		DB:         d,
		Clock:      clk,
		Logger:     c.Logger(),
		Tracer:     c.Tracer("people"),
		Meter:      c.Meter("people"),
		Audit:      rec,
		Signals:    bus,
		Authz:      az,
		Actor:      actorOf,
		Session:    identity.SessionFrom,
		Accounts:   r.accts,
		Access:     r.access,
		AuditCount: r.counter,
		Policy:     r.pol,
	}
	return o, r
}

// newRig builds a Service over d; mods edit the Options before people.New.
func newRig(t *testing.T, d db.DB, mods ...func(*people.Options)) *rig {
	t.Helper()
	o, r := validOptions(t, d)
	for _, m := range mods {
		m(&o)
	}
	svc, err := people.New(o)
	if err != nil {
		t.Fatalf("people.New: %v", err)
	}
	r.svc, r.opts = svc, o
	return r
}

// link hands the service its sources (no page providers).
func (r *rig) link(sources ...people.Source) {
	r.t.Helper()
	wantNoErr(r.t, r.svc.Link(sources, nil, nil))
}

func actionsOf(es []audit.Entry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = string(e.Action)
	}
	return out
}

// ---- HTTP ------------------------------------------------------------------------------

// failRec is the Failer a handler test installs: it records the error handed to
// httpx.Fail and answers with the status the shell's mapper would.
type failRec struct {
	mu   sync.Mutex
	errs []error
}

func (f *failRec) Fail(w http.ResponseWriter, _ *http.Request, err error) {
	f.mu.Lock()
	f.errs = append(f.errs, err)
	f.mu.Unlock()
	status := httpx.StatusOf(errs.KindOf(err))
	if errors.Is(err, httpx.ErrRateLimited) {
		status = http.StatusTooManyRequests
	}
	w.WriteHeader(status)
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

// mux mounts every route of the service under /api/people, as app does.
func (r *rig) mux() *http.ServeMux {
	r.t.Helper()
	m := http.NewServeMux()
	for _, rt := range r.svc.Routes() {
		pattern := rt.Pattern
		if pattern == "/" {
			pattern = "/{$}"
		}
		m.Handle(rt.Method+" /api/people"+pattern, rt.Handler)
	}
	return m
}

// do runs one request through the mux with ctx and returns the response and the failures.
func (r *rig) do(ctx context.Context, method, path, body string) (*httptest.ResponseRecorder, *failRec) {
	r.t.Helper()
	f := &failRec{}
	ctx = httpx.WithFailer(ctx, f)
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequestWithContext(ctx, method, "/api/people"+path, rd)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.mux().ServeHTTP(w, req)
	return w, f
}

// keep returns err unchanged; closures that report a service's error use it so wrapcheck
// does not ask a test to wrap the very error it inspects.
func keep(err error) error { return err }
