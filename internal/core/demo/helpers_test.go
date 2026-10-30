package demo_test

// Fakes and builders shared by the unit and integration tests of core/demo. Everything is
// written from plan/design/demo.md (D53), not from the implementation. All people,
// addresses and numbers are invented (the .invalid domain, 555-01xx phones).

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"        //nolint:depguard // faultDB must implement db.DB
	"github.com/jackc/pgx/v5/pgconn" //nolint:depguard // faultDB must implement db.DB
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/contracts/demoseed"
	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/audit/audittest"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/demo"
	"github.com/zedgama3/bms/internal/core/departments"
	"github.com/zedgama3/bms/internal/core/events"
	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

const testTimeout = 90 * time.Second

// seedDay is a fixed Monday; the first Thursday at least 21 days later is 2027-01-28... the
// tests compute it from the spec instead of hard-coding it.
var seedDay = time.Date(2026, 10, 26, 15, 30, 0, 0, time.UTC)

func ctxT(t testing.TB) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	t.Cleanup(cancel)
	return ctx
}

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

// ---- the call log ----------------------------------------------------------------------

// call is one service call a fake seam saw.
type call struct {
	Seam, Method string
	Person       model.PersonID // identity.PersonFrom(ctx): who acted; zero for no session
	Detail       string         // a stable text of the interesting input
}

type callLog struct {
	mu    sync.Mutex
	calls []call
}

func (l *callLog) add(ctx context.Context, seam, method, detail string) {
	p, _ := identity.PersonFrom(ctx)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, call{Seam: seam, Method: method, Person: p, Detail: detail})
}

func (l *callLog) all() []call {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]call(nil), l.calls...)
}

func (l *callLog) count() int { return len(l.all()) }

// ---- fake seams ------------------------------------------------------------------------

// fakeAccounts is demo.Accounts. When rows is set it inserts the person and account rows the
// core.demo_account foreign keys need; err fails every call.
type fakeAccounts struct {
	log  *callLog
	rows db.DB
	mu   sync.Mutex
	byEm map[string]identity.AccountResult
	err  func(email string) error
}

func (f *fakeAccounts) EnsureAccount(ctx context.Context, c identity.Credentials) (identity.AccountResult, error) {
	f.log.add(ctx, "accounts", "EnsureAccount", string(c.Email)+"|"+c.Password)
	if f.err != nil {
		if err := f.err(string(c.Email)); err != nil {
			return identity.AccountResult{}, err
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.byEm == nil {
		f.byEm = map[string]identity.AccountResult{}
	}
	if r, ok := f.byEm[string(c.Email)]; ok {
		r.Created = false
		return r, nil
	}
	r := identity.AccountResult{Account: model.NewID[model.AccountID](), Person: model.NewID[model.PersonID](), Created: true}
	f.byEm[string(c.Email)] = r
	if f.rows != nil {
		now := time.Now()
		if _, err := f.rows.Exec(ctx, `INSERT INTO core.person (id, created_at, updated_at) VALUES ($1, $2, $2)`, [16]byte(r.Person), now); err != nil {
			return identity.AccountResult{}, err //nolint:wrapcheck // test double
		}
		if _, err := f.rows.Exec(ctx, `INSERT INTO core.account (id, person_id, email, password_hash, password_changed_at, created_at, updated_at)
			VALUES ($1, $2, $3, 'x', $4, $4, $4)`, [16]byte(r.Account), [16]byte(r.Person), string(c.Email), now); err != nil {
			return identity.AccountResult{}, err //nolint:wrapcheck // test double
		}
	}
	return r, nil
}

type fakeAccess struct {
	log      *callLog
	roles    []authz.Role
	grantErr func(in authz.GrantInput) error
	mu       sync.Mutex
	grants   []authz.GrantInput
	ensured  []authz.GrantInput
}

var starterRoleKeys = []string{
	authz.RoleAccessAdmin, authz.RoleAccountAdmin, authz.RoleAuditReviewer, authz.RoleDepartmentAdmin,
	authz.RoleDepartmentSteward, authz.RoleEventAdmin, authz.RoleSettingsAdmin, authz.RoleVolunteerCoordinator,
}

func newFakeAccess(log *callLog, missing ...string) *fakeAccess {
	f := &fakeAccess{log: log}
	for _, k := range starterRoleKeys {
		skip := false
		for _, m := range missing {
			skip = skip || m == k
		}
		if !skip {
			f.roles = append(f.roles, authz.Role{ID: model.NewID[authz.RoleID](), Key: k, Name: k})
		}
	}
	return f
}

func (f *fakeAccess) roleKey(id authz.RoleID) string {
	for _, r := range f.roles {
		if r.ID == id {
			return r.Key
		}
	}
	return "?"
}

func (f *fakeAccess) EnsureGrant(ctx context.Context, in authz.GrantInput) (authz.Grant, bool, error) {
	f.log.add(ctx, "access", "EnsureGrant", f.roleKey(in.Role))
	f.mu.Lock()
	f.ensured = append(f.ensured, in)
	f.mu.Unlock()
	return authz.Grant{ID: model.NewID[authz.GrantID](), Person: in.Person, Role: in.Role}, true, nil
}

func (f *fakeAccess) Grant(ctx context.Context, in authz.GrantInput) (authz.Grant, error) {
	f.log.add(ctx, "access", "Grant", f.roleKey(in.Role))
	if f.grantErr != nil {
		if err := f.grantErr(in); err != nil {
			return authz.Grant{}, err
		}
	}
	f.mu.Lock()
	f.grants = append(f.grants, in)
	f.mu.Unlock()
	return authz.Grant{ID: model.NewID[authz.GrantID](), Person: in.Person, Role: in.Role, Department: in.Department}, nil
}

func (f *fakeAccess) ListRoles(ctx context.Context) ([]authz.Role, error) {
	f.log.add(ctx, "access", "ListRoles", "")
	return append([]authz.Role(nil), f.roles...), nil
}

// StarterRoleIDs is authz's actor-less lookup: NotFound for a key that names no starter role.
func (f *fakeAccess) StarterRoleIDs(ctx context.Context, keys []string) (map[string]authz.RoleID, error) {
	f.log.add(ctx, "access", "StarterRoleIDs", strings.Join(keys, ","))
	out := map[string]authz.RoleID{}
	for _, k := range keys {
		found := false
		for _, r := range f.roles {
			if r.Key == k {
				out[k], found = r.ID, true
			}
		}
		if !found {
			return nil, errs.E("test.StarterRoleIDs", errs.NotFound, fmt.Errorf("no starter role %q", k))
		}
	}
	return out, nil
}

// fakeAddresses is demo.Addresses: person to login address.
type fakeAddresses struct {
	mu  sync.Mutex
	m   map[model.PersonID]model.Email
	err error
}

func (f *fakeAddresses) set(p model.PersonID, e string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.m == nil {
		f.m = map[model.PersonID]model.Email{}
	}
	f.m[p] = model.Email(e)
}

func (f *fakeAddresses) EmailOf(_ context.Context, p model.PersonID) (model.Email, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return "", f.err
	}
	e, ok := f.m[p]
	if !ok {
		return "", errs.E("test.fakeAddresses.EmailOf", errs.NotFound, errors.New("no account"))
	}
	return e, nil
}

type fakeProfiles struct {
	log *callLog
	err func(method string, ctxPerson model.PersonID) error
	mu  sync.Mutex
	// inputs by method, in call order.
	updates []people.ProfileInput
	creates []people.ProfileInput
}

func (f *fakeProfiles) UpdateMe(ctx context.Context, in people.ProfileInput) (people.Person, error) {
	p, _ := identity.PersonFrom(ctx)
	f.log.add(ctx, "profiles", "UpdateMe", deref(in.PlayaName))
	if f.err != nil {
		if err := f.err("UpdateMe", p); err != nil {
			return people.Person{}, err
		}
	}
	f.mu.Lock()
	f.updates = append(f.updates, in)
	f.mu.Unlock()
	return people.Person{ID: p}, nil
}

func (f *fakeProfiles) Create(ctx context.Context, in people.ProfileInput) (people.Person, error) {
	f.log.add(ctx, "profiles", "Create", deref(in.PlayaName))
	if f.err != nil {
		p, _ := identity.PersonFrom(ctx)
		if err := f.err("Create", p); err != nil {
			return people.Person{}, err
		}
	}
	f.mu.Lock()
	f.creates = append(f.creates, in)
	f.mu.Unlock()
	return people.Person{ID: model.NewID[model.PersonID]()}, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

type fakeDepartments struct {
	log *callLog
	mu  sync.Mutex
	in  []departments.CreateInput
	ids []model.DepartmentID
}

func (f *fakeDepartments) Create(ctx context.Context, in departments.CreateInput) (departments.Department, error) {
	f.log.add(ctx, "departments", "Create", in.Name)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.in = append(f.in, in)
	d := departments.Department{ID: model.NewID[model.DepartmentID](), Name: in.Name, Description: in.Description, ContactEmail: model.Email(in.ContactEmail)}
	f.ids = append(f.ids, d.ID)
	return d, nil
}

type fakeEvents struct {
	log      *callLog
	mu       sync.Mutex
	created  []events.CreateInput
	schedule []events.ScheduleInput
	id       model.EventID
}

func (f *fakeEvents) Create(ctx context.Context, in events.CreateInput) (events.Created, error) {
	f.log.add(ctx, "events", "Create", in.Name)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, in)
	f.id = model.NewID[model.EventID]()
	return events.Created{Event: events.Event{ID: f.id}}, nil
}

func (f *fakeEvents) AddScheduleItem(ctx context.Context, _ model.EventID, in events.ScheduleInput) (events.ScheduleItem, error) {
	f.log.add(ctx, "events", "AddScheduleItem", in.Key)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.schedule = append(f.schedule, in)
	return events.ScheduleItem{}, nil
}

type fakeSettings struct {
	log *callLog
	mu  sync.Mutex
	set []settings.Changes
}

func (f *fakeSettings) Set(ctx context.Context, ch settings.Changes) (settings.Revision, error) {
	f.log.add(ctx, "settings", "Set", "")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.set = append(f.set, ch)
	return 1, nil
}

type fakeSchema struct {
	log *callLog
	err error
	fn  func(ctx context.Context) error // runs the real migration in reset tests
}

func (f *fakeSchema) Migrate(ctx context.Context) error {
	f.log.add(ctx, "schema", "Migrate", "")
	if f.err != nil {
		return f.err
	}
	if f.fn != nil {
		return f.fn(ctx)
	}
	return nil
}

type fakeObjects struct {
	log *callLog
	err error
}

func (f *fakeObjects) Clear(ctx context.Context) (int, error) {
	f.log.add(ctx, "objects", "Clear", "")
	if f.err != nil {
		return 0, f.err
	}
	return 3, nil
}

// fakePolicy is the inner files.PolicySource.
type fakePolicy struct {
	p   files.Policy
	err error
}

func (f fakePolicy) Policy(context.Context) (files.Policy, error) { return f.p, f.err }

// fakeProvider is a demoseed.Provider that records what it was given.
type fakeProvider struct {
	mu    sync.Mutex
	reqs  []demoseed.Request
	fn    func(ctx context.Context, req demoseed.Request) (demoseed.Result, error)
	calls int
}

func (f *fakeProvider) Seed(ctx context.Context, req demoseed.Request) (demoseed.Result, error) {
	f.mu.Lock()
	f.calls++
	f.reqs = append(f.reqs, req)
	f.mu.Unlock()
	if f.fn != nil {
		return f.fn(ctx, req)
	}
	return demoseed.Result{Items: 1}, nil
}

// ---- the rig ---------------------------------------------------------------------------

type rig struct {
	t    *testing.T
	svc  *demo.Service
	opts demo.Options
	clk  *clock.Fake
	rec  *audittest.Fake
	cap  *telemetrytest.Capture
	log  *callLog

	accounts    *fakeAccounts
	access      *fakeAccess
	addresses   *fakeAddresses
	profiles    *fakeProfiles
	departments *fakeDepartments
	events      *fakeEvents
	settings    *fakeSettings
	schema      *fakeSchema
	objects     *fakeObjects
}

// actorFromSession is the ActorFunc app writes: a session in ctx, no I/O.
func actorFromSession(ctx context.Context) (authz.Actor, bool) {
	s, ok := identity.SessionFrom(ctx)
	if !ok || s.PasswordChangeRequired {
		return authz.Actor{}, false
	}
	return authz.Actor{Person: s.Person, Superuser: s.Superuser}, true
}

// asPerson returns ctx carrying a valid session for the person.
func asPerson(ctx context.Context, p model.PersonID, superuser bool) context.Context {
	now := seedDay
	return identity.WithSession(ctx, identity.Session{
		ID: model.NewID[identity.SessionID](), Account: model.NewID[model.AccountID](), Person: p, Superuser: superuser,
		IssuedAt: now, ExpiresAt: now.Add(time.Hour),
	})
}

// newRig builds a Service over d with fakes for every seam. Enabled is true unless a mod
// changes it.
func newRig(t *testing.T, d db.DB, mods ...func(*demo.Options)) *rig {
	t.Helper()
	log := &callLog{}
	c := telemetrytest.New(t)
	r := &rig{
		t: t, clk: clock.NewFake(seedDay), rec: audittest.NewFake("demo"), cap: c, log: log,
		accounts: &fakeAccounts{log: log}, access: newFakeAccess(log), addresses: &fakeAddresses{},
		profiles: &fakeProfiles{log: log}, departments: &fakeDepartments{log: log}, events: &fakeEvents{log: log},
		settings: &fakeSettings{log: log}, schema: &fakeSchema{log: log}, objects: &fakeObjects{log: log},
	}
	o := demo.Options{
		Enabled:            true,
		DB:                 d,
		Clock:              r.clk,
		Logger:             c.Logger(),
		Tracer:             c.Tracer("demo"),
		Meter:              c.Meter("demo"),
		Audit:              r.rec,
		Actor:              actorFromSession,
		Files:              fakePolicy{p: files.DefaultPolicy()},
		Accounts:           r.accounts,
		Access:             r.access,
		Addresses:          r.addresses,
		Profiles:           r.profiles,
		Departments:        r.departments,
		Events:             r.events,
		Settings:           r.settings,
		Schema:             r.schema,
		Objects:            r.objects,
		Schemas:            []string{"core"},
		DataDir:            t.TempDir(),
		ContributorTimeout: demo.DefaultContributorTimeout,
	}
	for _, m := range mods {
		m(&o)
	}
	svc, err := demo.New(o)
	if err != nil {
		t.Fatalf("demo.New: %v", err)
	}
	r.svc, r.opts = svc, o
	return r
}

// stepNames lists a report's step names.
func stepNames(rep demo.Report) []string {
	var out []string
	for _, s := range rep.Steps {
		out = append(out, s.Name)
	}
	return out
}

var _ io.Writer = io.Discard

var actorKeys = []string{
	demoseed.ActorParticipant, demoseed.ActorNewcomer, demoseed.ActorLead, demoseed.ActorCoordinator,
	demoseed.ActorSteward, demoseed.ActorEventAdmin, demoseed.ActorSettingsAdmin, demoseed.ActorAccountAdmin,
	demoseed.ActorAccessAdmin, demoseed.ActorAuditReviewer,
}

var _ = audit.ActorOperator

// faultDB makes every read, write and transaction fail with err.
type faultDB struct {
	db.DB
	err error
}

func (f *faultDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, f.err
}

func (f *faultDB) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, f.err }

func (f *faultDB) QueryRow(context.Context, string, ...any) pgx.Row { return errRow{f.err} }

func (f *faultDB) Transact(context.Context, db.Mode, func(context.Context) error) error { return f.err }

type errRow struct{ err error }

func (r errRow) Scan(...any) error { return r.err }
