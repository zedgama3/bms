package identity

import (
	"context"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/settings/settingstest"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
)

// Moved from app: TestSettingsPolicy_ReadsEveryKey. All eleven keys, the whole Policy.
func TestSettingsPolicy_ReadsEveryKey(t *testing.T) {
	r := settingstest.NewFake("accounts", Settings)
	r.Set("signup", "invitation")
	r.Set("session_lifetime", "48h")
	r.Set("superuser_session_lifetime", "30m")
	r.Set("password_min_length", "16")
	r.Set("login_failure_limit", "5")
	r.Set("client_failure_limit", "50")
	r.Set("login_failure_window", "10m")
	r.Set("password_reset_lifetime", "2h")
	r.Set("password_reset_client_limit", "40")
	r.Set("password_reset_address_limit", "7")
	r.Set("password_reset_cooldown", "10m")
	got, err := policyFrom(context.Background(), r)
	if err != nil {
		t.Fatalf("policyFrom: %v", err)
	}
	want := Policy{
		SignupMode: SignupInvitation, SessionLifetime: 48 * time.Hour,
		SuperuserSessionLifetime: 30 * time.Minute, PasswordMinLength: 16,
		LoginFailureLimit: 5, ClientFailureLimit: 50, LoginFailureWindow: 10 * time.Minute,
		PasswordResetLifetime:    2 * time.Hour,
		PasswordResetClientLimit: 40, PasswordResetAddressLimit: 7,
		PasswordResetCooldown: 10 * time.Minute,
	}
	if got != want {
		t.Errorf("Policy = %+v, want %+v", got, want)
	}
}

// rawReader answers fixed values without any declared Check, so the Policy.Validate step
// identity applies after policyFrom is what rejects them (the settings Fake would refuse
// the same single-key values first).
type rawReader struct{ over func(*Policy) }

func (r rawReader) Text(_ context.Context, key string) (string, error) {
	p := r.policy()
	if key == "signup" {
		return string(p.SignupMode), nil
	}
	return "", errs.E("test.rawReader", errs.Invalid, nil)
}

func (r rawReader) policy() Policy {
	p := DefaultPolicy()
	r.over(&p)
	return p
}

func (r rawReader) Duration(_ context.Context, key string) (time.Duration, error) {
	p := r.policy()
	m := map[string]time.Duration{
		"session_lifetime": p.SessionLifetime, "superuser_session_lifetime": p.SuperuserSessionLifetime,
		"login_failure_window": p.LoginFailureWindow, "password_reset_lifetime": p.PasswordResetLifetime,
		"password_reset_cooldown": p.PasswordResetCooldown,
	}
	return m[key], nil
}

func (r rawReader) Int(_ context.Context, key string) (int64, error) {
	p := r.policy()
	m := map[string]int{
		"password_min_length": p.PasswordMinLength, "login_failure_limit": p.LoginFailureLimit,
		"client_failure_limit": p.ClientFailureLimit, "password_reset_client_limit": p.PasswordResetClientLimit,
		"password_reset_address_limit": p.PasswordResetAddressLimit,
	}
	return int64(m[key]), nil
}

func (rawReader) Bool(context.Context, string) (bool, error) { return false, nil }

func (rawReader) Secret(context.Context, string) (secret.Secret, error) { return secret.Secret{}, nil }

// F62: an invalid Policy (min length 3, address limit 0, negative cooldown) fails the
// use case Invalid; identity never falls back silently to the defaults.
func TestPolicy_InvalidValues_FailInvalid(t *testing.T) {
	for name, over := range map[string]func(*Policy){
		"min length 3":      func(p *Policy) { p.PasswordMinLength = 3 },
		"address limit 0":   func(p *Policy) { p.PasswordResetAddressLimit = 0 },
		"cooldown negative": func(p *Policy) { p.PasswordResetCooldown = -time.Second },
	} {
		t.Run(name, func(t *testing.T) {
			s, _ := bareService(t)
			s.settings = rawReader{over: over}
			if _, err := s.policy(context.Background()); !errs.Is(err, errs.Invalid) {
				t.Errorf("err = %v, want Invalid", err)
			}
		})
	}
}
