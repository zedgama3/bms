package identity_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/core/settings/settingstest"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// boundaries step 5: identity maps its "accounts.*" keys onto Policy itself, from
// Options.Settings, on every use case. The mapping is unexported, so these tests see it
// through the use cases that read the policy: SessionInfo.SignupOpen, SignUp's mode and
// SignUp's password-length rule. The other keys (lifetimes, limits, windows, cooldown) are
// exercised through the policySource shim by the reset, hardening and session tests.

// Moved from app: TestSettingsPolicy_Defaults_AreIdentityDefaultPolicy.
func TestSettingsPolicy_Defaults_AreIdentityDefaultPolicy(t *testing.T) {
	want := identity.DefaultPolicy()
	r := newRig(t, dbtest.NewFake(), withPolicy(settingstest.NewFake("accounts", identity.Settings)))
	ctx := from(ctxT(t), "198.51.100.7")

	info, err := r.svc.SessionInfo(ctx)
	wantNoErr(t, err)
	if info.SignupOpen != (want.SignupMode == identity.SignupOpen) {
		t.Errorf("SignupOpen = %v, want %v", info.SignupOpen, want.SignupMode == identity.SignupOpen)
	}
	short := strings.Repeat("q", want.PasswordMinLength-1)
	_, err = r.svc.SignUp(ctx, identity.Credentials{Email: mail(t, "ada.quill@example.org"), Password: short})
	wantField(t, err, "password", "too_short")
	if got := paramOf(err, "password", "too_short", "min"); got != strconv.Itoa(want.PasswordMinLength) {
		t.Errorf("min = %q, want %d", got, want.PasswordMinLength)
	}
}

// Moved from app: TestSettingsPolicy_WrongArea_ReturnsInvalid. A reader of another area
// declares no accounts key, so the use case fails Invalid and never falls back to defaults.
func TestSettingsPolicy_WrongArea_ReturnsInvalid(t *testing.T) {
	r := newRig(t, dbtest.NewFake(), withPolicy(settingstest.NewFake("people", people.Settings)))
	ctx := from(ctxT(t), "198.51.100.7")
	if _, err := r.svc.SessionInfo(ctx); !errs.Is(err, errs.Invalid) {
		t.Errorf("SessionInfo err = %v, want Invalid (the area declares no accounts key)", err)
	}
	_, err := r.svc.SignUp(ctx, identity.Credentials{Email: mail(t, "ada.quill@example.org"), Password: pwGood})
	wantKind(t, err, errs.Invalid)
}

// An unset key is settings' declared default, not identity.DefaultPolicy: the declarations
// here carry other defaults and identity must follow them.
func TestPolicy_UnsetKey_IsTheDeclaredDefault(t *testing.T) {
	decls := append([]settings.Decl(nil), identity.Settings...)
	for i := range decls {
		switch decls[i].Key {
		case "password_min_length":
			decls[i].Default = "20"
		case "signup":
			decls[i].Default = string(identity.SignupInvitation)
		}
	}
	r := newRig(t, dbtest.NewFake(), withPolicy(settingstest.NewFake("accounts", decls)))
	ctx := from(ctxT(t), "198.51.100.7")

	info, err := r.svc.SessionInfo(ctx)
	wantNoErr(t, err)
	if info.SignupOpen {
		t.Error("SignupOpen: the declared default signup is invitation")
	}
	_, err = r.svc.SignUp(ctx, identity.Credentials{Email: mail(t, "ada.quill@example.org"), Password: pwGood})
	wantKind(t, err, errs.Unsupported)

	// The declared password_min_length default (20) applies once sign-up is open.
	f := settingstest.NewFake("accounts", decls)
	f.Set("signup", "open")
	r = newRig(t, dbtest.NewFake(), withPolicy(f))
	_, err = r.svc.SignUp(ctx, identity.Credentials{Email: mail(t, "ada.quill@example.org"), Password: strings.Repeat("q", 19)})
	wantField(t, err, "password", "too_short")
	if got := paramOf(err, "password", "too_short", "min"); got != "20" {
		t.Errorf("min = %q, want 20 (the declared default)", got)
	}
}

// The reader is asked per use case, never cached (spec: read per use case).
func TestPolicy_ChangedSetting_AppliesToTheNextUseCase(t *testing.T) {
	f := settingstest.NewFake("accounts", identity.Settings)
	r := newRig(t, dbtest.NewFake(), withPolicy(f))
	ctx := from(ctxT(t), "198.51.100.7")
	info, err := r.svc.SessionInfo(ctx)
	wantNoErr(t, err)
	if !info.SignupOpen {
		t.Fatal("signup should start open")
	}
	f.Set("signup", "invitation")
	info, err = r.svc.SessionInfo(ctx)
	wantNoErr(t, err)
	if info.SignupOpen {
		t.Error("SignupOpen still true after the setting changed: the policy was cached")
	}
}
