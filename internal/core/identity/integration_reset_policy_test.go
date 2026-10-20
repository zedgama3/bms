//go:build integration

package identity_test

// F97 and F98 against a real database (plan/design/identity.md, amendment 2026-10-20, D41):
// the cooldown as a setting. Every address, token and password is invented.

import (
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// F97: with the cooldown at 0 a request seconds after the last one sends again, replaces the
// token, and records no "cooldown" entry.
func TestRequestPasswordReset_CooldownOff_AlwaysSends(t *testing.T) {
	ps := newPolicySource()
	ps.set(func(p *identity.Policy) { p.PasswordResetCooldown = 0 })
	r := newResetRig(t, withPolicy(ps))
	u := r.signUp("ada.quill")

	first := r.requestToken(u)
	wantNoErr(t, r.clk.Advance(2*time.Second))
	second := r.requestToken(u)
	if first == second {
		t.Error("the second request reused the first token")
	}
	if n := len(r.n.Sent()); n != 2 {
		t.Errorf("%d messages, want 2", n)
	}
	if n := len(cooldownEntries(r.rig)); n != 0 {
		t.Errorf("%d cooldown entries with the cooldown off, want 0", n)
	}
	wantKind(t, r.complete(first, pwNew), errs.Invalid) // replaced
	wantNoErr(t, r.complete(second, pwNew))
	r.noLeak(first, second, string(u.email))
}

// F97: the per-address bucket still bounds mail when the cooldown is off.
func TestRequestPasswordReset_CooldownOff_AddressBucketStillBounds(t *testing.T) {
	ps := resetPolicy()
	ps.set(func(p *identity.Policy) { p.PasswordResetCooldown = 0 })
	r := newResetRig(t, withPolicy(ps))
	u := r.signUp("ada.quill")
	for range testResetAddressLimit {
		wantNoErr(t, r.request(u.email))
	}
	wantRateLimited(t, r.request(u.email))
	if n := len(r.n.Sent()); n != testResetAddressLimit {
		t.Errorf("%d messages, want %d", n, testResetAddressLimit)
	}
}

// F98: a change between two requests applies to the second, with no restart.
func TestRequestPasswordReset_PolicyChange_AppliesToNextRequest(t *testing.T) {
	t.Run("address limit lowered below the count, then raised", func(t *testing.T) {
		ps := newPolicySource()
		r := newResetRig(t, withPolicy(ps))
		u := r.signUp("ada.quill")
		ps.set(func(p *identity.Policy) { p.PasswordResetCooldown = 0 })
		for range 3 {
			wantNoErr(t, r.request(u.email))
		}
		ps.set(func(p *identity.Policy) { p.PasswordResetAddressLimit = 2 })
		wantRateLimited(t, r.request(u.email))
		ps.set(func(p *identity.Policy) { p.PasswordResetAddressLimit = 100; p.PasswordResetClientLimit = 1000 })
		wantNoErr(t, r.request(u.email))
	})
	t.Run("cooldown shortened below the newest row's age sends", func(t *testing.T) {
		ps := newPolicySource()
		r := newResetRig(t, withPolicy(ps))
		u := r.signUp("ada.quill")
		r.requestToken(u)
		wantNoErr(t, r.clk.Advance(time.Minute))
		wantNoErr(t, r.request(u.email))
		if len(r.n.Sent()) != 1 {
			t.Fatalf("%d messages inside the 5m cooldown, want 1", len(r.n.Sent()))
		}
		ps.set(func(p *identity.Policy) { p.PasswordResetCooldown = 30 * time.Second })
		r.requestToken(u) // sends, no restart
		if len(cooldownEntries(r.rig)) != 1 {
			t.Errorf("%d cooldown entries, want only the earlier one", len(cooldownEntries(r.rig)))
		}
	})
	t.Run("cooldown lengthened applies to the next request", func(t *testing.T) {
		ps := newPolicySource()
		ps.set(func(p *identity.Policy) { p.PasswordResetCooldown = time.Minute })
		r := newResetRig(t, withPolicy(ps))
		u := r.signUp("ada.quill")
		r.requestToken(u)
		wantNoErr(t, r.clk.Advance(2*time.Minute)) // past 1m: would send
		ps.set(func(p *identity.Policy) { p.PasswordResetCooldown = 10 * time.Minute })
		wantNoErr(t, r.request(u.email))
		if len(r.n.Sent()) != 1 {
			t.Errorf("%d messages, want 1: the lengthened cooldown must apply at once", len(r.n.Sent()))
		}
		wantNoErr(t, r.clk.Advance(8*time.Minute))
		r.requestToken(u)
	})
	t.Run("cooldown turned off then on", func(t *testing.T) {
		ps := newPolicySource()
		r := newResetRig(t, withPolicy(ps))
		u := r.signUp("ada.quill")
		r.requestToken(u)
		ps.set(func(p *identity.Policy) { p.PasswordResetCooldown = 0 })
		r.requestToken(u)
		ps.set(func(p *identity.Policy) { p.PasswordResetCooldown = defaultCooldown })
		wantNoErr(t, r.request(u.email))
		if len(r.n.Sent()) != 2 {
			t.Errorf("%d messages, want 2", len(r.n.Sent()))
		}
	})
}
