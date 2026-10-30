//go:build integration

package identity_test

// The demo amendment (D53): EnsureAccount and the Guard seam. plan/design/identity.md,
// "Amendment 2026-10-28 (approved, owner, D53)", F99 to F108. Every address and password
// is invented.

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// fakeGuard protects the accounts it is told to; err makes every question fail.
type fakeGuard struct {
	mu        sync.Mutex
	protected map[model.AccountID]bool
	err       error
	asked     int
}

func newFakeGuard() *fakeGuard { return &fakeGuard{protected: map[model.AccountID]bool{}} }

func (g *fakeGuard) protect(a model.AccountID) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.protected[a] = true
}

func (g *fakeGuard) failWith(err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.err = err
}

func (g *fakeGuard) Protected(_ context.Context, a model.AccountID) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.asked++
	if g.err != nil {
		return false, g.err
	}
	return g.protected[a], nil
}

func withGuard(g identity.Guard) func(*identity.Options) {
	return func(o *identity.Options) { o.Guard = g }
}

// wantProtected checks Permission with field "account" and code "protected".
func wantProtected(t testing.TB, err error) {
	t.Helper()
	wantKind(t, err, errs.Permission)
	for _, fe := range validate.Fields(err) {
		if fe.Field == "account" && fe.Code == "protected" {
			return
		}
	}
	t.Fatalf("no field error account/protected in %v", validate.Fields(err))
}

const demoPW = "burn-demo-password" //nolint:gosec // G101: the demo account password is public by design (demo.md)

// ---- EnsureAccount (F99 to F102) --------------------------------------------------------

func TestEnsureAccount_New_CreatesAccountPublishesAndAudits(t *testing.T) {
	p := newPoolRig(t)
	res, err := p.svc.EnsureAccount(ctxT(t), identity.Credentials{Email: mail(t, "participant@demo.invalid"), Password: demoPW})
	wantNoErr(t, err)
	if !res.Created || res.Account.IsZero() || res.Person.IsZero() {
		t.Fatalf("result = %+v, want Created with IDs", res)
	}
	if got := p.accountCount(); got != 1 {
		t.Fatalf("%d accounts, want 1", got)
	}
	if n := len(p.bus.Published()); n != 1 {
		t.Fatalf("%d signals published, want identity.account-created", n)
	}
	e := p.only(actCreated)
	if e.Actor.Kind != audit.ActorOperator {
		t.Errorf("actor kind = %v, want ActorOperator", e.Actor.Kind)
	}
	// The account can sign in with the password.
	_, err = p.svc.SignIn(from(ctxT(t), nextIP()), identity.Credentials{Email: mail(t, "participant@demo.invalid"), Password: demoPW})
	wantNoErr(t, err)
}

func TestEnsureAccount_WeakPassword_ReturnsInvalid(t *testing.T) {
	p := newPoolRig(t)
	for name, pw := range map[string]string{"short": pwShort, "common": pwCommon, "empty": ""} {
		t.Run(name, func(t *testing.T) {
			_, err := p.svc.EnsureAccount(ctxT(t), identity.Credentials{Email: mail(t, "lead@demo.invalid"), Password: pw})
			wantKind(t, err, errs.Invalid)
		})
	}
	if got := p.accountCount(); got != 0 {
		t.Fatalf("%d accounts after refused passwords, want 0", got)
	}
}

func TestEnsureAccount_SuperuserAddress_ReturnsConflict(t *testing.T) {
	p := newPoolRig(t)
	p.superuser("root.person@example.org")
	_, err := p.svc.EnsureAccount(ctxT(t), identity.Credentials{Email: mail(t, "root.person@example.org"), Password: demoPW})
	wantKind(t, err, errs.Conflict)
	if got := p.rowOf(mustPerson(t, p, "root.person@example.org")).Superuser; !got {
		t.Error("the superuser row changed")
	}
}

func mustPerson(t *testing.T, p *prig, email string) model.PersonID {
	t.Helper()
	var id [16]byte
	if err := p.pool.QueryRow(ctxT(t), "SELECT person_id FROM core.account WHERE lower(email) = lower($1)", email).Scan(&id); err != nil {
		t.Fatalf("looking up an account: %v", err)
	}
	return model.PersonID(id)
}

func TestEnsureAccount_ReadOnly_ReturnsUnavailable(t *testing.T) {
	p := newPoolRig(t)
	p.fdb.set(func(f *faultDB) { f.ro = true })
	_, err := p.svc.EnsureAccount(ctxT(t), identity.Credentials{Email: mail(t, "lead@demo.invalid"), Password: demoPW})
	wantReadOnly(t, err)
}

func TestEnsureAccount_Existing_ResetsPassword(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUpAs("steward@demo.invalid", pwGood)
	oldCookie := p.cookie(u.email, pwGood)
	boss := p.signUp("boss.lady")
	wantNoErr(t, p.admin().DisableAccount(boss.ctx(t), u.sess.Account))

	res, err := p.svc.EnsureAccount(ctxT(t), identity.Credentials{Email: u.email, Password: demoPW})
	wantNoErr(t, err)
	if res.Created {
		t.Error("Created = true for an existing address")
	}
	if res.Account != u.sess.Account || res.Person != u.sess.Person {
		t.Error("result names another account")
	}
	// New password works, the old one does not, the account is enabled again, old sessions are over.
	_, err = p.svc.SignIn(from(ctxT(t), nextIP()), identity.Credentials{Email: u.email, Password: demoPW})
	wantNoErr(t, err)
	_, err = p.svc.SignIn(from(ctxT(t), nextIP()), identity.Credentials{Email: u.email, Password: pwGood})
	wantKind(t, err, errs.Unauthenticated)
	p.wantRejected(oldCookie)
	if n := p.accountCount(); n != 2 {
		t.Errorf("%d accounts, want 2 (no duplicate)", n)
	}
	if n := p.count(actPwChanged); n < 1 {
		t.Error("no identity.password.changed entry")
	}
}

func TestEnsureAccount_NoGuardConsulted(t *testing.T) {
	g := newFakeGuard()
	p := newPoolRig(t, withGuard(g))
	u := p.signUpAs("lead@demo.invalid", pwGood)
	g.protect(u.sess.Account)
	_, err := p.svc.EnsureAccount(ctxT(t), identity.Credentials{Email: u.email, Password: demoPW})
	wantNoErr(t, err) // EnsureAccount itself is not guarded
}

// ---- the Guard (F103 to F108) -----------------------------------------------------------

type guardRig struct {
	*prig
	g      *fakeGuard
	target user
	boss   user
}

func newGuardRig(t *testing.T) *guardRig {
	t.Helper()
	g := newFakeGuard()
	p := newPoolRig(t, withGuard(g), func(o *identity.Options) { o.Notify = nil })
	r := &guardRig{prig: p, g: g}
	r.target = p.signUpAs("participant@demo.invalid", demoPW)
	r.boss = p.signUp("boss.lady")
	return r
}

func TestChangePassword_Protected_ReturnsPermission(t *testing.T) {
	r := newGuardRig(t)
	r.g.protect(r.target.sess.Account)
	_, err := r.svc.ChangePassword(r.target.ctx(t), identity.PasswordChange{Current: demoPW, New: pwNew})
	wantProtected(t, err)
	// The password is unchanged.
	_, err = r.svc.SignIn(from(ctxT(t), nextIP()), identity.Credentials{Email: r.target.email, Password: demoPW})
	wantNoErr(t, err)
}

// D53: a shared demo account cannot be re-pointed at another person (LinkAccount), or the
// shared login would stop belonging to its demo person.
func TestLinkAccount_Protected_ReturnsPermission(t *testing.T) {
	r := newGuardRig(t)
	r.g.protect(r.target.sess.Account)
	walkOn := r.signUp("walk.on.zed").sess.Person
	_, _, err := r.svc.LinkAccount(ctxT(t), model.Email(r.target.email), walkOn)
	wantProtected(t, err)
	if got := mustPerson(t, r.prig, string(r.target.email)); got != r.target.sess.Person {
		t.Errorf("the protected account moved to person %v", got)
	}
}

func TestLinkAccount_Unprotected_Allowed(t *testing.T) {
	r := newGuardRig(t)
	walkOn := r.signUp("walk.on.yan").sess.Person
	_, _, err := r.svc.LinkAccount(ctxT(t), model.Email(r.boss.email), walkOn)
	if errs.Is(err, errs.Permission) {
		t.Fatalf("an unprotected account was refused: %v", err)
	}
}

func TestChangePassword_Unprotected_Allowed(t *testing.T) {
	r := newGuardRig(t)
	_, err := r.svc.ChangePassword(r.boss.ctx(t), identity.PasswordChange{Current: pwGood, New: pwNew})
	wantNoErr(t, err)
}

func TestSetTemporaryPassword_Protected_ReturnsPermission(t *testing.T) {
	r := newGuardRig(t)
	r.g.protect(r.target.sess.Account)
	_, err := r.admin().SetTemporaryPassword(r.boss.ctx(t), r.target.sess.Account)
	wantProtected(t, err)
	if r.count(actTempSet) != 0 {
		t.Error("a temporary-set entry was recorded for a refused change")
	}
}

func TestDisableAccount_Protected_ReturnsPermission(t *testing.T) {
	r := newGuardRig(t)
	r.g.protect(r.target.sess.Account)
	err := r.admin().DisableAccount(r.boss.ctx(t), r.target.sess.Account)
	wantProtected(t, err)
	if r.rowOf(r.target.sess.Person).DisabledAt != nil {
		t.Error("the protected account was disabled")
	}
}

func TestEnableAccount_SignOut_EndOthers_Protected_StayAllowed(t *testing.T) {
	r := newGuardRig(t)
	other := r.signUp("other.person")
	wantNoErr(t, r.admin().DisableAccount(r.boss.ctx(t), other.sess.Account))
	r.g.protect(other.sess.Account)
	r.g.protect(r.target.sess.Account)
	wantNoErr(t, r.admin().EnableAccount(r.boss.ctx(t), other.sess.Account))
	_, err := r.svc.EndOtherSessions(r.target.ctx(t))
	wantNoErr(t, err)
	wantNoErr(t, r.svc.SignOut(r.target.ctx(t)))
}

func TestDeletePerson_Protected_ReturnsPermission(t *testing.T) {
	r := newGuardRig(t)
	r.g.protect(r.target.sess.Account)
	n, err := r.svc.DeletePerson(ctxT(t), r.target.sess.Person)
	wantProtected(t, err)
	if n != 0 {
		t.Errorf("deleted %d rows", n)
	}
	if got := r.accountCount(); got != 2 {
		t.Errorf("%d accounts, want 2", got)
	}
}

func TestRequestPasswordReset_Protected_SendsNothing(t *testing.T) {
	g := newFakeGuard()
	rr := newResetRig(t, withGuard(g))
	u := rr.signUpAs("participant@demo.invalid", demoPW)
	g.protect(u.sess.Account)
	wantNoErr(t, rr.request(u.email))
	if n := len(rr.n.Sent()); n != 0 {
		t.Fatalf("%d messages sent for a protected account", n)
	}
	if n := len(rr.rows()); n != 0 {
		t.Fatalf("%d reset tokens stored for a protected account", n)
	}
}

func TestCompletePasswordReset_Protected_ReturnsPermission(t *testing.T) {
	g := newFakeGuard()
	rr := newResetRig(t, withGuard(g))
	u := rr.signUpAs("participant@demo.invalid", demoPW)
	token := rr.requestToken(u) // issued while unprotected (for example before the demo seed ran)
	g.protect(u.sess.Account)
	err := rr.complete(token, pwNew)
	wantProtected(t, err)
	_, err = rr.svc.SignIn(from(ctxT(t), nextIP()), identity.Credentials{Email: u.email, Password: demoPW})
	wantNoErr(t, err)
}

func TestGuard_Error_FailsTheUseCase(t *testing.T) {
	boom := errs.E("test.guard", errs.Unavailable, errors.New("guard store down"))
	r := newGuardRig(t)
	r.g.failWith(boom)
	cases := map[string]func() error{
		"ChangePassword": func() error {
			_, err := r.svc.ChangePassword(r.target.ctx(t), identity.PasswordChange{Current: demoPW, New: pwNew})
			return wrapT(err)
		},
		"SetTemporaryPassword": func() error {
			_, err := r.admin().SetTemporaryPassword(r.boss.ctx(t), r.target.sess.Account)
			return wrapT(err)
		},
		"DisableAccount": func() error { return r.admin().DisableAccount(r.boss.ctx(t), r.target.sess.Account) },
		"DeletePerson": func() error {
			_, err := r.svc.DeletePerson(ctxT(t), r.target.sess.Person)
			return wrapT(err)
		},
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			wantKind(t, call(), errs.Unavailable)
		})
	}
	if r.rowOf(r.target.sess.Person).DisabledAt != nil {
		t.Error("a failing guard let DisableAccount through")
	}
	if got := r.accountCount(); got != 2 {
		t.Errorf("%d accounts, want 2: a failing guard let DeletePerson through", got)
	}
}

func TestGuard_Error_FailsResetUseCases(t *testing.T) {
	g := newFakeGuard()
	rr := newResetRig(t, withGuard(g))
	u := rr.signUpAs("participant@demo.invalid", demoPW)
	token := rr.requestToken(u)
	g.failWith(errs.E("test.guard", errs.Unavailable, errors.New("down")))
	wantKind(t, rr.complete(token, pwNew), errs.Unavailable)
	before := len(rr.n.Sent())
	wantKind(t, rr.request(u.email), errs.Unavailable)
	if len(rr.n.Sent()) != before {
		t.Error("a message was sent although the guard failed")
	}
}

func TestGuard_Nil_ProtectsNothing(t *testing.T) {
	p := newPoolRig(t) // no Guard
	u := p.signUpAs("participant@demo.invalid", demoPW)
	boss := p.signUp("boss.lady")
	_, err := p.svc.ChangePassword(u.ctx(t), identity.PasswordChange{Current: demoPW, New: pwNew})
	wantNoErr(t, err)
	wantNoErr(t, p.admin().DisableAccount(boss.ctx(t), u.sess.Account))
	_, err = p.svc.DeletePerson(ctxT(t), u.sess.Person)
	wantNoErr(t, err)
}

// wrapT adds the test context to an error from the service under test, keeping its kind.
func wrapT(err error) error {
	if err != nil {
		return fmt.Errorf("demo guard: %w", err)
	}
	return nil
}
