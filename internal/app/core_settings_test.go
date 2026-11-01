package app

import (
	"context"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/core/settings/settingstest"
	"github.com/zedgama3/bms/internal/lib/errs"
)

func TestSettingsPolicy_Defaults_AreIdentityDefaultPolicy(t *testing.T) {
	r := settingstest.NewFake("accounts", identity.Settings)
	got, err := settingsPolicy{r: r}.Policy(context.Background())
	if err != nil {
		t.Fatalf("Policy: %v", err)
	}
	if got != identity.DefaultPolicy() {
		t.Errorf("Policy = %+v, want %+v", got, identity.DefaultPolicy())
	}
	if err := got.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

func TestSettingsPolicy_ReadsEveryKey(t *testing.T) {
	r := settingstest.NewFake("accounts", identity.Settings)
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
	got, err := settingsPolicy{r: r}.Policy(context.Background())
	if err != nil {
		t.Fatalf("Policy: %v", err)
	}
	want := identity.Policy{
		SignupMode: identity.SignupInvitation, SessionLifetime: 48 * time.Hour,
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

func TestSettingsPolicy_WrongArea_ReturnsInvalid(t *testing.T) {
	r := settingstest.NewFake("people", people.Settings)
	if _, err := (settingsPolicy{r: r}).Policy(context.Background()); !errs.Is(err, errs.Invalid) {
		t.Errorf("err = %v, want Invalid (the area declares no accounts key)", err)
	}
}

func TestPeoplePolicy_Defaults_AreDefaultPolicy(t *testing.T) {
	r := settingstest.NewFake("people", people.Settings)
	got, err := peoplePolicy{r: r}.Policy(context.Background())
	if err != nil {
		t.Fatalf("Policy: %v", err)
	}
	if got != people.DefaultPolicy() {
		t.Errorf("Policy = %+v, want %+v", got, people.DefaultPolicy())
	}
}

func TestPeoplePolicy_ReadsEveryKey(t *testing.T) {
	r := settingstest.NewFake("people", people.Settings)
	r.Set("legal_name", "required")
	r.Set("phone", "hidden")
	r.Set("emergency_contacts", "hidden")
	got, err := peoplePolicy{r: r}.Policy(context.Background())
	if err != nil {
		t.Fatalf("Policy: %v", err)
	}
	want := people.Policy{LegalName: people.FieldRequired, Phone: people.FieldHidden, EmergencyContacts: people.FieldHidden}
	if got != want {
		t.Errorf("Policy = %+v, want %+v", got, want)
	}
}

// Until the settings part is built the lazy sources answer the shipped defaults.
func TestLazyPolicies_BeforeSettings_AreTheDefaults(t *testing.T) {
	a := &assembly{}
	ip, err := lazyIdentityPolicy{a: a}.Policy(context.Background())
	if err != nil || ip != identity.DefaultPolicy() {
		t.Errorf("identity policy = (%+v, %v), want the defaults", ip, err)
	}
	pp, err := lazyPeoplePolicy{a: a}.Policy(context.Background())
	if err != nil || pp != people.DefaultPolicy() {
		t.Errorf("people policy = (%+v, %v), want the defaults", pp, err)
	}
}

// The real service, built over a database that refuses every statement, is what the lazy
// sources resolve once the settings part has run; their first read fails with the load's
// kind, not with a wiring error.
func TestLazyPolicies_AfterSettings_ReadThroughTheService(t *testing.T) {
	a := builtCore(t)
	_, err := lazyIdentityPolicy{a: a}.Policy(context.Background())
	if err == nil || errs.Is(err, errs.Internal) {
		t.Errorf("identity policy over db.None(): err = %v, want the load's store kind", err)
	}
	_, err = lazyPeoplePolicy{a: a}.Policy(context.Background())
	if err == nil || errs.Is(err, errs.Internal) {
		t.Errorf("people policy over db.None(): err = %v, want the load's store kind", err)
	}
}

func TestRefresher_StartThenStop_Returns(t *testing.T) {
	a := builtCore(t)
	var rn *runnable
	for i := range a.runnables {
		if a.runnables[i].name == "settings" {
			rn = &a.runnables[i]
		}
	}
	if rn == nil {
		t.Fatal("the settings part registered no runnable")
	}
	if err := rn.start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rn.stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}
}

func TestRefresher_StopBeforeStart_IsNil(t *testing.T) {
	if err := (&refresher{}).stop(context.Background()); err != nil {
		t.Errorf("stop before start: %v", err)
	}
}

// Run is still running when the stop deadline passes: stop reports Timeout and does not hang.
func TestRefresher_StopWithExpiredContext_ReturnsTimeout(t *testing.T) {
	r := &refresher{cancel: func() {}, done: make(chan struct{})} // a Run that never finishes
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if err := r.stop(ctx); !errs.Is(err, errs.Timeout) {
		t.Errorf("err = %v, want Timeout", err)
	}
}
