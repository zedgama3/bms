package datarights_test

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

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/audit/audittest"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/authz/authztest"
	"github.com/zedgama3/bms/internal/core/datarights"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/notify/notifytest"
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
	// The permission names do not change with the move (boundaries decision 12).
	permDirectory authz.Permission = "people.directory.read"
	permContacts  authz.Permission = "people.contacts.read"
	permManage    authz.Permission = "people.profiles.manage"
	permExport    authz.Permission = "people.data.export"
	permDelete    authz.Permission = "people.data.delete"
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

// ---- sessions --------------------------------------------------------------------------

func sessionOf(p model.PersonID, super bool) identity.Session {
	return identity.Session{
		ID:        model.NewID[identity.SessionID](),
		Account:   model.NewID[model.AccountID](),
		Person:    p,
		Superuser: super,
		IssuedAt:  epoch,
		ExpiresAt: epoch.Add(time.Hour),
	}
}

// signedIn remembers every person a test signed in with asPerson or asSuper: a session
// implies an account, so the in-memory Accounts fake knows them (the account exists while
// the person has a session). Person IDs are random, so tests do not clash.
var signedIn sync.Map

// asPerson returns ctx as a request from the signed-in person p.
func asPerson(ctx context.Context, p model.PersonID) context.Context {
	signedIn.Store(p, true)
	return identity.WithSession(ctx, sessionOf(p, false))
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

func (l *callLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.calls...)
}

// fakeRaw is a datarights.RawSource (access, notify's outbox) that logs "<name>.export"
// and "<name>.delete".
type fakeRaw struct {
	mu        sync.Mutex
	name      string
	log       *callLog
	exportOut json.RawMessage
	exportErr error
	deleteN   int
	deleteErr error
}

var _ datarights.RawSource = (*fakeRaw)(nil)

func newFakeRaw(name string, log *callLog, exportOut string, deleteN int) *fakeRaw {
	return &fakeRaw{name: name, log: log, exportOut: json.RawMessage(exportOut), deleteN: deleteN}
}

func (f *fakeRaw) ExportPerson(context.Context, model.PersonID) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log.add(f.name + ".export")
	return f.exportOut, f.exportErr
}

func (f *fakeRaw) DeletePerson(context.Context, model.PersonID) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log.add(f.name + ".delete")
	if f.deleteErr != nil {
		return 0, f.deleteErr
	}
	return f.deleteN, nil
}

type fakeAccount struct {
	email model.Email
}

// fakeAccounts is identity as the tests need it: a datarights.RawSource named "accounts"
// that also keeps accounts in memory and answers EmailOf and LinkAccount, so the same fake
// serves as people.Accounts. (The spec does not say how datarights learns that a person
// with no profile row has an account, F58; the fake answers EmailOf in case it asks.)
type fakeAccounts struct {
	mu        sync.Mutex
	log       *callLog
	byPerson  map[model.PersonID]fakeAccount
	exportOut json.RawMessage
	exportErr error
	deleteN   int
	deleteErr error
	deleted   []model.PersonID
	gone      map[model.PersonID]bool // accounts deleted: no longer signed in
}

var _ datarights.RawSource = (*fakeAccounts)(nil)

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

func (f *fakeAccounts) LinkAccount(context.Context, model.Email, model.PersonID) (model.AccountID, model.PersonID, error) {
	return model.AccountID{}, model.PersonID{}, errs.E("test.fakeAccounts.LinkAccount", errs.NotFound, errors.New("not used by these tests"))
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

// fakeFiles is a datarights.FilesSource.
type fakeFiles struct {
	mu        sync.Mutex
	log       *callLog
	exportOut json.RawMessage
	exportErr error
	del       files.Deletion
	deleteErr error
}

var _ datarights.FilesSource = (*fakeFiles)(nil)

func (f *fakeFiles) ExportPerson(context.Context, model.PersonID) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log.add("files.export")
	return f.exportOut, f.exportErr
}

func (f *fakeFiles) DeletePerson(context.Context, model.PersonID) (files.Deletion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log.add("files.delete")
	return f.del, f.deleteErr
}

// fakeAuditLog is the AuditLog seam: it counts the entries about a person and exports none.
type fakeAuditLog struct {
	mu  sync.Mutex
	n   int
	err error
}

func (f *fakeAuditLog) CountAbout(context.Context, model.PersonID) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.n, f.err
}

func (f *fakeAuditLog) Export(context.Context, model.PersonID, io.Writer) (int, error) {
	return 0, nil
}

// stubHolders is the HolderSource the test controls.
type stubHolders struct {
	who []model.PersonID
	err error
}

func (h *stubHolders) HoldersOf(context.Context, authz.Permission) ([]model.PersonID, error) {
	return h.who, h.err
}

// fakeSource is one module's PersonData.
type fakeSource struct {
	name      string
	log       *callLog
	data      json.RawMessage
	exportErr error
	del       func(ctx context.Context, p model.PersonID) (datarights.Deletion, error)
}

var _ datarights.PersonData = (*fakeSource)(nil)

func (s *fakeSource) ExportPerson(context.Context, model.PersonID) (datarights.Export, error) {
	s.log.add("module." + s.name + ".export")
	if s.exportErr != nil {
		return datarights.Export{}, s.exportErr
	}
	return datarights.Export{Data: s.data}, nil
}

func (s *fakeSource) DeletePerson(ctx context.Context, p model.PersonID) (datarights.Deletion, error) {
	s.log.add("module." + s.name + ".delete")
	if s.del != nil {
		return s.del(ctx, p)
	}
	return datarights.Deletion{}, nil
}

func (s *fakeSource) source() datarights.Source {
	return datarights.Source{Name: s.name, PersonData: s}
}

// fakePeople is a datarights.PeopleSource that keeps no rows: it logs "people.export" and
// "people.delete" (Exists is not logged) and answers from its fields.
type fakePeople struct {
	mu        sync.Mutex
	log       *callLog
	exportOut json.RawMessage
	exportErr error
	deleteN   int
	deleteErr error
	existsErr error
}

var _ datarights.PeopleSource = (*fakePeople)(nil)

func (f *fakePeople) ExportPerson(context.Context, model.PersonID) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log.add("people.export")
	return f.exportOut, f.exportErr
}

func (f *fakePeople) DeletePerson(context.Context, model.PersonID) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log.add("people.delete")
	return f.deleteN, f.deleteErr
}

func (f *fakePeople) Exists(context.Context, model.PersonID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.existsErr
}

// ---- the rig ---------------------------------------------------------------------------

// rig is a datarights.Service over the given DB with every seam faked.
type rig struct {
	t       *testing.T
	svc     *datarights.Service
	opts    datarights.Options
	clk     *clock.Fake
	rec     *audittest.Fake
	bus     *signalstest.Fake
	az      *authztest.Fake
	cap     *telemetrytest.Capture
	log     *callLog
	accts   *fakeAccounts
	access  *fakeRaw
	outbox  *fakeRaw
	files   *fakeFiles
	counter *fakeAuditLog
	holders *stubHolders
	nf      *notifytest.Fake
}

// validOptions returns a complete Options over d with people as the PeopleSource, and the
// rig that holds the fakes. A nil people gets a fakePeople.
func validOptions(t *testing.T, d db.DB, people datarights.PeopleSource) (datarights.Options, *rig) {
	t.Helper()
	c := telemetrytest.New(t)
	clk := clock.NewFake(epoch)
	rec := audittest.NewFake("datarights")
	bus, err := signalstest.NewFake("datarights", datarights.PublishedSignals, nil)
	wantNoErr(t, err)
	az := authztest.NewFake()
	az.SetActor(actorOf)
	log := &callLog{}
	r := &rig{
		t: t, clk: clk, rec: rec, bus: bus, az: az, cap: c, log: log,
		accts:   newFakeAccounts(log),
		access:  newFakeRaw("access", log, `{"grants":[]}`, 2),
		outbox:  newFakeRaw("notify", log, `{"outbox":[]}`, 0),
		files:   &fakeFiles{log: log, exportOut: json.RawMessage(`{"files":[]}`)},
		counter: &fakeAuditLog{},
		holders: &stubHolders{who: []model.PersonID{newPerson(), newPerson()}},
		nf:      notifytest.NewFake("datarights"),
	}
	if people == nil {
		people = &fakePeople{log: log, exportOut: json.RawMessage(`{}`), deleteN: 1}
	}
	o := datarights.Options{
		DB:       d,
		Clock:    clk,
		Logger:   c.Logger(),
		Tracer:   c.Tracer("datarights"),
		Meter:    c.Meter("datarights"),
		Audit:    rec,
		Signals:  bus,
		Authz:    az,
		Actor:    actorOf,
		Holders:  r.holders,
		Notify:   r.nf,
		AuditLog: r.counter,
		Accounts: r.accts,
		Access:   r.access,
		Outbox:   r.outbox,
		Files:    r.files,
		People:   people,
	}
	return o, r
}

// newRig builds a Service over d with a fakePeople; mods edit the Options before New.
func newRig(t *testing.T, d db.DB, mods ...func(*datarights.Options)) *rig {
	t.Helper()
	o, r := validOptions(t, d, nil)
	for _, m := range mods {
		m(&o)
	}
	svc, err := datarights.New(o)
	if err != nil {
		t.Fatalf("datarights.New: %v", err)
	}
	r.svc, r.opts = svc, o
	return r
}

// link hands the service its module sources.
func (r *rig) link(sources ...datarights.Source) {
	r.t.Helper()
	wantNoErr(r.t, r.svc.Link(sources))
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

// mux mounts every route of the service under /api/datarights, as app does.
func (r *rig) mux() *http.ServeMux {
	r.t.Helper()
	m := http.NewServeMux()
	for _, rt := range r.svc.Routes() {
		m.Handle(rt.Method+" /api/datarights"+rt.Pattern, rt.Handler)
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
	req := httptest.NewRequestWithContext(ctx, method, "/api/datarights"+path, rd)
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

// ---- contexts a use case must refuse ----------------------------------------------------

// asRestricted is a session that must change its password first: no actor.
func asRestricted(ctx context.Context, p model.PersonID) context.Context {
	s := sessionOf(p, false)
	s.PasswordChangeRequired = true
	return identity.WithSession(ctx, s)
}

// inDelivery returns a ctx a signal handler would see, by delivering a signal to a probe
// handler on a throw-away bus: it carries no actor.
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
