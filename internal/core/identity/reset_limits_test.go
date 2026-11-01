package identity_test

// Tests of the amendment "2026-10-20 (D41): the reset limits are settings" in
// plan/design/identity.md: F94 (Policy.Validate), F95 and F96 (CheckSettings), and the part of
// F98 that needs no database. The reset-limiter tests in reset_test.go take their limits from
// the fake PolicySource built here, not from constants. Every address is invented.

import (
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/notify/notifytest"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// The small limits the reset-limiter tests run with (inside the policy's ranges: the client
// limit may not be below 5 or below the address limit).
const (
	testResetClientLimit  = 5
	testResetAddressLimit = 2
)

// resetPolicy returns a PolicySource with small reset limits.
func resetPolicy() *policySource {
	ps := newPolicySource()
	ps.set(func(p *identity.Policy) {
		p.PasswordResetClientLimit = testResetClientLimit
		p.PasswordResetAddressLimit = testResetAddressLimit
	})
	return ps
}

// fieldCodes returns the set of field names carried by err.
func fieldNames(err error) map[string]bool {
	out := map[string]bool{}
	for _, fe := range validate.Fields(err) {
		out[fe.Field] = true
	}
	return out
}

func TestPolicy_Default_ResetLimits(t *testing.T) {
	p := identity.DefaultPolicy()
	if p.PasswordResetClientLimit != 20 || p.PasswordResetAddressLimit != 5 || p.PasswordResetCooldown != 5*time.Minute {
		t.Errorf("defaults = %d/%d/%v, want 20/5/5m", p.PasswordResetClientLimit, p.PasswordResetAddressLimit, p.PasswordResetCooldown)
	}
	wantNoErr(t, p.Validate())
}

// F94
func TestPolicy_Validate_ResetLimits_Table(t *testing.T) {
	const (
		kClient   = "accounts.password_reset_client_limit"
		kAddress  = "accounts.password_reset_address_limit"
		kCooldown = "accounts.password_reset_cooldown"
	)
	tests := []struct {
		name string
		mod  func(*identity.Policy)
		want []string // the field names that must fail, exactly; nil = valid
	}{
		{"defaults", func(*identity.Policy) {}, nil},
		{"address floor", func(p *identity.Policy) { p.PasswordResetAddressLimit = 1 }, nil},
		{"address below floor", func(p *identity.Policy) { p.PasswordResetAddressLimit = 0 }, []string{kAddress}},
		{"address negative", func(p *identity.Policy) { p.PasswordResetAddressLimit = -1 }, []string{kAddress}},
		{"address ceiling", func(p *identity.Policy) { p.PasswordResetAddressLimit = 100; p.PasswordResetClientLimit = 100 }, nil},
		{"address above ceiling", func(p *identity.Policy) { p.PasswordResetAddressLimit = 101; p.PasswordResetClientLimit = 1000 }, []string{kAddress}},
		{"client floor", func(p *identity.Policy) { p.PasswordResetClientLimit = 5 }, nil},
		{"client below floor, dependent bound", func(p *identity.Policy) { p.PasswordResetClientLimit = 4 }, []string{kClient}},
		{"client below floor with a low address limit", func(p *identity.Policy) { p.PasswordResetAddressLimit = 1; p.PasswordResetClientLimit = 4 }, []string{kClient}},
		{"client ceiling", func(p *identity.Policy) { p.PasswordResetClientLimit = 1000 }, nil},
		{"client above ceiling", func(p *identity.Policy) { p.PasswordResetClientLimit = 1001 }, []string{kClient}},
		{"client equal to address above 5", func(p *identity.Policy) { p.PasswordResetAddressLimit = 50; p.PasswordResetClientLimit = 50 }, nil},
		{"client one below address", func(p *identity.Policy) { p.PasswordResetAddressLimit = 50; p.PasswordResetClientLimit = 49 }, []string{kClient}},
		{"client below address (address 6, client 5)", func(p *identity.Policy) { p.PasswordResetAddressLimit = 6; p.PasswordResetClientLimit = 5 }, []string{kClient}},
		{"cooldown off", func(p *identity.Policy) { p.PasswordResetCooldown = 0 }, nil},
		{"cooldown negative", func(p *identity.Policy) { p.PasswordResetCooldown = -time.Nanosecond }, []string{kCooldown}},
		{"cooldown at one hour", func(p *identity.Policy) { p.PasswordResetCooldown = time.Hour }, nil},
		{"cooldown above one hour", func(p *identity.Policy) {
			p.PasswordResetLifetime = 24 * time.Hour
			p.PasswordResetCooldown = time.Hour + time.Nanosecond
		}, []string{kCooldown}},
		{"cooldown above the lifetime", func(p *identity.Policy) {
			p.PasswordResetLifetime = 10 * time.Minute
			p.PasswordResetCooldown = 20 * time.Minute
		}, []string{kCooldown}},
		{"cooldown equal to the lifetime", func(p *identity.Policy) {
			p.PasswordResetLifetime = 10 * time.Minute
			p.PasswordResetCooldown = 10 * time.Minute
		}, nil},
		{"cooldown one past the lifetime", func(p *identity.Policy) {
			p.PasswordResetLifetime = 10 * time.Minute
			p.PasswordResetCooldown = 10*time.Minute + time.Nanosecond
		}, []string{kCooldown}},
		{"every violation at once", func(p *identity.Policy) {
			p.PasswordResetAddressLimit = 0
			p.PasswordResetClientLimit = 4
			p.PasswordResetCooldown = -time.Second
		}, []string{kAddress, kClient, kCooldown}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := identity.DefaultPolicy()
			tc.mod(&p)
			err := p.Validate()
			if len(tc.want) == 0 {
				wantNoErr(t, err)
				return
			}
			wantKind(t, err, errs.Invalid)
			got := fieldNames(err)
			for _, f := range tc.want {
				if !got[f] {
					t.Errorf("no field error for %s in %v", f, validate.Fields(err))
				}
			}
			if len(got) != len(tc.want) {
				t.Errorf("failing fields = %v, want exactly %v", got, tc.want)
			}
			for _, fe := range validate.Fields(err) {
				if fe.Code != validate.CodeRange {
					t.Errorf("%s code = %q, want range", fe.Field, fe.Code)
				}
			}
		})
	}
}

// F94 (dependent bound): the client limit's error says what it must now be at least.
func TestPolicy_Validate_ResetClientLimit_ParamsNameTheDependentBound(t *testing.T) {
	p := identity.DefaultPolicy()
	p.PasswordResetAddressLimit = 50
	p.PasswordResetClientLimit = 10
	fes := validate.Fields(p.Validate())
	if len(fes) != 1 || fes[0].Field != "accounts.password_reset_client_limit" {
		t.Fatalf("fields = %v", fes)
	}
	if fes[0].Params["min"] != "50" || fes[0].Params["max"] != "1000" {
		t.Errorf("params = %v, want min 50 max 1000", fes[0].Params)
	}
}

// F95
func TestCheckSettings_ResetClientNotBelowAddress(t *testing.T) {
	tests := []struct {
		name            string
		address, client string
		ok              bool
	}{
		{"equal", "7", "7", true},
		{"client larger", "5", "20", true},
		{"client one below", "7", "6", false},
		{"client far below", "50", "10", false},
		{"unparsable address is the key's own check", "x", "20", true},
		{"unparsable client is the key's own check", "5", "x", true},
		{"both absent", "", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := map[string]string{}
			if tc.address != "" {
				v["password_reset_address_limit"] = tc.address
			}
			if tc.client != "" {
				v["password_reset_client_limit"] = tc.client
			}
			err := identity.CheckSettings(v)
			if (err == nil) != tc.ok {
				t.Fatalf("CheckSettings = %v, want ok=%v", err, tc.ok)
			}
			if err == nil {
				return
			}
			wantKind(t, err, errs.Invalid)
			fes := validate.Fields(err)
			if len(fes) != 1 || fes[0].Field != "password_reset_client_limit" || fes[0].Code != validate.CodeRange {
				t.Fatalf("fields = %v, want one range error on password_reset_client_limit", fes)
			}
			if fes[0].Params["min"] != tc.address || fes[0].Params["max"] != "1000" {
				t.Errorf("params = %v, want min %s max 1000", fes[0].Params, tc.address)
			}
		})
	}
}

// F96
func TestCheckSettings_CooldownNotAboveLifetime(t *testing.T) {
	tests := []struct {
		name               string
		cooldown, lifetime string
		ok                 bool
	}{
		{"equal", "10m", "10m", true},
		{"below", "5m", "1h", true},
		{"off", "0s", "10m", true},
		{"one second above", "10m1s", "10m", false},
		{"far above", "1h", "10m", false},
		{"unparsable cooldown is the key's own check", "soon", "10m", true},
		{"unparsable lifetime is the key's own check", "5m", "long", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := identity.CheckSettings(map[string]string{"password_reset_cooldown": tc.cooldown, "password_reset_lifetime": tc.lifetime})
			if (err == nil) != tc.ok {
				t.Fatalf("CheckSettings = %v, want ok=%v", err, tc.ok)
			}
			if err == nil {
				return
			}
			wantKind(t, err, errs.Invalid)
			fes := validate.Fields(err)
			if len(fes) != 1 || fes[0].Field != "password_reset_cooldown" || fes[0].Code != validate.CodeRange {
				t.Fatalf("fields = %v, want one range error on password_reset_cooldown", fes)
			}
			if fes[0].Params["min"] != "0s" || fes[0].Params["max"] != tc.lifetime {
				t.Errorf("params = %v, want min 0s max %s", fes[0].Params, tc.lifetime)
			}
		})
	}
}

// The test plan's "two rules fail at once and both field errors are returned", across the
// sign-in rule and the two new ones.
func TestCheckSettings_SeveralRulesFail_AllReported(t *testing.T) {
	err := identity.CheckSettings(map[string]string{
		"login_failure_limit":          "50",
		"client_failure_limit":         "40",
		"password_reset_address_limit": "50",
		"password_reset_client_limit":  "10",
		"password_reset_cooldown":      "2h",
		"password_reset_lifetime":      "1h",
	})
	wantKind(t, err, errs.Invalid)
	got := fieldNames(err)
	for _, f := range []string{"client_failure_limit", "password_reset_client_limit", "password_reset_cooldown"} {
		if !got[f] {
			t.Errorf("missing field error for %s in %v", f, validate.Fields(err))
		}
	}
	if len(got) != 3 {
		t.Errorf("fields = %v, want exactly the three", got)
	}
}

func TestCheckSettings_ResetRulesHold_OverDefaults(t *testing.T) {
	got := map[string]string{}
	for _, d := range identity.Settings {
		got[d.Key] = d.Default
	}
	wantNoErr(t, identity.CheckSettings(got))
}

// F98, the limiter half (no database): a changed limit applies to the very next request, with
// no restart and no limiter reset; the limiter holds counts, never limits.
func TestRequestPasswordReset_PolicyChange_LimitsApplyToNextRequest(t *testing.T) {
	t.Run("address limit lowered then raised", func(t *testing.T) {
		ps := newPolicySource()
		r := newRig(t, dbtest.NewFake(), withNotify(notifytest.NewFake("identity")), withPolicy(ps))
		for range 3 { // default address limit is 5
			wantNoErr(t, requestReset(t, r, "moved.limit@example.org"))
		}
		ps.set(func(p *identity.Policy) { p.PasswordResetAddressLimit = 2 })
		wantRateLimited(t, requestReset(t, r, "moved.limit@example.org"))
		wantNoErr(t, requestReset(t, r, "other.address@example.org")) // another address is untouched
		ps.set(func(p *identity.Policy) { p.PasswordResetAddressLimit = 100; p.PasswordResetClientLimit = 1000 })
		wantNoErr(t, requestReset(t, r, "moved.limit@example.org"))
	})
	t.Run("client limit lowered then raised", func(t *testing.T) {
		ps := newPolicySource()
		r := newRig(t, dbtest.NewFake(), withNotify(notifytest.NewFake("identity")), withPolicy(ps))
		ctx := from(ctxT(t), "203.0.113.70")
		for i := range 6 { // default client limit is 20
			wantNoErr(t, keep(r.svc.RequestPasswordReset(ctx, mail(t, "c"+string(rune('a'+i))+"@example.org"))))
		}
		ps.set(func(p *identity.Policy) { p.PasswordResetClientLimit = 5 })
		wantRateLimited(t, keep(r.svc.RequestPasswordReset(ctx, mail(t, "cz@example.org"))))
		wantNoErr(t, keep(r.svc.RequestPasswordReset(from(ctxT(t), "203.0.113.71"), mail(t, "cz@example.org"))))
		ps.set(func(p *identity.Policy) { p.PasswordResetClientLimit = 1000 })
		wantNoErr(t, keep(r.svc.RequestPasswordReset(ctx, mail(t, "cz@example.org"))))
	})
}

// F62 with the new fields: a source whose policy fails Validate fails the request Invalid;
// identity does not fall back silently, and nothing is sent.
func TestRequestPasswordReset_InvalidResetPolicy_Invalid(t *testing.T) {
	tests := []struct {
		name string
		mod  func(*identity.Policy)
	}{
		// the settings Fake rejects these single-key values (declared Check) before
		// Policy.Validate runs; see TestPolicy_InvalidValues_FailInvalid (white-box)
		{"address limit zero", func(p *identity.Policy) { p.PasswordResetAddressLimit = 0 }},
		{"client below address", func(p *identity.Policy) { p.PasswordResetAddressLimit = 50; p.PasswordResetClientLimit = 10 }},
		{"cooldown negative", func(p *identity.Policy) { p.PasswordResetCooldown = -time.Second }},
		{"cooldown above lifetime", func(p *identity.Policy) {
			p.PasswordResetLifetime = 10 * time.Minute
			p.PasswordResetCooldown = 11 * time.Minute
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ps := newPolicySource()
			ps.set(tc.mod)
			n := notifytest.NewFake("identity")
			r := newRig(t, dbtest.NewFake(), withNotify(n), withPolicy(ps))
			wantKind(t, requestReset(t, r, "who.cares@example.org"), errs.Invalid)
			if len(n.Sent()) != 0 {
				t.Error("an invalid policy still sent a message")
			}
		})
	}
}

// D41: the three keys are declared right after password_reset_lifetime, in this order, with
// the kinds the amendment gives.
func TestSettings_DeclaresResetLimits_InOrderWithKinds(t *testing.T) {
	var keys []string
	for _, d := range identity.Settings {
		keys = append(keys, d.Key)
	}
	want := []string{"password_reset_client_limit", "password_reset_address_limit", "password_reset_cooldown"}
	at := -1
	for i, k := range keys {
		if k == "password_reset_lifetime" {
			at = i
		}
	}
	if at < 0 || at+len(want) >= len(keys) {
		t.Fatalf("keys = %v: no room after password_reset_lifetime", keys)
	}
	for i, k := range want {
		if keys[at+1+i] != k {
			t.Fatalf("keys after password_reset_lifetime = %v, want %v", keys[at+1:], want)
		}
	}
	kinds := map[string]settings.Kind{}
	for _, d := range identity.Settings {
		kinds[d.Key] = d.Kind
	}
	if kinds[want[0]] != settings.Int || kinds[want[1]] != settings.Int || kinds[want[2]] != settings.Duration {
		t.Errorf("kinds = %v/%v/%v, want Int/Int/Duration", kinds[want[0]], kinds[want[1]], kinds[want[2]])
	}
}
