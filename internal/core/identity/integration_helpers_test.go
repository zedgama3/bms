//go:build integration

package identity_test

import (
	"fmt"
	"github.com/zedgama3/bms/internal/lib/validate"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"context"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
	"github.com/zedgama3/bms/internal/model"
)

// prig is a rig over a real, migrated, throw-away PostgreSQL database.
type prig struct {
	*rig
	pool *db.Pool
	fdb  *faultDB
}

func newPoolRig(t *testing.T, mods ...func(*identity.Options)) *prig {
	t.Helper()
	pool := dbtest.TempDB(t)
	mig, err := db.NewMigrator(pool, slog.New(slog.DiscardHandler))
	wantNoErr(t, err)
	_, err = mig.Migrate(ctxT(t), []db.Set{identity.Set})
	wantNoErr(t, err)
	fdb := newFaultDB(pool)
	return &prig{rig: newRig(t, fdb, mods...), pool: pool, fdb: fdb}
}

// other returns another Service over the same database, clock, recorder and bus.
func (p *prig) other(mods ...func(*identity.Options)) *identity.Service {
	p.t.Helper()
	o := p.opts
	for _, m := range mods {
		m(&o)
	}
	svc, err := identity.New(o)
	wantNoErr(p.t, err)
	return svc
}

// admin returns a Service whose checker allows both accounts permissions.
func (p *prig) admin() *identity.Service {
	return p.other(withPerms(allowing(identity.PermissionAccountsRead, identity.PermissionAccountsManage)))
}

var ipCounter atomic.Uint32

// nextIP returns a fresh documentation-range client address so the limiter never
// interferes between users.
func nextIP() string {
	n := ipCounter.Add(1)
	return fmt.Sprintf("10.%d.%d.%d", (n>>16)&255, (n>>8)&255, n&255)
}

// user is a signed-up person.
type user struct {
	email model.Email
	pw    string
	sess  identity.Session
}

// ctx returns a request context for the user's session.
func (u user) ctx(t testing.TB) context.Context {
	t.Helper()
	return identity.WithSession(from(ctxT(t), nextIP()), u.sess)
}

// signUp creates the account local@example.org with pwGood.
func (p *prig) signUp(local string) user {
	p.t.Helper()
	return p.signUpAs(local+"@example.org", pwGood)
}

func (p *prig) signUpAs(email, pw string) user {
	p.t.Helper()
	iss, err := p.svc.SignUp(from(ctxT(p.t), nextIP()), identity.Credentials{Email: mail(p.t, email), Password: pw})
	if err != nil {
		p.t.Fatalf("SignUp(%s): %v", "<invented>", err)
	}
	return user{email: mail(p.t, email), pw: pw, sess: iss.Session}
}

// signIn signs a user in again with the service svc and returns the new session.
func (p *prig) signIn(svc *identity.Service, u user) (identity.Issued, error) {
	iter, err := svc.SignIn(from(ctxT(p.t), nextIP()), identity.Credentials{Email: u.email, Password: u.pw})
	return iter, keep(err)
}

// cookie signs in through the POST /sign-in handler and returns the session cookie.
func (p *prig) cookie(email model.Email, pw string) *http.Cookie {
	p.t.Helper()
	w, f := call(p.t, handlerFor(p.t, p.svc, "POST", "/sign-in"), from(ctxT(p.t), nextIP()), credsJSON(string(email), pw))
	if f.count() != 0 {
		p.t.Fatalf("sign-in handler failed: %v", f.last())
	}
	cs := setCookies(w)
	if len(cs) != 1 {
		p.t.Fatalf("%d cookies from sign-in", len(cs))
	}
	return cs[0]
}

// auth runs Authenticate for a request carrying c (nil: no cookie).
func (p *prig) auth(c *http.Cookie) (context.Context, model.PersonID, error) {
	p.t.Helper()
	name, val := "", ""
	if c != nil {
		name, val = c.Name, c.Value
	}
	ctx, person, err := p.svc.Authenticate(from(ctxT(p.t), nextIP()), requestWithCookie(p.t, name, val))
	return ctx, person, keep(err)
}

func (p *prig) wantAuthenticated(c *http.Cookie, want model.PersonID) {
	p.t.Helper()
	ctx, person, err := p.auth(c)
	wantNoErr(p.t, err)
	if person != want {
		p.t.Fatalf("person = %v, want %v", person, want)
	}
	if _, ok := identity.SessionFrom(ctx); !ok {
		p.t.Fatal("no session in the returned ctx")
	}
}

func (p *prig) wantRejected(c *http.Cookie) {
	p.t.Helper()
	_, _, err := p.auth(c)
	wantKind(p.t, err, errs.Unauthenticated)
}

// superuser creates (or resets) a superuser account and signs it in.
func (p *prig) superuser(email string) (identity.SuperuserResult, identity.Issued) {
	p.t.Helper()
	res, err := p.svc.EnsureSuperuser(ctxT(p.t), mail(p.t, email))
	wantNoErr(p.t, err)
	iss, err := p.svc.SignIn(from(ctxT(p.t), nextIP()), identity.Credentials{Email: mail(p.t, email), Password: res.Password.Value()})
	wantNoErr(p.t, err)
	return res, iss
}

// rowState is what the tests read straight from core.account.
type rowState struct {
	LastLoginAt, DisabledAt *time.Time
	Generation              int
	PasswordChangeRequired  bool
	Superuser               bool
	PasswordHash            string
}

// rowOf reads the account row of person p straight from the table (no audit entry).
func (p *prig) rowOf(person model.PersonID) rowState {
	p.t.Helper()
	var r rowState
	err := p.pool.QueryRow(ctxT(p.t),
		"SELECT last_login_at, disabled_at, session_generation, password_change_required, superuser, password_hash FROM core.account WHERE person_id = $1",
		[16]byte(person)).Scan(&r.LastLoginAt, &r.DisabledAt, &r.Generation, &r.PasswordChangeRequired, &r.Superuser, &r.PasswordHash)
	if err != nil {
		p.t.Fatalf("reading the account row: %v", err)
	}
	return r
}

// accountCount counts rows in core.account.
func (p *prig) accountCount() int {
	p.t.Helper()
	var n int
	if err := p.pool.QueryRow(ctxT(p.t), "SELECT count(*) FROM core.account").Scan(&n); err != nil {
		p.t.Fatalf("counting accounts: %v", err)
	}
	return n
}

// callPlain runs a route handler without a Failer, so httpx.Fail writes its own fallback
// problem body: what two responses carry can then be compared byte for byte.
func callPlain(t testing.TB, rt httpx.Route, ctx context.Context, body string) *httptest.ResponseRecorder { //nolint:revive // ctx is deliberately second: the route is the subject
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	r := httptest.NewRequestWithContext(ctx, rt.Method, "/api/accounts"+strings.TrimSuffix(rt.Pattern, "/"), rd)
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	rt.Handler.ServeHTTP(w, r)
	return w
}

func repeat(s string, n int) string { return strings.Repeat(s, n) }

func bytesContains(b []byte, s string) bool { return strings.Contains(string(b), s) }

// hasFieldCode checks a field error without constraining the kind.
func hasFieldCode(t testing.TB, err error, field, code string) {
	t.Helper()
	for _, fe := range validate.Fields(err) {
		if fe.Field == field && fe.Code == code {
			return
		}
	}
	t.Fatalf("no field error %q/%q in %v", field, code, validate.Fields(err))
}

// counts returns the statements counted so far.
func (f *faultDB) counts() (reads, writes, txs int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reads, f.writes, f.txs
}

func (f *faultDB) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads, f.writes, f.txs = 0, 0, 0
	f.sqls = nil
}

// statements returns the SQL text of every statement run since the last reset.
func (f *faultDB) statements() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.sqls...)
}

// defaultCooldown is the policy default (5m), read from DefaultPolicy rather than a constant.
var defaultCooldown = identity.DefaultPolicy().PasswordResetCooldown

func (h *holdersFake) permissions() []authz.Permission {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]authz.Permission(nil), h.asked...)
}

func (a *alertSenderFake) calls() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.ctxErrs)
}

func (a *alertSenderFake) sent() []notify.Message {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]notify.Message(nil), a.msgs...)
}
