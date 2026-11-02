//go:build integration

package datarights_test

import (
	"context"
	"encoding/json"
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
	"github.com/zedgama3/bms/internal/core/datarights"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/core/notify/notifytest"
	"github.com/zedgama3/bms/internal/core/people"
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
	sessionKeyText = "datarights-test-session-key-0123456789abcdef-xyz"
	pwGood         = "tangerine-orbit-quiet-77"
)

// spyDB counts what the services run against the real pool.
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

// loggedPeople is the real people.Service as datarights' PeopleSource, logging its export
// and deletion in the rig's call log so the order of the whole deletion can be asserted.
type loggedPeople struct {
	datarights.PeopleSource
	log *callLog
}

func (l loggedPeople) ExportPerson(ctx context.Context, p model.PersonID) (json.RawMessage, error) {
	l.log.add("people.export")
	return l.PeopleSource.ExportPerson(ctx, p) //nolint:wrapcheck // a test wrapper
}

func (l loggedPeople) DeletePerson(ctx context.Context, p model.PersonID) (int, error) {
	l.log.add("people.delete")
	return l.PeopleSource.DeletePerson(ctx, p) //nolint:wrapcheck // a test wrapper
}

// irig is a datarights rig over a real, migrated, throw-away PostgreSQL database, with the
// real people.Service as its PeopleSource and, optionally, a real identity.Service.
type irig struct {
	*rig
	pool  *db.Pool
	spy   *spyDB
	ppl   *people.Service
	prec  *audittest.Fake   // people's own recorder
	pbus  *signalstest.Fake // people's own signals
	idSvc *identity.Service
	idRec *audittest.Fake
	idBus *signalstest.Fake
	idCap *telemetrytest.Capture
}

// newIRig builds the rig. With realIdentity the Accounts seam of both people and
// datarights is the identity service over the same database; otherwise it is the in-memory
// fake. mods edit datarights' Options before New.
func newIRig(t *testing.T, realIdentity bool, mods ...func(*datarights.Options)) *irig {
	t.Helper()
	pool := dbtest.TempDB(t)
	mig, err := db.NewMigrator(pool, slog.New(slog.DiscardHandler))
	wantNoErr(t, err)
	_, err = mig.Migrate(ctxT(t), []db.Set{identity.Set, people.Set})
	wantNoErr(t, err)
	spy := &spyDB{DB: pool}

	idCap := telemetrytest.New(t)
	idRec := audittest.NewFake("identity")
	idBus, err := signalstest.NewFake("identity", []signals.Name{identity.SignalAccountCreated}, nil)
	wantNoErr(t, err)
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
		Holders:  idleHolders{}, Notify: notifytest.NewFake("identity"), Alerts: idleAlerts{},
	})
	wantNoErr(t, err)

	// datarights' fakes first, then people over the same clock, authorizer and DB.
	o, r := validOptions(t, spy, nil)
	pc := telemetrytest.New(t)
	prec := audittest.NewFake("people")
	pbus, err := signalstest.NewFake("people", people.PublishedSignals, people.SubscribedSignals)
	wantNoErr(t, err)
	pLookup, err := people.NewLookup(people.LookupOptions{DB: spy, Tracer: pc.Tracer("people")})
	wantNoErr(t, err)
	var accounts people.Accounts = r.accts
	if realIdentity {
		accounts = idSvc
	}
	ppl, err := people.New(people.Options{
		Lookup:   pLookup,
		DB:       spy,
		Clock:    r.clk,
		Logger:   pc.Logger(),
		Tracer:   pc.Tracer("people"),
		Meter:    pc.Meter("people"),
		Audit:    prec,
		Signals:  pbus,
		Authz:    r.az,
		Actor:    actorOf,
		Session:  identity.SessionFrom,
		Accounts: accounts,
		Settings: settingstest.NewFake("people", people.Settings),
	})
	wantNoErr(t, err)

	o.People = loggedPeople{PeopleSource: ppl, log: r.log}
	if realIdentity {
		o.Accounts = idSvc
	}
	for _, m := range mods {
		m(&o)
	}
	svc, err := datarights.New(o)
	wantNoErr(t, err)
	r.svc, r.opts = svc, o
	return &irig{rig: r, pool: pool, spy: spy, ppl: ppl, prec: prec, pbus: pbus, idSvc: idSvc, idRec: idRec, idBus: idBus, idCap: idCap}
}

// publishedByPeople lists what people published (people.person-deleted).
func (r *irig) publishedByPeople(name signals.Name) []signals.Signal {
	var out []signals.Signal
	for _, s := range r.pbus.Published() {
		if s.Name == name {
			out = append(out, s)
		}
	}
	return out
}

var ipCounter atomic.Uint32

// adminCounter gives each admin() person a distinct playa name (names are unique).
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

func (r *irig) insertContact(p model.PersonID, name, phone, rel string, pos int) {
	r.t.Helper()
	id := model.NewID[people.ContactID]()
	_, err := r.pool.Exec(ctxT(r.t),
		`INSERT INTO core.person_emergency_contact (id, person_id, name, phone, relationship, position, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $7)`,
		[16]byte(id), [16]byte(p), name, phone, rel, pos, r.clk.Now())
	if err != nil {
		r.t.Fatalf("seeding a contact: %v", err)
	}
}

func (r *irig) scalarInt(sql string, args ...any) int {
	r.t.Helper()
	var n int
	if err := r.pool.QueryRow(ctxT(r.t), sql, args...).Scan(&n); err != nil {
		r.t.Fatalf("query: %v", err)
	}
	return n
}

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

// admin returns a fresh person (with a profile row) holding the given permissions and a
// ctx for them.
func (r *irig) admin(perms ...authz.Permission) (model.PersonID, context.Context) {
	r.t.Helper()
	p := r.insertPerson(seed{Playa: fmt.Sprintf("Qx Steward %d", adminCounter.Add(1))})
	r.grant(p, perms...)
	return p, asPerson(ctxT(r.t), p)
}

// create makes a person through people's own use case, as an admin would.
func (r *irig) create(ctx context.Context, playa string) people.Profile {
	r.t.Helper()
	prof, err := r.ppl.Create(ctx, people.ProfileInput{PlayaName: str(playa)})
	wantNoErr(r.t, err)
	return prof
}

// idleHolders and idleAlerts are the superuser alert's seams for a rig that signs no
// superuser in: nobody holds the permission and no alert address is configured.
type idleHolders struct{}

func (idleHolders) HoldersOf(context.Context, authz.Permission) ([]model.PersonID, error) {
	return nil, nil
}

type idleAlerts struct{}

func (idleAlerts) SendAlert(context.Context, notify.Message) error {
	return errs.E("test.SendAlert", errs.Unsupported, errors.New("no alert address"))
}

// ctx returns a request context for the user's session.
func (u user) ctx(t testing.TB) context.Context {
	t.Helper()
	return identity.WithSession(fromIP(ctxT(t)), u.sess)
}

// signUp creates the account local@example.org through identity and, when deliver is
// true, hands the account-created signal to people's handler, as the dispatcher would.
// Only a rig built with realIdentity may call it.
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
			wantNoErr(r.t, r.pbus.Deliver(ctxT(r.t), s))
			return
		}
	}
	r.t.Fatal("identity published no account-created signal for the user")
}

// ---- helpers only the integration tests use --------------------------------------------

// Every person, address and phone number in this package is invented. The markers below
// are what the leak checks look for in logs, spans, metrics and audit entries.
const (
	mPlaya        = "Glimmer Wasp"
	mLegal        = "Zephyrine Marlowe-Quince"
	mPhone        = "+1 555 0177"
	mContactEmail = "marker.contact@example.org"
	mECName       = "Orville Tansy"
	mECPhone      = "+1 555 0188"
	mRelationship = "cousin once removed"
	mNote         = "private-note-marker-4471"
	mDecision     = "decision-note-marker-9023"

	sigDeleted   signals.Name = "people.person-deleted"
	sigRequested signals.Name = "datarights.deletion-requested"
)

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

func wantRateLimited(t testing.TB, err error) {
	t.Helper()
	wantKind(t, err, errs.Unavailable)
	if !errors.Is(err, httpx.ErrRateLimited) {
		t.Fatalf("error does not wrap httpx.ErrRateLimited: %v", err)
	}
}

func wantCode(t testing.TB, err error, kind errs.Kind, code string) {
	t.Helper()
	wantKind(t, err, kind)
	if !hasField(err, "", code) {
		t.Fatalf("no field error with code %q in %v", code, validate.Fields(err))
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

func asSuper(ctx context.Context, p model.PersonID) context.Context {
	signedIn.Store(p, true)
	return identity.WithSession(ctx, sessionOf(p, true))
}

func payloadPerson(s signals.Signal) model.PersonID {
	id, err := signals.Get[model.PersonID](s.Payload, "person")
	if err != nil {
		return model.PersonID{}
	}
	return id
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

// grant gives p every listed permission site-wide.
func (r *rig) grant(p model.PersonID, perms ...authz.Permission) {
	for _, s := range perms {
		r.az.Grant(p, s)
	}
}

func allMarkers() []string {
	return []string{mPlaya, mLegal, mPhone, mContactEmail, mECName, mECPhone, mRelationship, mNote, mDecision}
}

// noLeak fails when a needle reached a log, span, metric or audit entry.
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
