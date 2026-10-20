package identity_test

import (
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/identity/postgres"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/model"
)

// F1
func TestNew_Table(t *testing.T) {
	tests := []struct {
		name  string
		mod   func(*identity.Options)
		field string
	}{
		{"nil db", func(o *identity.Options) { o.DB = nil }, "identity.db"},
		{"nil clock", func(o *identity.Options) { o.Clock = nil }, "identity.clock"},
		{"nil logger", func(o *identity.Options) { o.Logger = nil }, "identity.logger"},
		{"nil tracer", func(o *identity.Options) { o.Tracer = nil }, "identity.tracer"},
		{"nil audit", func(o *identity.Options) { o.Audit = nil }, "identity.audit"},
		{"nil signals", func(o *identity.Options) { o.Signals = nil }, "identity.signals"},
		{"unset key", func(o *identity.Options) { o.Key = secret.Secret{} }, "session.key"},
		{"empty key", func(o *identity.Options) { o.Key = secret.New("") }, "session.key"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o, _ := validOptions(t, dbtest.NewFake())
			tc.mod(&o)
			svc, err := identity.New(o)
			wantField(t, err, tc.field, "")
			if svc != nil {
				t.Error("New returned a service with an error")
			}
			wantField(t, o.Validate(), tc.field, "")
		})
	}
}

func TestNew_ZeroOptions_ReportsEveryProblemAtOnce(t *testing.T) {
	err := identity.Options{}.Validate()
	for _, f := range []string{"identity.db", "identity.clock", "identity.logger", "identity.tracer", "identity.audit", "identity.signals", "session.key"} {
		wantField(t, err, f, "")
	}
}

func TestNew_OptionalSeamsNil_Succeeds(t *testing.T) {
	o, _ := validOptions(t, dbtest.NewFake())
	o.Meter = nil
	o.Policy, o.Permissions, o.Lister, o.Names, o.Alerter = nil, nil, nil, nil, nil
	if _, err := identity.New(o); err != nil {
		t.Fatalf("New: %v", err)
	}
}

func TestNew_DoesNoIO(t *testing.T) {
	fake := dbtest.NewFake()
	newRig(t, fake)
	if n := len(fake.Calls()); n != 0 {
		t.Fatalf("New ran %d statements", n)
	}
}

func TestNew_WorksAgainstNoDatabase(t *testing.T) {
	newRig(t, db.None())
}

func TestNew_WorksOnReadOnlyCopy(t *testing.T) {
	fake := dbtest.NewFake()
	fake.SetReadOnly(true)
	newRig(t, fake)
}

func TestConstants_PinnedBySpec(t *testing.T) {
	if identity.CookieName != "__Host-bms_session" || identity.DevCookieName != "bms_session_dev" {
		t.Errorf("cookie names = %q, %q", identity.CookieName, identity.DevCookieName)
	}
	checks := map[string][2]int{
		"TokenVersion":            {identity.TokenVersion, 1},
		"MaxPasswordBytes":        {identity.MaxPasswordBytes, 256},
		"PasswordMinLengthFloor":  {identity.PasswordMinLengthFloor, 12},
		"GeneratedPasswordLength": {identity.GeneratedPasswordLength, 24},
		"HashConcurrency":         {identity.HashConcurrency, 4},
		"MaxTrackedKeys":          {identity.MaxTrackedKeys, 10000},
		"MaxPageSize":             {identity.MaxPageSize, 100},
		"DefaultPageSize":         {identity.DefaultPageSize, 50},
	}
	for name, c := range checks {
		if c[0] != c[1] {
			t.Errorf("%s = %d, want %d", name, c[0], c[1])
		}
	}
	if identity.ClockSkew != 5*time.Minute {
		t.Errorf("ClockSkew = %v", identity.ClockSkew)
	}
}

func TestDeclarations_PermissionsSignalSet(t *testing.T) {
	if identity.PermissionAccountsRead != "accounts.read" || identity.PermissionAccountsManage != "accounts.manage" {
		t.Errorf("permission names = %q, %q", identity.PermissionAccountsRead, identity.PermissionAccountsManage)
	}
	if identity.SignalAccountCreated != "identity.account-created" || identity.PayloadAccount != "account" || identity.PayloadPerson != "person" {
		t.Errorf("signal = %q payload keys %q %q", identity.SignalAccountCreated, identity.PayloadAccount, identity.PayloadPerson)
	}
	if len(identity.Permissions) != 2 {
		t.Fatalf("Permissions = %v", identity.Permissions)
	}
	for _, d := range identity.Permissions {
		if err := d.Validate(); err != nil {
			t.Errorf("%s: %v", d.Name, err)
		}
		if d.Scope != authz.ScopeSite || d.Reaches != model.SensitivityPersonal {
			t.Errorf("%s: scope %v reaches %v, want site and personal", d.Name, d.Scope, d.Reaches)
		}
	}
	if identity.Set.Name != "identity" || identity.Set.Schema != "core" {
		t.Errorf("Set = %+v", identity.Set)
	}
	if v, err := db.Version(postgres.Migrations); err != nil || v < 1 {
		t.Errorf("Version = %d, %v", v, err)
	}
}

// ---- Policy (F56) ----

func TestDefaultPolicy_Values(t *testing.T) {
	p := identity.DefaultPolicy()
	want := identity.Policy{
		SignupMode:               identity.SignupOpen,
		SessionLifetime:          720 * time.Hour,
		SuperuserSessionLifetime: time.Hour,
		PasswordMinLength:        12,
		LoginFailureLimit:        10,
		ClientFailureLimit:       100,
		LoginFailureWindow:       15 * time.Minute,
		PasswordResetLifetime:    time.Hour,
	}
	if p != want {
		t.Errorf("DefaultPolicy = %+v, want %+v", p, want)
	}
	if err := p.Validate(); err != nil {
		t.Errorf("default policy invalid: %v", err)
	}
}

// F56
func TestPolicy_Validate_Table(t *testing.T) {
	tests := []struct {
		name  string
		mod   func(*identity.Policy)
		field string // "" = valid
	}{
		{"defaults", func(*identity.Policy) {}, ""},
		{"invitation mode", func(p *identity.Policy) { p.SignupMode = identity.SignupInvitation }, ""},
		{"unknown mode", func(p *identity.Policy) { p.SignupMode = "closed" }, "accounts.signup"},
		{"empty mode", func(p *identity.Policy) { p.SignupMode = "" }, "accounts.signup"},
		{"session 1h", func(p *identity.Policy) { p.SessionLifetime = time.Hour }, ""},
		{"session below 1h", func(p *identity.Policy) { p.SessionLifetime = time.Hour - time.Second }, "accounts.session_lifetime"},
		{"session 8760h", func(p *identity.Policy) { p.SessionLifetime = 8760 * time.Hour }, ""},
		{"session above 8760h", func(p *identity.Policy) { p.SessionLifetime = 8760*time.Hour + time.Second }, "accounts.session_lifetime"},
		{"session zero", func(p *identity.Policy) { p.SessionLifetime = 0 }, "accounts.session_lifetime"},
		{"superuser 10m", func(p *identity.Policy) { p.SuperuserSessionLifetime = 10 * time.Minute }, ""},
		{"superuser below 10m", func(p *identity.Policy) { p.SuperuserSessionLifetime = 9 * time.Minute }, "accounts.superuser_session_lifetime"},
		{"superuser 24h", func(p *identity.Policy) { p.SuperuserSessionLifetime = 24 * time.Hour }, ""},
		{"superuser above 24h", func(p *identity.Policy) { p.SuperuserSessionLifetime = 25 * time.Hour }, "accounts.superuser_session_lifetime"},
		{"min length 12", func(p *identity.Policy) { p.PasswordMinLength = 12 }, ""},
		{"min length below the floor", func(p *identity.Policy) { p.PasswordMinLength = 11 }, "accounts.password_min_length"},
		{"min length 128", func(p *identity.Policy) { p.PasswordMinLength = 128 }, ""},
		{"min length above 128", func(p *identity.Policy) { p.PasswordMinLength = 129 }, "accounts.password_min_length"},
		{"limit 3", func(p *identity.Policy) { p.LoginFailureLimit = 3 }, ""},
		{"limit below 3", func(p *identity.Policy) { p.LoginFailureLimit = 2 }, "accounts.login_failure_limit"},
		{"limit 100", func(p *identity.Policy) { p.LoginFailureLimit = 100 }, ""},
		{"limit above 100", func(p *identity.Policy) { p.LoginFailureLimit = 101 }, "accounts.login_failure_limit"},
		{"client limit 10", func(p *identity.Policy) { p.ClientFailureLimit = 10 }, ""},
		{"client limit below 10", func(p *identity.Policy) { p.ClientFailureLimit = 9 }, "accounts.client_failure_limit"},
		{"client limit 1000", func(p *identity.Policy) { p.ClientFailureLimit = 1000 }, ""},
		{"client limit above 1000", func(p *identity.Policy) { p.ClientFailureLimit = 1001 }, "accounts.client_failure_limit"},
		{"client limit zero", func(p *identity.Policy) { p.ClientFailureLimit = 0 }, "accounts.client_failure_limit"},
		{"client limit negative", func(p *identity.Policy) { p.ClientFailureLimit = -1 }, "accounts.client_failure_limit"},
		{"client limit equals account limit", func(p *identity.Policy) { p.LoginFailureLimit, p.ClientFailureLimit = 50, 50 }, ""},
		{"client limit one below account limit", func(p *identity.Policy) { p.LoginFailureLimit, p.ClientFailureLimit = 50, 49 }, "accounts.client_failure_limit"},
		{"client limit far below account limit", func(p *identity.Policy) { p.LoginFailureLimit, p.ClientFailureLimit = 100, 10 }, "accounts.client_failure_limit"},
		{"window 1m", func(p *identity.Policy) { p.LoginFailureWindow = time.Minute }, ""},
		{"window below 1m", func(p *identity.Policy) { p.LoginFailureWindow = 59 * time.Second }, "accounts.login_failure_window"},
		{"window 24h", func(p *identity.Policy) { p.LoginFailureWindow = 24 * time.Hour }, ""},
		{"window above 24h", func(p *identity.Policy) { p.LoginFailureWindow = 25 * time.Hour }, "accounts.login_failure_window"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := identity.DefaultPolicy()
			tc.mod(&p)
			err := p.Validate()
			if tc.field == "" {
				wantNoErr(t, err)
				return
			}
			wantField(t, err, tc.field, "")
		})
	}
}

func TestPolicy_Validate_ReportsEveryKey(t *testing.T) {
	err := identity.Policy{}.Validate()
	for _, f := range []string{"accounts.signup", "accounts.session_lifetime", "accounts.superuser_session_lifetime",
		"accounts.password_min_length", "accounts.login_failure_limit", "accounts.client_failure_limit", "accounts.login_failure_window"} {
		wantField(t, err, f, "")
	}
}

// ---- values ----

func TestSession_Validate_Table(t *testing.T) {
	good := sessionFor(model.NewID[model.AccountID](), model.NewID[model.PersonID](), epoch)
	tests := []struct {
		name string
		mod  func(*identity.Session)
		ok   bool
	}{
		{"good", func(*identity.Session) {}, true},
		{"zero id", func(s *identity.Session) { s.ID = identity.SessionID{} }, false},
		{"zero account", func(s *identity.Session) { s.Account = model.AccountID{} }, false},
		{"zero person", func(s *identity.Session) { s.Person = model.PersonID{} }, false},
		{"expires equals issued", func(s *identity.Session) { s.ExpiresAt = s.IssuedAt }, false},
		{"expires before issued", func(s *identity.Session) { s.ExpiresAt = s.IssuedAt.Add(-time.Second) }, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := good
			tc.mod(&s)
			err := s.Validate()
			if tc.ok {
				wantNoErr(t, err)
				return
			}
			wantKind(t, err, 1) // errs.Invalid
		})
	}
}

func goodAccount() identity.Account {
	return identity.Account{
		ID:                model.NewID[model.AccountID](),
		Person:            model.NewID[model.PersonID](),
		Email:             model.Email("river.stone@example.org"),
		PasswordChangedAt: epoch,
		CreatedAt:         epoch,
		UpdatedAt:         epoch,
	}
}

func TestAccount_Validate_Table(t *testing.T) {
	tests := []struct {
		name  string
		mod   func(*identity.Account)
		field string
	}{
		{"good", func(*identity.Account) {}, ""},
		{"zero id", func(a *identity.Account) { a.ID = model.AccountID{} }, "id"},
		{"zero person", func(a *identity.Account) { a.Person = model.PersonID{} }, "person"},
		{"bad email", func(a *identity.Account) { a.Email = "not-an-address" }, "email"},
		{"empty email", func(a *identity.Account) { a.Email = "" }, "email"},
		{"zero created", func(a *identity.Account) { a.CreatedAt = time.Time{} }, "created_at"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := goodAccount()
			tc.mod(&a)
			err := a.Validate()
			if tc.field == "" {
				wantNoErr(t, err)
				return
			}
			wantField(t, err, tc.field, "")
		})
	}
}

func TestAccount_LogValue_IDsOnlyNeverEmail(t *testing.T) {
	a := goodAccount()
	a.Superuser = true
	got := a.LogValue().String()
	if strings.Contains(got, string(a.Email)) || strings.Contains(got, "example.org") {
		t.Errorf("LogValue carries the address: %s", got)
	}
	if !strings.Contains(got, a.ID.String()) {
		t.Errorf("LogValue lacks the account ID: %s", got)
	}
}

func TestAccountQuery_Validate_Table(t *testing.T) {
	tests := []struct {
		name  string
		q     identity.AccountQuery
		field string
	}{
		{"zero", identity.AccountQuery{}, ""},
		{"limit max", identity.AccountQuery{Limit: identity.MaxPageSize}, ""},
		{"limit above max", identity.AccountQuery{Limit: identity.MaxPageSize + 1}, "limit"},
		{"limit negative", identity.AccountQuery{Limit: -1}, "limit"},
		{"email ok", identity.AccountQuery{Email: "river.stone@example.org"}, ""},
		{"email malformed", identity.AccountQuery{Email: "river.stone"}, "email"},
		{"cursor garbage", identity.AccountQuery{After: identity.Cursor("!!not a cursor!!")}, "after"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.q.Validate()
			if tc.field == "" {
				wantNoErr(t, err)
				return
			}
			wantField(t, err, tc.field, "")
		})
	}
}

var _ clock.Clock = (*clock.Fake)(nil)
