//go:build integration

package identity_test

import (
	"bytes"
	"context"
	"log/slog"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

func TestSignUp_Success_IssuesSessionRecordsAndPublishes(t *testing.T) {
	p := newPoolRig(t)
	const addr = "Ada.Quill@Example.org"
	iss, err := p.svc.SignUp(from(ctxT(t), nextIP()), identity.Credentials{Email: mail(t, addr), Password: pwGood})
	wantNoErr(t, err)
	s := iss.Session
	if err := s.Validate(); err != nil {
		t.Fatalf("issued session invalid: %v", err)
	}
	if s.Superuser || s.PasswordChangeRequired {
		t.Errorf("session flags = %+v", s)
	}
	if !s.IssuedAt.Equal(p.clk.Now()) || !s.ExpiresAt.Equal(p.clk.Now().Add(720*time.Hour)) {
		t.Errorf("times = %v..%v, want now..now+720h", s.IssuedAt, s.ExpiresAt)
	}
	e := p.only(actCreated)
	if e.Outcome != audit.Succeeded || e.Actor.Kind != audit.ActorPerson || e.Actor.Person != s.Person ||
		e.Subject != s.Person || e.Record.Kind != "identity.account" || e.Record.ID != [16]byte(s.Account) {
		t.Errorf("entry = %+v", e)
	}
	pub := p.bus.Published()
	if len(pub) != 1 || pub[0].Name != identity.SignalAccountCreated {
		t.Fatalf("published = %+v", pub)
	}
	acct, err := signals.Get[model.AccountID](pub[0].Payload, identity.PayloadAccount)
	wantNoErr(t, err)
	person, err := signals.Get[model.PersonID](pub[0].Payload, identity.PayloadPerson)
	wantNoErr(t, err)
	if acct != s.Account || person != s.Person {
		t.Errorf("payload = %v %v", acct, person)
	}
	if row := p.rowOf(s.Person); row.PasswordHash == pwGood || row.PasswordHash == "" {
		t.Error("password stored in the clear or not at all")
	}
	// stored as entered
	su := identity.Session{ID: model.NewID[identity.SessionID](), Account: newAccountID(), Person: newPersonID(), Superuser: true, IssuedAt: p.clk.Now(), ExpiresAt: p.clk.Now().Add(time.Hour)}
	got, err := p.admin().GetAccount(identity.WithSession(from(ctxT(t), nextIP()), su), s.Account)
	wantNoErr(t, err)
	if got.Email != model.Email(addr) {
		t.Error("the address was not stored as entered")
	}
	p.noLeak(addr, "ada.quill", pwGood)
}

// F15
func TestSignUp_Taken_Conflict(t *testing.T) {
	p := newPoolRig(t)
	p.signUpAs("Dune.Walker@example.org", pwGood)
	before := len(p.bus.Published())
	for _, addr := range []string{"Dune.Walker@example.org", "dune.walker@EXAMPLE.ORG"} {
		_, err := p.svc.SignUp(from(ctxT(t), nextIP()), identity.Credentials{Email: mail(t, addr), Password: pwOther})
		wantKind(t, err, errs.Conflict)
		hasFieldCode(t, err, "email", "taken")
	}
	if p.count(actCreated) != 1 || len(p.bus.Published()) != before {
		t.Error("a refused sign-up left an entry or a signal")
	}
	if p.accountCount() != 1 {
		t.Errorf("%d accounts", p.accountCount())
	}
}

// F15, api D31: through the sign-up handler a taken address is a 409 whose error carries the
// email/taken field error for the shell's mapper to put in `fields`.
func TestSignUp_Taken_Handler_409WithEmailTaken(t *testing.T) {
	p := newPoolRig(t)
	p.signUpAs("Dune.Walker@example.org", pwGood)
	rt := handlerFor(t, p.svc, "POST", "/sign-up")
	w, f := call(t, rt, from(ctxT(t), nextIP()), credsJSON("dune.walker@EXAMPLE.ORG", pwOther))
	if w.Code != 409 {
		t.Fatalf("status = %d, want 409", w.Code)
	}
	if f.count() != 1 {
		t.Fatalf("%d errors handed to Fail, want 1", f.count())
	}
	err := f.last()
	wantKind(t, err, errs.Conflict)
	hasFieldCode(t, err, "email", "taken")
	if len(setCookies(w)) != 0 {
		t.Error("a refused sign-up set a cookie")
	}
}

func TestSignUp_ConcurrentSameAddress_OneWins(t *testing.T) {
	p := newPoolRig(t)
	const n = 8
	errsOut := make(chan error, n)
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			_, err := p.svc.SignUp(from(ctxT(t), nextIP()), identity.Credentials{Email: mail(t, "race.runner@example.org"), Password: pwGood})
			errsOut <- err
		})
	}
	wg.Wait()
	close(errsOut)
	ok, conflict := 0, 0
	for err := range errsOut {
		switch {
		case err == nil:
			ok++
		case errs.Is(err, errs.Conflict):
			conflict++
		default:
			t.Errorf("unexpected: %v", err)
		}
	}
	if ok != 1 || conflict != n-1 {
		t.Errorf("ok=%d conflict=%d, want 1 and %d", ok, conflict, n-1)
	}
	if p.accountCount() != 1 {
		t.Errorf("%d accounts", p.accountCount())
	}
}

// failingBus is a signals.Bus whose Publish fails.
type failingBus struct{ err error }

func (b failingBus) Publish(context.Context, signals.Name, signals.Payload) error { return b.err }
func (b failingBus) Subscribe(signals.Name, signals.Handler) error                { return nil }

// F57
func TestSignUp_PublishFails_RollsBack(t *testing.T) {
	p := newPoolRig(t, func(o *identity.Options) { o.Signals = failingBus{err: errs.E("test.bus", errs.Unavailable, errDown)} })
	_, err := p.svc.SignUp(from(ctxT(t), nextIP()), identity.Credentials{Email: mail(t, "ada.quill@example.org"), Password: pwGood})
	wantKind(t, err, errs.Unavailable)
	if p.accountCount() != 0 {
		t.Error("an account survived a failed publish")
	}
}

// F58
func TestSignUp_AuditFails_RollsBack(t *testing.T) {
	p := newPoolRig(t)
	p.rec.Fail(errs.E("test.audit", errs.Unavailable, errDown))
	_, err := p.svc.SignUp(from(ctxT(t), nextIP()), identity.Credentials{Email: mail(t, "ada.quill@example.org"), Password: pwGood})
	wantKind(t, err, errs.Unavailable)
	if p.accountCount() != 0 {
		t.Error("an account survived a failed audit write")
	}
	if len(p.bus.Published()) != 0 {
		t.Error("a signal was published for a rolled-back sign-up")
	}
	p.rec.Fail(nil)
	p.signUp("ada.quill") // the address is free again
}

func TestSignUp_CancelledContext_Canceled(t *testing.T) {
	p := newPoolRig(t)
	ctx, cancel := context.WithCancel(from(ctxT(t), nextIP()))
	cancel()
	_, err := p.svc.SignUp(ctx, identity.Credentials{Email: mail(t, "ada.quill@example.org"), Password: pwGood})
	wantKind(t, err, errs.Canceled)
}

func TestSignUp_ReadOnlyCopy_Unavailable(t *testing.T) {
	p := newPoolRig(t)
	p.fdb.set(func(f *faultDB) { f.ro = true })
	_, err := p.svc.SignUp(from(ctxT(t), nextIP()), identity.Credentials{Email: mail(t, "ada.quill@example.org"), Password: pwGood})
	wantReadOnly(t, err)
	if p.accountCount() != 0 || len(p.rec.Recorded()) != 0 {
		t.Error("a read-only copy wrote")
	}
}

func TestSignIn_Success_SessionEntryAndLastLogin(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("ada.quill")
	wantNoErr(t, p.clk.Advance(3*time.Hour))
	iss, err := p.svc.SignIn(from(ctxT(t), nextIP()), identity.Credentials{Email: mail(t, "ADA.QUILL@example.org"), Password: pwGood})
	wantNoErr(t, err)
	if iss.Session.Person != u.sess.Person || iss.Session.Account != u.sess.Account || iss.Session.ID == u.sess.ID {
		t.Errorf("session = %+v", iss.Session)
	}
	if !iss.Session.ExpiresAt.Equal(p.clk.Now().Add(720 * time.Hour)) {
		t.Errorf("ExpiresAt = %v", iss.Session.ExpiresAt)
	}
	e := p.only(actLogin)
	if e.Outcome != audit.Succeeded || e.Actor.Kind != audit.ActorPerson || e.Actor.Person != u.sess.Person || e.Actor.Superuser ||
		e.Subject != u.sess.Person || e.Record.Kind != "identity.session" || e.Record.ID != [16]byte(iss.Session.ID) {
		t.Errorf("entry = %+v", e)
	}
	row := p.rowOf(u.sess.Person)
	if row.LastLoginAt == nil || !row.LastLoginAt.Equal(p.clk.Now()) {
		t.Errorf("last_login_at = %v, want %v", row.LastLoginAt, p.clk.Now())
	}
	if got := p.measured(metricLogins, map[string]string{"outcome": "ok"}); got != 1 {
		t.Errorf("logins{ok} = %v", got)
	}
}

// F19
func TestSignIn_WrongPassword_RecordsFailed(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("ada.quill")
	_, err := p.svc.SignIn(from(ctxT(t), nextIP()), identity.Credentials{Email: u.email, Password: pwOther})
	wantKind(t, err, errs.Unauthenticated)
	e := p.only(actLoginFailed)
	if e.Outcome != audit.Failed || e.Actor.Kind != audit.ActorAnonymous || e.Record.Kind != "identity.account" || e.Record.ID != [16]byte(u.sess.Account) {
		t.Errorf("entry = %+v", e)
	}
	if p.count(actLogin) != 0 {
		t.Error("a login was recorded")
	}
	if got := p.measured(metricLogins, map[string]string{"outcome": "bad_credentials"}); got != 1 {
		t.Errorf("logins{bad_credentials} = %v", got)
	}
	p.noLeak("ada.quill", pwOther, pwGood)
}

// F20
func TestSignIn_Disabled_UnauthenticatedDenied(t *testing.T) {
	p := newPoolRig(t)
	victim := p.signUp("sam.gray")
	admin := p.admin()
	boss := p.signUp("boss.lady")
	wantNoErr(t, admin.DisableAccount(boss.ctx(t), victim.sess.Account))
	_, err := p.signIn(p.svc, victim) // correct password
	wantKind(t, err, errs.Unauthenticated)
	e := p.only(actLoginFailed)
	if e.Outcome != audit.Denied || e.Record.ID != [16]byte(victim.sess.Account) {
		t.Errorf("entry = %+v, want Denied with the account", e)
	}
	if got := p.measured(metricLogins, map[string]string{"outcome": "disabled"}); got != 1 {
		t.Errorf("logins{disabled} = %v", got)
	}
}

// Enumeration resistance: unknown address, wrong password and disabled account answer with
// the same status and the same body.
func TestSignIn_EnumerationResistance_Integration(t *testing.T) {
	p := newPoolRig(t)
	known := p.signUp("known.person")
	victim := p.signUp("disabled.person")
	boss := p.signUp("boss.lady")
	wantNoErr(t, p.admin().DisableAccount(boss.ctx(t), victim.sess.Account))

	rt := handlerFor(t, p.svc, "POST", "/sign-in")
	send := func(email, pw string) (int, []byte, string) {
		w := callPlain(t, rt, from(ctxT(t), nextIP()), credsJSON(email, pw))
		return w.Code, w.Body.Bytes(), w.Header().Get("Content-Type")
	}
	cases := map[string][2]string{
		"unknown address":           {"nobody.at.all@example.org", pwGood},
		"wrong password":            {string(known.email), pwOther},
		"disabled, right password":  {string(victim.email), victim.pw},
		"disabled, wrong password":  {string(victim.email), pwOther},
		"known address, other case": {"KNOWN.person@example.org", pwOther},
	}
	var wantCode int
	var wantBody []byte
	var wantType string
	names := make([]string, 0, len(cases))
	for n := range cases {
		names = append(names, n)
	}
	sort.Strings(names)
	for i, n := range names {
		code, body, ct := send(cases[n][0], cases[n][1])
		if code != 401 {
			t.Errorf("%s: status = %d, want 401", n, code)
		}
		if i == 0 {
			wantCode, wantBody, wantType = code, body, ct
			continue
		}
		if code != wantCode || !bytes.Equal(body, wantBody) || ct != wantType {
			t.Errorf("%s: response differs from %s: %d %q (%s) vs %d %q (%s)", n, names[0], code, body, ct, wantCode, wantBody, wantType)
		}
	}
}

// The dummy hash: unknown and known-wrong take the same order of time. Medians over many
// samples with a loose ratio keep the property (an unknown address must not skip the hash,
// which would make it orders of magnitude faster) without depending on machine noise.
func TestSignIn_UnknownAndWrong_SameTimingClass(t *testing.T) {
	p := newPoolRig(t, withPolicy(limitPolicy(100)))
	known := p.signUp("known.person")
	median := func(email string) time.Duration {
		var ds []time.Duration
		for range 15 {
			start := time.Now()
			_, err := p.svc.SignIn(from(ctxT(t), nextIP()), identity.Credentials{Email: mail(t, email), Password: pwOther})
			ds = append(ds, time.Since(start))
			wantKind(t, err, errs.Unauthenticated)
		}
		sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
		return ds[len(ds)/2]
	}
	median("warm.up@example.org") // first hash pays allocation and scheduling costs
	unknown, wrong := median("nobody.at.all@example.org"), median(string(known.email))
	if unknown*5 < wrong || wrong*5 < unknown {
		t.Errorf("median unknown %v vs wrong password %v differ by more than 5x", unknown, wrong)
	}
}

// Decision 4: the limiter keys on the submitted address, so an existing account's address
// and an unknown one lock out identically, and then even the right password gets 429.
func TestSignIn_Limiter_SameForKnownAndUnknownAddress(t *testing.T) {
	p := newPoolRig(t, withPolicy(limitPolicy(3)))
	known := p.signUp("known.person")
	for _, email := range []string{string(known.email), "nobody.at.all@example.org"} {
		for range 3 {
			_, err := p.svc.SignIn(from(ctxT(t), nextIP()), identity.Credentials{Email: mail(t, email), Password: pwOther})
			wantKind(t, err, errs.Unauthenticated)
		}
		pw := pwOther
		if email == string(known.email) {
			pw = known.pw // right password, still limited
		}
		_, err := p.svc.SignIn(from(ctxT(t), nextIP()), identity.Credentials{Email: mail(t, email), Password: pw})
		wantRateLimited(t, err)
	}
}

// F21
func TestSignIn_ReadOnly_SucceedsWithoutTouch(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("ada.quill")
	p.fdb.set(func(f *faultDB) { f.ro = true })
	iss, err := p.signIn(p.svc, u)
	wantNoErr(t, err)
	if iss.Session.Person != u.sess.Person {
		t.Error("wrong session")
	}
	if row := p.rowOf(u.sess.Person); row.LastLoginAt != nil {
		t.Errorf("last_login_at = %v on a read-only copy", row.LastLoginAt)
	}
	if p.count(actLogin) != 1 {
		t.Error("the login was not recorded (it spools)")
	}
}

// F23
func TestSignIn_AuditFails_NoSession(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("ada.quill")
	p.rec.Fail(errs.E("test.audit", errs.Unavailable, errDown))
	iss, err := p.signIn(p.svc, u)
	wantKind(t, err, errs.Unavailable)
	if iss.Session != (identity.Session{}) {
		t.Error("a session was issued although the audit write failed")
	}
}

// F22 against a real pool with the connection failing.
func TestSignIn_StoreUnavailable_PassesKind_Integration(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("ada.quill")
	p.fdb.set(func(f *faultDB) { f.readErr = errDown })
	_, err := p.signIn(p.svc, u)
	wantKind(t, err, errs.Unavailable)
}

// F59
func TestSignIn_TemporaryPassword_RestrictedSession(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("ada.quill")
	boss := p.signUp("boss.lady")
	tmp, err := p.admin().SetTemporaryPassword(boss.ctx(t), u.sess.Account)
	wantNoErr(t, err)
	iss, err := p.svc.SignIn(from(ctxT(t), nextIP()), identity.Credentials{Email: u.email, Password: tmp.Value()})
	wantNoErr(t, err)
	if !iss.Session.PasswordChangeRequired {
		t.Fatal("session is not restricted")
	}
	c := p.cookie(u.email, tmp.Value())
	ctx, person, err := p.auth(c)
	wantNoErr(t, err)
	if !person.IsZero() {
		t.Error("Authenticate returned a person for a restricted session")
	}
	s, ok := identity.SessionFrom(ctx)
	if !ok || !s.PasswordChangeRequired || s.Person != u.sess.Person {
		t.Errorf("SessionFrom = %+v, %v", s, ok)
	}
	if _, ok := identity.PersonFrom(ctx); ok {
		t.Error("PersonFrom ok for a restricted session")
	}
}

// F27
func TestSignIn_Superuser_AlerterFailureDoesNotFail(t *testing.T) {
	al := &alerter{err: errs.E("test.alert", errs.Unavailable, errDown)}
	p := newPoolRig(t, func(o *identity.Options) { o.Alerter = al })
	res, iss := p.superuser("root.operator@example.org")
	if al.count() != 1 || al.calls[0] != res.Person {
		t.Errorf("alerter calls = %v", al.calls)
	}
	if !iss.Session.Superuser {
		t.Error("session is not marked superuser")
	}
	if !iss.Session.ExpiresAt.Equal(p.clk.Now().Add(time.Hour)) {
		t.Errorf("superuser session lasts until %v, want one hour", iss.Session.ExpiresAt)
	}
	e := p.only(actSuLogin)
	if e.Outcome != audit.Succeeded || !e.Actor.Superuser || e.Actor.Person != res.Person {
		t.Errorf("entry = %+v", e)
	}
	if p.count(actLogin) != 0 {
		t.Error("a plain identity.login was recorded for the superuser")
	}
	if got := p.measured(metricLogins, map[string]string{"outcome": "superuser"}); got != 1 {
		t.Errorf("logins{superuser} = %v", got)
	}
}

func TestSignIn_Superuser_NilAlerter_OneWarnLogWithPersonID(t *testing.T) {
	p := newPoolRig(t)
	res, _ := p.superuser("root.operator@example.org")
	warns := 0
	for _, rec := range p.cap.Records() {
		if rec.Level >= slog.LevelWarn && rec.Message == "superuser signed in" {
			warns++
			if rec.Attrs["person_id"] != res.Person.String() {
				t.Errorf("person_id = %q", rec.Attrs["person_id"])
			}
		}
	}
	if warns != 1 {
		t.Errorf("%d warn lines, want 1", warns)
	}
	p.noLeak("root.operator")
}

func TestSignIn_Superuser_SessionLifetimeFollowsPolicy(t *testing.T) {
	ps := newPolicySource()
	ps.set(func(pl *identity.Policy) {
		pl.SuperuserSessionLifetime = 30 * time.Minute
		pl.SessionLifetime = 48 * time.Hour
	})
	p := newPoolRig(t, withPolicy(ps))
	_, iss := p.superuser("root.operator@example.org")
	if !iss.Session.ExpiresAt.Equal(p.clk.Now().Add(30 * time.Minute)) {
		t.Errorf("ExpiresAt = %v", iss.Session.ExpiresAt)
	}
	u := p.signUp("ada.quill")
	if !u.sess.ExpiresAt.Equal(p.clk.Now().Add(48 * time.Hour)) {
		t.Errorf("participant ExpiresAt = %v", u.sess.ExpiresAt)
	}
}

func TestSignIn_ConcurrentLogins_AllSucceed(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("ada.quill")
	var wg sync.WaitGroup
	for range 6 {
		wg.Go(func() {
			if _, err := p.signIn(p.svc, u); err != nil {
				t.Errorf("SignIn: %v", err)
			}
		})
	}
	wg.Wait()
}

// A successful sign-in reserves a slot like any attempt and gives back only its own: the
// earlier failures stay counted, so exactly one failure of capacity is left (limit 3: two
// failures, a success, then one more failure fits and the next is limited).
func TestSignIn_SuccessForgivesOnlyItsOwnAttempt(t *testing.T) {
	const limit = 3
	p := newPoolRig(t, withPolicy(limitPolicy(limit)))
	u := p.signUp("ada.quill")
	const ip = "203.0.113.150"
	fail := func() error {
		_, err := p.svc.SignIn(from(ctxT(t), ip), identity.Credentials{Email: u.email, Password: pwOther})
		return keep(err)
	}
	for range limit - 1 {
		wantKind(t, fail(), errs.Unauthenticated)
	}
	_, err := p.svc.SignIn(from(ctxT(t), ip), identity.Credentials{Email: u.email, Password: u.pw})
	wantNoErr(t, err) // the success itself is not refused
	// Had the success stayed counted, this failure would already be limited; had it wiped
	// the earlier failures, the next one would not be.
	wantKind(t, fail(), errs.Unauthenticated)
	wantRateLimited(t, fail())
}
