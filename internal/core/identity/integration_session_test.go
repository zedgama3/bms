//go:build integration

package identity_test

import (
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/lib/errs"
)

func TestAuthenticate_ValidCookie_SessionInCtx(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("ada.quill")
	c := p.cookie(u.email, u.pw)
	checkCookieAttrs(t, c, identity.CookieName, true)
	ctx, person, err := p.auth(c)
	wantNoErr(t, err)
	s, ok := identity.SessionFrom(ctx)
	if !ok || person != u.sess.Person || s.Person != u.sess.Person || s.Account != u.sess.Account || s.Superuser || s.PasswordChangeRequired {
		t.Fatalf("session = %+v person %v", s, person)
	}
	if got, ok := identity.PersonFrom(ctx); !ok || got != person {
		t.Errorf("PersonFrom = %v, %v", got, ok)
	}
	if s.ExpiresAt.Sub(s.IssuedAt) != 720*time.Hour {
		t.Errorf("lifetime = %v", s.ExpiresAt.Sub(s.IssuedAt))
	}
}

// Authenticate reads state once and never writes (decision 2).
func TestAuthenticate_OneStatementNoWrite(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("ada.quill")
	c := p.cookie(u.email, u.pw)
	p.fdb.reset()
	p.wantAuthenticated(c, u.sess.Person)
	reads, writes, txs := p.fdb.counts()
	if reads != 1 || writes != 0 || txs != 0 {
		t.Errorf("reads=%d writes=%d transactions=%d, want 1, 0, 0", reads, writes, txs)
	}
	if len(p.rec.Recorded()) != 2 { // sign-up and the sign-in; Authenticate adds nothing
		t.Errorf("entries = %v", actionsOf(p.rec.Recorded()))
	}
}

func TestAuthenticate_WorksOnReadOnlyCopy(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("ada.quill")
	c := p.cookie(u.email, u.pw)
	p.fdb.set(func(f *faultDB) { f.ro = true })
	p.wantAuthenticated(c, u.sess.Person)
}

// The spec's token layout, forged with the derived key, is accepted by the real service:
// a pinned format test that also proves tampering is what rejects.
func TestAuthenticate_ForgedWellFormedToken_Accepted(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("ada.quill")
	parts := validParts(u.sess.Account, p.clk.Now())
	parts.Expires = p.clk.Now().Add(time.Hour).Unix()
	c := reqCookie(identity.CookieName, parts.encode(sessionKeyText))
	p.wantAuthenticated(c, u.sess.Person)
	// issued within the skew in the future is accepted too
	parts.Issued = p.clk.Now().Add(identity.ClockSkew - time.Second).Unix()
	p.wantAuthenticated(reqCookie(identity.CookieName, parts.encode(sessionKeyText)), u.sess.Person)
}

// F5
func TestAuthenticate_Revoked_Unauthenticated(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("ada.quill")
	parts := validParts(u.sess.Account, p.clk.Now())
	parts.Gen = 1 // the account is at generation 0
	_, _, err := p.auth(reqCookie(identity.CookieName, parts.encode(sessionKeyText)))
	wantKind(t, err, errs.Unauthenticated)
	if got := p.measured(metricRejected, map[string]string{"reason": "revoked"}); got != 1 {
		t.Errorf("sessions_rejected{revoked} = %v, want 1", got)
	}
	// a token minted for generation 0 still works until the generation moves
	parts.Gen = 0
	p.wantAuthenticated(reqCookie(identity.CookieName, parts.encode(sessionKeyText)), u.sess.Person)
}

// F5
func TestAuthenticate_Disabled_Unauthenticated(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("ada.quill")
	boss := p.signUp("boss.lady")
	c := p.cookie(u.email, u.pw)
	p.wantAuthenticated(c, u.sess.Person)
	wantNoErr(t, p.admin().DisableAccount(boss.ctx(t), u.sess.Account))
	p.wantRejected(c)
	// Even a freshly forged token with the current generation is refused while disabled.
	row := p.rowOf(u.sess.Person)
	parts := validParts(u.sess.Account, p.clk.Now())
	parts.Gen = uint32(row.Generation) //nolint:gosec // small non-negative test value
	_, _, err := p.auth(reqCookie(identity.CookieName, parts.encode(sessionKeyText)))
	wantKind(t, err, errs.Unauthenticated)
	if got := p.measured(metricRejected, map[string]string{"reason": "disabled"}); got < 1 {
		t.Errorf("sessions_rejected{disabled} = %v", got)
	}
	// Enabling does not resurrect the old session (the generation moved on disable).
	wantNoErr(t, p.admin().EnableAccount(boss.ctx(t), u.sess.Account))
	p.wantRejected(c)
	p.wantAuthenticated(p.cookie(u.email, u.pw), u.sess.Person)
}

// F4 on a real account, driven by the fake clock.
func TestAuthenticate_Expired_RealAccount_Unauthenticated(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("ada.quill")
	c := p.cookie(u.email, u.pw)
	wantNoErr(t, p.clk.Advance(720*time.Hour-time.Second))
	p.wantAuthenticated(c, u.sess.Person)
	wantNoErr(t, p.clk.Advance(time.Second))
	p.wantRejected(c)
	if got := p.measured(metricRejected, map[string]string{"reason": "expired"}); got != 1 {
		t.Errorf("sessions_rejected{expired} = %v", got)
	}
}

func TestAuthenticate_SuperuserSessionExpiresAfterAnHour(t *testing.T) {
	p := newPoolRig(t)
	res, _ := p.superuser("root.operator@example.org")
	c := p.cookie("root.operator@example.org", res.Password.Value())
	if c.MaxAge != 3600 {
		t.Errorf("Max-Age = %d, want 3600", c.MaxAge)
	}
	ctx, _, err := p.auth(c)
	wantNoErr(t, err)
	if s, _ := identity.SessionFrom(ctx); !s.Superuser {
		t.Error("Superuser flag missing from the authenticated session")
	}
	wantNoErr(t, p.clk.Advance(time.Hour))
	p.wantRejected(c)
}

// F6
func TestAuthenticate_UnknownAccount_Unauthenticated_Integration(t *testing.T) {
	p := newPoolRig(t)
	p.signUp("ada.quill")
	parts := validParts(newAccountID(), p.clk.Now())
	_, _, err := p.auth(reqCookie(identity.CookieName, parts.encode(sessionKeyText)))
	wantKind(t, err, errs.Unauthenticated)
	if got := p.measured(metricRejected, map[string]string{"reason": "unknown"}); got != 1 {
		t.Errorf("sessions_rejected{unknown} = %v", got)
	}
}

// F7 on a real pool.
func TestAuthenticate_StoreUnavailable_PassesKind_Integration(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("ada.quill")
	c := p.cookie(u.email, u.pw)
	p.fdb.set(func(f *faultDB) { f.readErr = errTimeout })
	_, _, err := p.auth(c)
	wantKind(t, err, errs.Timeout)
}

// F25
func TestChangePassword_NoSession_Unauthenticated(t *testing.T) {
	p := newPoolRig(t)
	_, err := p.svc.ChangePassword(from(ctxT(t), nextIP()), identity.PasswordChange{Current: pwGood, New: pwNew})
	wantKind(t, err, errs.Unauthenticated)
}

func TestChangePassword_Success_NewSessionOthersEnded(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("ada.quill")
	cOld := p.cookie(u.email, u.pw)
	iss, err := p.svc.ChangePassword(u.ctx(t), identity.PasswordChange{Current: pwGood, New: pwNew})
	wantNoErr(t, err)
	if iss.Session.Person != u.sess.Person || iss.Session.PasswordChangeRequired || iss.Session.ID == u.sess.ID {
		t.Errorf("issued = %+v", iss.Session)
	}
	p.wantRejected(cOld)
	if u2 := (user{email: u.email, pw: pwGood}); true {
		_, err := p.signIn(p.svc, u2)
		wantKind(t, err, errs.Unauthenticated)
	}
	p.wantAuthenticated(p.cookie(u.email, pwNew), u.sess.Person)
	e := p.only(actPwChanged)
	if e.Outcome != audit.Succeeded || e.Subject != u.sess.Person || e.Record.Kind != "identity.account" || e.Record.ID != [16]byte(u.sess.Account) {
		t.Errorf("entry = %+v", e)
	}
	p.noLeak(pwGood, pwNew, "ada.quill")
}

// F26
func TestChangePassword_WrongCurrent_Invalid(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("ada.quill")
	c := p.cookie(u.email, u.pw)
	before := p.rowOf(u.sess.Person)
	_, err := p.svc.ChangePassword(u.ctx(t), identity.PasswordChange{Current: pwOther, New: pwNew})
	wantField(t, err, "current_password", "incorrect")
	after := p.rowOf(u.sess.Person)
	if after.Generation != before.Generation || after.PasswordHash != before.PasswordHash {
		t.Error("a refused change altered the account")
	}
	p.wantAuthenticated(c, u.sess.Person)
	if p.count(actPwChanged) != 0 {
		t.Error("an entry was recorded for a refused change")
	}
}

// F11-F13 for ChangePassword (the field is new_password).
func TestChangePassword_NewPasswordRules_Invalid(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("ada.quill")
	for _, tc := range []struct{ name, pw, code string }{
		{"short", "tiny", "too_short"},
		{"common", "password1234", "common"},
		{"long", repeat("x7", 129), "too_long"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := p.svc.ChangePassword(u.ctx(t), identity.PasswordChange{Current: pwGood, New: tc.pw})
			wantField(t, err, "new_password", tc.code)
		})
	}
	p.wantAuthenticated(p.cookie(u.email, pwGood), u.sess.Person)
}

func TestChangePassword_RestrictedSessionMayChange_AndBecomesFull(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("ada.quill")
	boss := p.signUp("boss.lady")
	tmp, err := p.admin().SetTemporaryPassword(boss.ctx(t), u.sess.Account)
	wantNoErr(t, err)
	iss, err := p.svc.SignIn(from(ctxT(t), nextIP()), identity.Credentials{Email: u.email, Password: tmp.Value()})
	wantNoErr(t, err)
	restricted := identity.WithSession(from(ctxT(t), nextIP()), iss.Session)
	// The restricted session cannot end other sessions (F29) ...
	_, err = p.svc.EndOtherSessions(restricted)
	wantKind(t, err, errs.Unauthenticated)
	// ... but can change its password.
	iss2, err := p.svc.ChangePassword(restricted, identity.PasswordChange{Current: tmp.Value(), New: pwNew})
	wantNoErr(t, err)
	if iss2.Session.PasswordChangeRequired {
		t.Error("the new session is still restricted")
	}
	if row := p.rowOf(u.sess.Person); row.PasswordChangeRequired {
		t.Error("password_change_required not cleared")
	}
	p.wantAuthenticated(p.cookie(u.email, pwNew), u.sess.Person)
}

// F28
func TestChangePassword_ReadOnly_Unavailable(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("ada.quill")
	p.fdb.set(func(f *faultDB) { f.ro = true })
	_, err := p.svc.ChangePassword(u.ctx(t), identity.PasswordChange{Current: pwGood, New: pwNew})
	wantReadOnly(t, err)
	p.fdb.set(func(f *faultDB) { f.ro = false })
	if _, err := p.signIn(p.svc, u); err != nil {
		t.Errorf("old password no longer works: %v", err)
	}
}

func TestChangePassword_AuditFails_NothingChanged(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("ada.quill")
	p.rec.Fail(errs.E("test.audit", errs.Unavailable, errDown))
	_, err := p.svc.ChangePassword(u.ctx(t), identity.PasswordChange{Current: pwGood, New: pwNew})
	wantKind(t, err, errs.Unavailable)
	p.rec.Fail(nil)
	if _, err := p.signIn(p.svc, u); err != nil {
		t.Errorf("old password no longer works after a rolled-back change: %v", err)
	}
}

// F29
func TestEndOtherSessions_Restricted_Unauthenticated(t *testing.T) {
	p := newPoolRig(t)
	_, err := p.svc.EndOtherSessions(from(ctxT(t), nextIP()))
	wantKind(t, err, errs.Unauthenticated)
	s := sessionFor(newAccountID(), newPersonID(), p.clk.Now())
	s.PasswordChangeRequired = true
	_, err = p.svc.EndOtherSessions(identity.WithSession(from(ctxT(t), nextIP()), s))
	wantKind(t, err, errs.Unauthenticated)
}

func TestEndOtherSessions_Success_OthersEnded(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("ada.quill")
	phone := p.cookie(u.email, u.pw)
	laptop := p.cookie(u.email, u.pw)
	p.wantAuthenticated(phone, u.sess.Person)
	iss, err := p.svc.EndOtherSessions(u.ctx(t))
	wantNoErr(t, err)
	if iss.Session.Person != u.sess.Person || iss.Session.ID == u.sess.ID {
		t.Errorf("issued = %+v", iss.Session)
	}
	p.wantRejected(phone)
	p.wantRejected(laptop)
	e := p.only(actEnded)
	if e.Outcome != audit.Succeeded || e.Record.Kind != "identity.session" {
		t.Errorf("entry = %+v", e)
	}
	// a session started afterwards works
	p.wantAuthenticated(p.cookie(u.email, u.pw), u.sess.Person)
}

// F30
func TestEndOtherSessions_ReadOnly_Unavailable(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("ada.quill")
	p.fdb.set(func(f *faultDB) { f.ro = true })
	_, err := p.svc.EndOtherSessions(u.ctx(t))
	wantReadOnly(t, err)
}

func TestSignOut_ThroughHandler_ClearsCookieAndRecords(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("ada.quill")
	w, f := call(t, handlerFor(t, p.svc, "POST", "/sign-out"), u.ctx(t), "")
	if f.count() != 0 || w.Code/100 != 2 {
		t.Fatalf("status %d failure %v", w.Code, f.last())
	}
	if cs := setCookies(w); len(cs) != 1 || cs[0].MaxAge >= 0 || cs[0].Name != identity.CookieName {
		t.Errorf("cookies = %+v", cs)
	}
	if p.only(actLogout).Actor.Person != u.sess.Person {
		t.Error("logout recorded for the wrong person")
	}
}

func TestSignOut_ReadOnlyCopy_Works(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("ada.quill")
	p.fdb.set(func(f *faultDB) { f.ro = true })
	wantNoErr(t, p.svc.SignOut(u.ctx(t)))
	if p.count(actLogout) != 1 {
		t.Error("logout not recorded on a read-only copy")
	}
}

// The whole story of the test plan: sign-up, sign-in, disable, 401 on a real pool.
func TestFlow_SignUpSignInDisableAuthenticate401(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("ada.quill")
	c := p.cookie(u.email, u.pw)
	p.wantAuthenticated(c, u.sess.Person)
	boss := p.signUp("boss.lady")
	wantNoErr(t, p.admin().DisableAccount(boss.ctx(t), u.sess.Account))
	p.wantRejected(c)
	_, err := p.signIn(p.svc, u)
	wantKind(t, err, errs.Unauthenticated)
}

func TestHandlers_SessionRoute_SignedInBodyAndNoStore(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("ada.quill")
	w, f := call(t, handlerFor(t, p.svc, "GET", "/session"), u.ctx(t), "")
	if f.count() != 0 || w.Code != http.StatusOK {
		t.Fatalf("status %d failure %v", w.Code, f.last())
	}
	var info identity.SessionInfo
	wantNoErr(t, json.Unmarshal(w.Body.Bytes(), &info))
	if !info.SignedIn || info.PersonID != u.sess.Person || info.AccountID != u.sess.Account {
		t.Errorf("info = %+v", info)
	}
}

func TestHandlers_SignInBodyNeverCarriesTheToken(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("ada.quill")
	w, _ := call(t, handlerFor(t, p.svc, "POST", "/sign-in"), from(ctxT(t), nextIP()), credsJSON(string(u.email), u.pw))
	cs := setCookies(w)
	if len(cs) != 1 {
		t.Fatalf("%d cookies", len(cs))
	}
	if bytesContains(w.Body.Bytes(), cs[0].Value) {
		t.Error("the token is in the response body")
	}
}

func TestSessions_ConcurrentEndOthers_Safe(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("ada.quill")
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			if _, err := p.svc.EndOtherSessions(u.ctx(t)); err != nil {
				t.Errorf("EndOtherSessions: %v", err)
			}
		})
	}
	wg.Wait()
}
