//go:build integration

package identity_test

// The per-account reset cooldown and the reset limiters' independence from sign-in:
// plan/design/identity.md decision 22, F92 and F93 (owner, 2026-10-20, D40). Every address,
// token and password is invented.

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// cooldownEntries returns the Denied reset-request entries whose Reason is "cooldown".
func cooldownEntries(r *rig) []audit.Entry {
	var out []audit.Entry
	for _, e := range r.rec.Recorded() {
		if string(e.Action) == actResetRequested && e.Outcome == audit.Denied && e.Reason == "cooldown" {
			out = append(out, e)
		}
	}
	return out
}

// F92: a second request inside the cooldown answers nil, sends nothing, mints no token, keeps
// the earlier link valid and records exactly one Denied entry with Reason "cooldown".
func TestRequestPasswordReset_InsideCooldown_SilentNilNothingSent(t *testing.T) {
	r := newResetRig(t)
	u := r.signUp("ada.quill")
	first := r.requestToken(u)
	rowsBefore := r.rows()

	wantNoErr(t, r.clk.Advance(time.Minute))
	wantNoErr(t, r.request(u.email))

	if got := len(r.n.Sent()); got != 1 {
		t.Errorf("%d messages after a request inside the cooldown, want still 1", got)
	}
	if got := r.rows(); len(got) != len(rowsBefore) || string(got[0].Hash) != string(rowsBefore[0].Hash) {
		t.Error("a request inside the cooldown changed the token rows")
	}
	if got := len(cooldownEntries(r.rig)); got != 1 {
		t.Errorf("%d cooldown entries, want exactly 1", got)
	}
	// the earlier link still works
	wantNoErr(t, r.complete(first, pwNew))
	r.noLeak(first, string(u.email))
}

// F92: the boundary. Just inside is refused, exactly the cooldown later sends, and the new
// token replaces the old one.
func TestRequestPasswordReset_AfterCooldown_Sends(t *testing.T) {
	r := newResetRig(t)
	u := r.signUp("ada.quill")
	first := r.requestToken(u)

	wantNoErr(t, r.clk.Advance(defaultCooldown-time.Second))
	wantNoErr(t, r.request(u.email))
	if len(r.n.Sent()) != 1 {
		t.Fatalf("one second inside the cooldown sent a message")
	}
	wantNoErr(t, r.clk.Advance(time.Second))
	second := r.requestToken(u)
	if second == first {
		t.Error("the new request reused the old token")
	}
	wantKind(t, r.complete(first, pwNew), errs.Invalid)
	wantNoErr(t, r.complete(second, pwNew))
}

// F92: "newest reset row, used or not": completing a reset does not reopen the window.
func TestRequestPasswordReset_AfterUsedToken_StillInsideCooldown(t *testing.T) {
	r := newResetRig(t)
	u := r.signUp("ada.quill")
	tok := r.requestToken(u)
	wantNoErr(t, r.complete(tok, pwNew))
	wantNoErr(t, r.request(u.email))
	if len(r.n.Sent()) != 1 {
		t.Errorf("%d messages, want 1: a used token still counts for the cooldown", len(r.n.Sent()))
	}
	if len(cooldownEntries(r.rig)) != 1 {
		t.Errorf("%d cooldown entries, want 1", len(cooldownEntries(r.rig)))
	}
}

// The cooldown is per account, not global.
func TestRequestPasswordReset_Cooldown_IsPerAccount(t *testing.T) {
	r := newResetRig(t)
	a, b := r.signUp("ada.quill"), r.signUp("sam.gray")
	r.requestToken(a)
	r.requestToken(b)
	if len(r.n.Sent()) != 2 {
		t.Errorf("%d messages, want 2", len(r.n.Sent()))
	}
}

// F92: two simultaneous requests for one account cannot both pass the cooldown.
func TestRequestPasswordReset_Cooldown_ConcurrentRequestsSendOnce(t *testing.T) {
	r := newResetRig(t)
	u := r.signUp("ada.quill")
	var wg sync.WaitGroup
	errsOut := make([]error, 4)
	for i := range errsOut {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errsOut[i] = r.request(u.email)
		}()
	}
	wg.Wait()
	for _, err := range errsOut {
		wantNoErr(t, err)
	}
	if got := len(r.n.Sent()); got != 1 {
		t.Errorf("%d messages from 4 simultaneous requests, want 1", got)
	}
	if got := len(r.rows()); got != 1 {
		t.Errorf("%d token rows, want 1", got)
	}
}

// F92 and "the answer for an unknown address is indistinguishable": a known address, one inside
// its cooldown, and an unknown one answer the same status, body and headers; the cooldown
// request is also nil from the service.
func TestRequestPasswordReset_CooldownAnswer_IndistinguishableFromFirstAndUnknown(t *testing.T) {
	r := newResetRig(t)
	u := r.signUp("ada.quill")
	rt := handlerFor(t, r.svc, "POST", "/password-reset")

	first := callPlain(t, rt, from(ctxT(t), nextIP()), emailJSON(string(u.email)))
	inside := callPlain(t, rt, from(ctxT(t), nextIP()), emailJSON(string(u.email)))
	unknown := callPlain(t, rt, from(ctxT(t), nextIP()), emailJSON("nobody.here@example.test"))

	if first.Code != http.StatusAccepted {
		t.Fatalf("first status = %d, want 202", first.Code)
	}
	sameResponse(t, "first vs inside cooldown", first, inside)
	sameResponse(t, "first vs unknown", first, unknown)
	if len(r.n.Sent()) != 1 {
		t.Errorf("%d messages, want 1", len(r.n.Sent()))
	}
}

// F92: the cooldown path adds no Denied noise beyond the single entry, and none for the
// first request.
func TestRequestPasswordReset_FirstRequest_NoCooldownEntry(t *testing.T) {
	r := newResetRig(t)
	u := r.signUp("ada.quill")
	r.requestToken(u)
	if n := len(cooldownEntries(r.rig)); n != 0 {
		t.Errorf("%d cooldown entries after the first request, want 0", n)
	}
}

// F93 with a real account: ChangePassword still shares sign-in's buckets (D36(i)), and reset
// requests spend none of them.
func TestResetRequests_DoNotSpendChangePasswordOrSignInBuckets(t *testing.T) {
	r := newResetRig(t)
	u := r.signUp("ada.quill")
	for range identity.DefaultPolicy().PasswordResetAddressLimit {
		wantNoErr(t, r.request(u.email))
	}
	wantRateLimited(t, r.request(u.email))
	ctx := identity.WithSession(from(ctxT(t), "203.0.113.60"), u.sess)
	_, err := r.svc.ChangePassword(ctx, identity.PasswordChange{Current: pwOther, New: pwNew})
	wantField(t, err, "current_password", "incorrect")
	if _, err := r.signIn(r.svc, u); err != nil {
		t.Errorf("sign-in after a reset burst: %v", err)
	}
}

// F93, the other way, with a real account: failed ChangePassword attempts exhaust sign-in's
// buckets but a reset for the account still goes through.
func TestChangePasswordFailures_DoNotBlockReset(t *testing.T) {
	ps := newPolicySource()
	ps.set(func(p *identity.Policy) { p.LoginFailureLimit = 3 })
	r := newResetRig(t, withPolicy(ps))
	u := r.signUp("ada.quill")
	ctx := func() context.Context { return identity.WithSession(from(ctxT(t), "203.0.113.61"), u.sess) }
	for range 3 {
		_, err := r.svc.ChangePassword(ctx(), identity.PasswordChange{Current: pwOther, New: pwNew})
		wantField(t, err, "current_password", "incorrect")
	}
	_, err := r.svc.ChangePassword(ctx(), identity.PasswordChange{Current: pwGood, New: pwNew})
	wantRateLimited(t, err)
	r.requestToken(u)
}
