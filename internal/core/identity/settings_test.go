package identity_test

import (
	"strconv"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

func TestSettings_DeclarationsAreValidAndDefaultToDefaultPolicy(t *testing.T) {
	a := settings.Area{Name: "accounts", Core: true, Decls: identity.Settings, Check: identity.CheckSettings}
	if err := a.Validate(); err != nil {
		t.Fatalf("Area.Validate: %v", err)
	}
	// The defaults are the policy's defaults.
	def := identity.DefaultPolicy()
	got := map[string]string{}
	for _, d := range identity.Settings {
		got[d.Key] = d.Default
	}
	want := map[string]string{
		"signup":                     string(def.SignupMode),
		"session_lifetime":           def.SessionLifetime.String(),
		"superuser_session_lifetime": def.SuperuserSessionLifetime.String(),
		"password_min_length":        "12",
		"login_failure_limit":        "10",
		"client_failure_limit":       "100",
		"login_failure_window":       def.LoginFailureWindow.String(),
		"password_reset_lifetime":    def.PasswordResetLifetime.String(),
		// D41: the reset limits default to DefaultPolicy's fields (20, 5, 5m).
		"password_reset_client_limit":  strconv.Itoa(def.PasswordResetClientLimit),
		"password_reset_address_limit": strconv.Itoa(def.PasswordResetAddressLimit),
		"password_reset_cooldown":      def.PasswordResetCooldown.String(),
	}
	if want["password_reset_client_limit"] != "20" || want["password_reset_address_limit"] != "5" || def.PasswordResetCooldown != 5*time.Minute {
		t.Errorf("DefaultPolicy reset limits = %s/%s/%v, want 20/5/5m", want["password_reset_client_limit"], want["password_reset_address_limit"], def.PasswordResetCooldown)
	}
	for k, w := range want {
		g := got[k]
		if k == "session_lifetime" || k == "superuser_session_lifetime" || k == "login_failure_window" || k == "password_reset_lifetime" || k == "password_reset_cooldown" {
			gd, gerr := time.ParseDuration(g)
			wd, werr := time.ParseDuration(w)
			if gerr != nil || werr != nil || gd != wd {
				t.Errorf("%s default = %q, want %q", k, g, w)
			}
			continue
		}
		if g != w {
			t.Errorf("%s default = %q, want %q", k, g, w)
		}
	}
	if len(got) != len(want) {
		t.Errorf("declares %d keys, want %d", len(got), len(want))
	}
	if err := identity.CheckSettings(got); err != nil {
		t.Errorf("CheckSettings over the defaults: %v", err)
	}
}

func TestSettings_Checks_AgreeWithPolicyValidate(t *testing.T) {
	byKey := map[string]settings.Decl{}
	for _, d := range identity.Settings {
		byKey[d.Key] = d
	}
	tests := []struct {
		key, value string
		ok         bool
	}{
		{"session_lifetime", "1h", true},
		{"session_lifetime", "8760h", true},
		{"session_lifetime", "30m", false},
		{"session_lifetime", "8761h", false},
		{"superuser_session_lifetime", "10m", true},
		{"superuser_session_lifetime", "9m", false},
		{"password_min_length", "12", true},
		{"password_min_length", "128", true},
		{"password_min_length", "11", false},
		{"password_min_length", "129", false},
		{"login_failure_limit", "3", true},
		{"login_failure_limit", "2", false},
		{"login_failure_limit", "101", false},
		{"client_failure_limit", "1000", true},
		{"client_failure_limit", "9", false},
		{"login_failure_window", "24h", true},
		{"login_failure_window", "30s", false},
		// D41: lo-1, lo, hi, hi+1 of the three reset keys.
		{"password_reset_client_limit", "4", false},
		{"password_reset_client_limit", "5", true},
		{"password_reset_client_limit", "1000", true},
		{"password_reset_client_limit", "1001", false},
		{"password_reset_address_limit", "0", false},
		{"password_reset_address_limit", "1", true},
		{"password_reset_address_limit", "100", true},
		{"password_reset_address_limit", "101", false},
		{"password_reset_cooldown", "-1s", false},
		{"password_reset_cooldown", "0s", true},
		{"password_reset_cooldown", "0", true},
		{"password_reset_cooldown", "1h", true},
		{"password_reset_cooldown", "1h1s", false},
	}
	for _, tc := range tests {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			err := byKey[tc.key].Check(tc.value)
			if (err == nil) != tc.ok {
				t.Fatalf("Check(%q) = %v, want ok=%v", tc.value, err, tc.ok)
			}
			if err != nil {
				if fe := validate.Fields(err); len(fe) != 1 || fe[0].Code != validate.CodeRange {
					t.Errorf("Check(%q) = %v, want one range field error", tc.value, err)
				}
			}
		})
	}
}

func TestCheckSettings_ClientLimitNotBelowLoginLimit(t *testing.T) {
	tests := []struct {
		name          string
		login, client string
		ok            bool
	}{
		{"equal", "50", "50", true},
		{"client larger", "10", "100", true},
		{"client smaller", "50", "40", false},
		{"unparsable login is the key's own check", "x", "100", true},
		{"unparsable client is the key's own check", "10", "x", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := identity.CheckSettings(map[string]string{"login_failure_limit": tc.login, "client_failure_limit": tc.client})
			if (err == nil) != tc.ok {
				t.Fatalf("CheckSettings = %v, want ok=%v", err, tc.ok)
			}
			if err != nil {
				if !errs.Is(err, errs.Invalid) {
					t.Errorf("kind = %v, want Invalid", errs.KindOf(err))
				}
				if fe := validate.Fields(err); len(fe) != 1 || fe[0].Field != "client_failure_limit" {
					t.Errorf("fields = %v, want client_failure_limit", fe)
				}
			}
		})
	}
}
