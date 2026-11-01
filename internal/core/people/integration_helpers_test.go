//go:build integration

package people_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"        //nolint:depguard // spyDB must implement db.DB, whose Query returns pgx.Rows
	"github.com/jackc/pgx/v5/pgconn" //nolint:depguard // spyDB must implement db.DB, whose Exec returns pgconn.CommandTag

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/audit/audittest"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/core/people/postgres"
	"github.com/zedgama3/bms/internal/core/settings/settingstest"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/signals/signalstest"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

const (
	sessionKeyText = "people-test-session-key-0123456789abcdef-xyz"
	pwGood         = "tangerine-orbit-quiet-77"
)

// peopleSet is the migration set of this package, built from its migrations.
func peopleSet() db.Set {
	return db.Set{Name: "people", Schema: "core", Files: postgres.Migrations}
}

// spyDB counts what the service runs against the real pool.
type spyDB struct {
	db.DB
	mu      sync.Mutex
	stmts   int
	writeTx int
	readTx  int
}

type spyCounts struct{ Stmts, WriteTx, ReadTx int }

func (s *spyDB) bump(f func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f()
}

func (s *spyDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	s.bump(func() { s.stmts++ })
	return s.DB.Exec(ctx, sql, args...) //nolint:wrapcheck // a test spy passes errors through unchanged
}

func (s *spyDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	s.bump(func() { s.stmts++ })
	return s.DB.Query(ctx, sql, args...) //nolint:wrapcheck // a test spy passes errors through unchanged
}

func (s *spyDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	s.bump(func() { s.stmts++ })
	return s.DB.QueryRow(ctx, sql, args...)
}

func (s *spyDB) Transact(ctx context.Context, mode db.Mode, fn func(context.Context) error) error {
	s.bump(func() {
		if mode == db.Write {
			s.writeTx++
		} else {
			s.readTx++
		}
	})
	return s.DB.Transact(ctx, mode, fn) //nolint:wrapcheck // a test spy passes errors through unchanged
}

func (s *spyDB) counts() spyCounts {
	s.mu.Lock()
	defer s.mu.Unlock()
	return spyCounts{Stmts: s.stmts, WriteTx: s.writeTx, ReadTx: s.readTx}
}

func (s *spyDB) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stmts, s.writeTx, s.readTx = 0, 0, 0
}

// irig is a people rig over a real, migrated, throw-away PostgreSQL database, with a real
// identity.Service next to it.
type irig struct {
	*rig
	pool  *db.Pool
	spy   *spyDB
	idSvc *identity.Service
	idRec *audittest.Fake
	idBus *signalstest.Fake
	idCap *telemetrytest.Capture
}

// newIRig builds the rig. With realIdentity the Accounts seam is the identity service
// over the same database; otherwise it is the in-memory fake.
func newIRig(t *testing.T, realIdentity bool, mods ...func(*people.Options)) *irig {
	t.Helper()
	pool := dbtest.TempDB(t)
	mig, err := db.NewMigrator(pool, slog.New(slog.DiscardHandler))
	wantNoErr(t, err)
	_, err = mig.Migrate(ctxT(t), []db.Set{identity.Set, peopleSet()})
	wantNoErr(t, err)
	spy := &spyDB{DB: pool}

	idCap := telemetrytest.New(t)
	idRec := audittest.NewFake("identity")
	idBus, err := signalstest.NewFake("identity", []signals.Name{identity.SignalAccountCreated}, nil)
	wantNoErr(t, err)

	o, r := validOptions(t, spy)
	idLookup, err := identity.NewLookup(identity.LookupOptions{DB: pool, Tracer: idCap.Tracer("identity")})
	wantNoErr(t, err)
	idSvc, err := identity.New(identity.Options{
		Lookup:   idLookup,
		DB:       pool,
		Clock:    clock.Real(),
		Logger:   idCap.Logger(),
		Tracer:   idCap.Tracer("identity"),
		Meter:    idCap.Meter("identity"),
		Key:      secret.New(sessionKeyText),
		Audit:    idRec,
		Signals:  idBus,
		Settings: settingstest.NewFake("accounts", identity.Settings),
	})
	wantNoErr(t, err)
	if realIdentity {
		o.Accounts = idSvc
	}
	for _, m := range mods {
		m(&o)
	}
	svc, err := people.New(o)
	wantNoErr(t, err)
	r.svc, r.opts = svc, o
	return &irig{rig: r, pool: pool, spy: spy, idSvc: idSvc, idRec: idRec, idBus: idBus, idCap: idCap}
}

var ipCounter atomic.Uint32

// adminCounter gives each admin() person a distinct playa name (P1: names are unique).
var adminCounter atomic.Uint32

func fromIP(ctx context.Context) context.Context {
	n := ipCounter.Add(1)
	ctx = httpx.WithClientAddr(ctx, netip.MustParseAddr(fmt.Sprintf("10.%d.%d.%d", (n>>16)&255, (n>>8)&255, n&255)))
	return httpx.WithScheme(ctx, "https")
}

// user is a signed-up person with a session.
type user struct {
	email   model.Email
	person  model.PersonID
	account model.AccountID
	sess    identity.Session
}

// ctx returns a request context for the user's session.
func (u user) ctx(t testing.TB) context.Context {
	t.Helper()
	return identity.WithSession(fromIP(ctxT(t)), u.sess)
}

// signUp creates the account local@example.org through identity and, when deliver is
// true, hands the account-created signal to people's handler, as the dispatcher would.
func (r *irig) signUp(local string, deliver bool) user {
	r.t.Helper()
	email := mail(r.t, local+"@example.org")
	iss, err := r.idSvc.SignUp(fromIP(ctxT(r.t)), identity.Credentials{Email: email, Password: pwGood})
	if err != nil {
		r.t.Fatalf("SignUp: %v", err)
	}
	u := user{email: email, person: iss.Session.Person, account: iss.Session.Account, sess: iss.Session}
	if deliver {
		r.deliverCreated(u)
	}
	return u
}

// deliverCreated finds identity's published account-created signal for u and delivers it
// to people's handler.
func (r *irig) deliverCreated(u user) {
	r.t.Helper()
	for _, s := range r.idBus.Published() {
		if s.Name != identity.SignalAccountCreated {
			continue
		}
		if payloadPerson(s) == u.person {
			wantNoErr(r.t, r.bus.Deliver(ctxT(r.t), s))
			return
		}
	}
	r.t.Fatal("identity published no account-created signal for the user")
}

// superuser creates a superuser account and returns it signed in.
func (r *irig) superuser(local string) user {
	r.t.Helper()
	email := mail(r.t, local+"@example.org")
	res, err := r.idSvc.EnsureSuperuser(ctxT(r.t), email)
	wantNoErr(r.t, err)
	iss, err := r.idSvc.SignIn(fromIP(ctxT(r.t)), identity.Credentials{Email: email, Password: res.Password.Value()})
	wantNoErr(r.t, err)
	return user{email: email, person: res.Person, account: res.Account, sess: iss.Session}
}

// ---- fixtures straight into the tables --------------------------------------------------

type seed struct {
	Playa, Pronouns, Legal, Phone, ContactEmail string
}

// insertPerson writes a core.person row directly and returns the new ID.
func (r *irig) insertPerson(s seed) model.PersonID {
	r.t.Helper()
	id := newPerson()
	r.insertPersonAs(id, s)
	return id
}

func (r *irig) insertPersonAs(id model.PersonID, s seed) {
	r.t.Helper()
	var ce *string
	if s.ContactEmail != "" {
		ce = &s.ContactEmail
	}
	_, err := r.pool.Exec(ctxT(r.t),
		`INSERT INTO core.person (id, playa_name, playa_name_key, pronouns, legal_name, phone, contact_email, created_at, updated_at)
		 VALUES ($1, $2, $8, $3, $4, $5, $6, $7, $7)`,
		[16]byte(id), s.Playa, s.Pronouns, s.Legal, s.Phone, ce, r.clk.Now(), people.NameKey(s.Playa))
	if err != nil {
		r.t.Fatalf("seeding a person: %v", err)
	}
}

func (r *irig) insertContact(p model.PersonID, name, phone, rel string, pos int) people.ContactID {
	r.t.Helper()
	id := model.NewID[people.ContactID]()
	_, err := r.pool.Exec(ctxT(r.t),
		`INSERT INTO core.person_emergency_contact (id, person_id, name, phone, relationship, position, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $7)`,
		[16]byte(id), [16]byte(p), name, phone, rel, pos, r.clk.Now())
	if err != nil {
		r.t.Fatalf("seeding a contact: %v", err)
	}
	return id
}

func (r *irig) scalarInt(sql string, args ...any) int {
	r.t.Helper()
	var n int
	if err := r.pool.QueryRow(ctxT(r.t), sql, args...).Scan(&n); err != nil {
		r.t.Fatalf("query: %v", err)
	}
	return n
}

func (r *irig) personRows() int { return r.scalarInt("SELECT count(*) FROM core.person") }

func (r *irig) rowExists(p model.PersonID) bool {
	return r.scalarInt("SELECT count(*) FROM core.person WHERE id = $1", [16]byte(p)) == 1
}

func (r *irig) contactRows(p model.PersonID) int {
	return r.scalarInt("SELECT count(*) FROM core.person_emergency_contact WHERE person_id = $1", [16]byte(p))
}

func (r *irig) consentRows(p model.PersonID) int {
	return r.scalarInt("SELECT count(*) FROM core.person_consent WHERE person_id = $1", [16]byte(p))
}

func (r *irig) requestRows(p model.PersonID) int {
	return r.scalarInt("SELECT count(*) FROM core.data_request WHERE person_id = $1", [16]byte(p))
}

func (r *irig) accountRows(p model.PersonID) int {
	return r.scalarInt("SELECT count(*) FROM core.account WHERE person_id = $1", [16]byte(p))
}

func (r *irig) exec(sql string, args ...any) {
	r.t.Helper()
	if _, err := r.pool.Exec(ctxT(r.t), sql, args...); err != nil {
		r.t.Fatalf("exec: %v", err)
	}
}

// grant gives p every listed permission site-wide.
func (r *irig) grant(p model.PersonID, perms ...authz.Permission) {
	for _, s := range perms {
		r.az.Grant(p, s)
	}
}

// admin returns a fresh person (with a profile row) holding the given permissions and a
// ctx for them.
func (r *irig) admin(perms ...authz.Permission) (model.PersonID, context.Context) {
	r.t.Helper()
	p := r.insertPerson(seed{Playa: fmt.Sprintf("Qx Steward %d", adminCounter.Add(1))})
	r.grant(p, perms...)
	return p, asPerson(ctxT(r.t), p)
}

// created is the shorthand an admin uses to make a person.
func (r *irig) create(ctx context.Context, playa string) people.Profile {
	r.t.Helper()
	prof, err := r.svc.Create(ctx, people.ProfileInput{PlayaName: str(playa)})
	wantNoErr(r.t, err)
	return prof
}

// ---- helpers only the integration tests use ----------------------------------------------

func wantRateLimited(t testing.TB, err error) {
	t.Helper()
	wantKind(t, err, errs.Unavailable)
	if !errors.Is(err, httpx.ErrRateLimited) {
		t.Fatalf("error does not wrap httpx.ErrRateLimited: %v", err)
	}
}

func (l *callLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.calls...)
}

func (f *fakeAccounts) addAccount(p model.PersonID, email model.Email, super bool) model.AccountID {
	f.mu.Lock()
	defer f.mu.Unlock()
	a := fakeAccount{acct: model.NewID[model.AccountID](), email: email, super: super}
	f.byPerson[p] = a
	return a.acct
}

func (s *policySrc) set(mod func(*people.Policy)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	mod(&s.p)
}

func (r *rig) entries(action string) []audit.Entry {
	var out []audit.Entry
	for _, e := range r.rec.Recorded() {
		if string(e.Action) == action {
			out = append(out, e)
		}
	}
	return out
}

func (r *rig) count(action string) int { return len(r.entries(action)) }

func (r *rig) only(action string) audit.Entry {
	r.t.Helper()
	got := r.entries(action)
	if len(got) != 1 {
		r.t.Fatalf("%d entries for %s, want 1 (all: %v)", len(got), action, actionsOf(r.rec.Recorded()))
	}
	return got[0]
}

func (r *rig) published(name signals.Name) []signals.Signal {
	var out []signals.Signal
	for _, s := range r.bus.Published() {
		if s.Name == name {
			out = append(out, s)
		}
	}
	return out
}

func payloadPerson(s signals.Signal) model.PersonID {
	id, err := signals.Get[model.PersonID](s.Payload, "person")
	if err != nil {
		return model.PersonID{}
	}
	return id
}

func wantCode(t testing.TB, err error, kind errs.Kind, code string) {
	t.Helper()
	wantKind(t, err, kind)
	if !hasField(err, "", code) {
		t.Fatalf("no field error with code %q in %v", code, validate.Fields(err))
	}
}

func (r *rig) measured(instrument string, want map[string]string) float64 {
	var sum float64
	for _, m := range r.cap.Measurements() {
		if m.Instrument != instrument {
			continue
		}
		ok := true
		for k, v := range want {
			if m.Attrs[k] != v {
				ok = false
			}
		}
		if ok {
			sum += m.Value
		}
	}
	return sum
}

// entries (Reason and Fields included).
func (r *rig) noLeak(needles ...string) {
	r.t.Helper()
	dump := fmt.Sprintf("%+v", r.rec.Recorded())
	for _, n := range needles {
		if n == "" {
			continue
		}
		if r.cap.Contains(n) {
			r.t.Errorf("a log, span or metric contains %q", n)
		}
		if strings.Contains(dump, n) {
			r.t.Errorf("an audit entry contains %q", n)
		}
	}
}

func allMarkers() []string {
	return []string{mPlaya, mLegal, mPhone, mContactEmail, mECName, mECPhone, mRelationship, mNote, mDecision}
}

func viewerOf(ctx context.Context) model.PersonID {
	p, _ := identity.PersonFrom(ctx)
	return p
}
