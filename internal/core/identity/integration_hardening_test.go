//go:build integration

package identity_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/identity"
)

func TestSignIn_ConcurrentWrongGuesses_KnownAccount_AtMostLimitVerifications(t *testing.T) {
	const limit, burst = 3, 20
	p := newPoolRig(t, withPolicy(limitPolicy(limit)))
	u := p.signUp("target.person")
	unauth, limited, other := tally(t, p.rig, burst, string(u.email), pwOther)
	if other != 0 {
		t.Fatalf("%d sign-ins ended with another error", other)
	}
	if unauth > limit || unauth+limited != burst || limited < burst-limit {
		t.Errorf("unauthenticated=%d rate-limited=%d of %d (limit %d)", unauth, limited, burst, limit)
	}
	if got := p.count(actLoginFailed); got > limit {
		t.Errorf("%d login-failed entries, want at most %d", got, limit)
	}
	if p.count(actLogin) != 0 {
		t.Error("a wrong password signed in")
	}
}

// ChangePassword's current-password check is a guessing oracle for a stolen session, so it
// is limited like sign-in.
func TestChangePassword_WrongCurrent_RateLimited(t *testing.T) {
	p := newPoolRig(t, withPolicy(limitPolicy(3)))
	u := p.signUp("ada.quill")
	ctx := func() context.Context { return identity.WithSession(from(ctxT(t), "203.0.113.5"), u.sess) }
	for range 3 {
		_, err := p.svc.ChangePassword(ctx(), identity.PasswordChange{Current: pwOther, New: pwNew})
		wantField(t, err, "current_password", "incorrect")
	}
	// now even the right current password is refused
	_, err := p.svc.ChangePassword(ctx(), identity.PasswordChange{Current: pwGood, New: pwNew})
	wantRateLimited(t, err)
	if p.count(actPwChanged) != 0 {
		t.Error("a limited attempt changed the password")
	}
	// Intended behaviour: ChangePassword shares sign-in's buckets (client address and the
	// account's canonical address), which bounds the total guesses against one password per
	// window. So signing in with the old, correct password is limited too ...
	_, err = p.signIn(p.svc, u)
	wantRateLimited(t, err)
	// ... until the window has passed; the limited attempts changed nothing.
	wantNoErr(t, p.clk.Advance(16*time.Minute))
	if _, err := p.signIn(p.svc, u); err != nil {
		t.Errorf("the old password stopped working: %v", err)
	}
}

// Reads that must never touch the credential: no statement mentions the hash column.
func TestReads_NeverSelectThePasswordHash(t *testing.T) {
	f := newAdminFixture(t)
	u := f.signUp("sam.gray")
	c := f.cookie(u.email, u.pw)
	check := func(name string) {
		t.Helper()
		stmts := f.fdb.statements()
		if len(stmts) == 0 {
			t.Fatalf("%s ran no statement", name)
		}
		for _, s := range stmts {
			if strings.Contains(strings.ToLower(s), "password_hash") {
				t.Errorf("%s ran a statement that reads password_hash: %.80q", name, strings.Join(strings.Fields(s), " "))
			}
		}
	}
	f.fdb.reset()
	_, err := f.adm.GetAccount(f.boss.ctx(t), u.sess.Account)
	wantNoErr(t, err)
	check("GetAccount")
	f.fdb.reset()
	_, err = f.adm.ListAccounts(f.boss.ctx(t), identity.AccountQuery{})
	wantNoErr(t, err)
	check("ListAccounts")
	f.fdb.reset()
	_, err = f.adm.ListAccounts(f.boss.ctx(t), identity.AccountQuery{Email: u.email})
	wantNoErr(t, err)
	check("ListAccounts by e-mail")
	f.fdb.reset()
	_, err = f.svc.ExportPerson(ctxT(t), u.sess.Person)
	wantNoErr(t, err)
	check("ExportPerson")
	f.fdb.reset()
	f.wantAuthenticated(c, u.sess.Person)
	check("Authenticate")
	// sanity: the capture does see the hash being read when a password is verified
	f.fdb.reset()
	_, err = f.signIn(f.svc, u)
	wantNoErr(t, err)
	seen := false
	for _, s := range f.fdb.statements() {
		seen = seen || strings.Contains(strings.ToLower(s), "password_hash")
	}
	if !seen {
		t.Error("the capture never saw the hash read during SignIn: the check above proves nothing")
	}
}
