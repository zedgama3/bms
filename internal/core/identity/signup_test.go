package identity_test

import (
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// paramOf returns the Params[key] of the field error for field/code, or "".
func paramOf(err error, field, code, key string) string {
	for _, fe := range validate.Fields(err) {
		if fe.Field == field && fe.Code == code {
			return fe.Params[key]
		}
	}
	return ""
}

// F9
func TestSignUp_InvitationMode_Unsupported(t *testing.T) {
	ps := newPolicySource()
	ps.set(func(p *identity.Policy) { p.SignupMode = identity.SignupInvitation })
	fake := dbtest.NewFake()
	r := newRig(t, fake, withPolicy(ps))
	_, err := r.svc.SignUp(from(ctxT(t), "198.51.100.7"), identity.Credentials{Email: mail(t, "ada.quill@example.org"), Password: pwGood})
	wantKind(t, err, errs.Unsupported)
	if n := len(fake.Calls()); n != 0 {
		t.Errorf("%d statements in invitation mode", n)
	}
	if len(r.rec.Recorded()) != 0 {
		t.Error("an entry was recorded for a refused sign-up")
	}
}

// F10
func TestSignUp_BadEmail_Invalid(t *testing.T) {
	for _, email := range []string{"", "no-at-sign", "two@@example.org", "a b@example.org", "<x>@example.org"} {
		t.Run(email, func(t *testing.T) {
			fake := dbtest.NewFake()
			r := newRig(t, fake)
			_, err := r.svc.SignUp(from(ctxT(t), "198.51.100.7"), identity.Credentials{Email: modelEmail(email), Password: pwGood})
			wantField(t, err, "email", "")
			if n := len(fake.Calls()); n != 0 {
				t.Errorf("%d statements for an invalid address", n)
			}
		})
	}
}

// F11, F12, F13
func TestPassword_Rules_Table(t *testing.T) {
	tests := []struct {
		name string
		pw   string
		code string
	}{
		{"empty", "", "required"},
		{"one short of the minimum", strings.Repeat("a", 11), "too_short"},
		{"minimum multibyte counts runes", strings.Repeat("é", 11), "too_short"},
		{"common, exact entry", "password1234", "common"},
		{"common, another entry", "qwertyuiopasdfghjkl", "common"},
		{"common, third entry", "1qaz2wsx3edc", "common"},
		{"too long by one byte", strings.Repeat("x7", 128) + "k", "too_long"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, dbtest.NewFake())
			_, err := r.svc.SignUp(from(ctxT(t), "198.51.100.7"), identity.Credentials{Email: mail(t, "ada.quill@example.org"), Password: tc.pw})
			wantField(t, err, "password", tc.code)
			switch tc.code {
			case "too_short":
				if got := paramOf(err, "password", "too_short", "min"); got != "12" {
					t.Errorf("min = %q, want 12", got)
				}
			case "too_long":
				if got := paramOf(err, "password", "too_long", "max"); got != "256" {
					t.Errorf("max = %q, want 256", got)
				}
			}
		})
	}
}

func TestPassword_MinLengthFollowsPolicy(t *testing.T) {
	ps := newPolicySource()
	ps.set(func(p *identity.Policy) { p.PasswordMinLength = 20 })
	r := newRig(t, dbtest.NewFake(), withPolicy(ps))
	_, err := r.svc.SignUp(from(ctxT(t), "198.51.100.7"), identity.Credentials{Email: mail(t, "ada.quill@example.org"), Password: strings.Repeat("q", 19)})
	wantField(t, err, "password", "too_short")
	if got := paramOf(err, "password", "too_short", "min"); got != "20" {
		t.Errorf("min = %q, want 20", got)
	}
}

func TestPassword_Boundaries_Accepted(t *testing.T) {
	for name, pw := range map[string]string{
		"exactly 12 runes of two bytes": strings.Repeat("é", 12),
		"exactly 256 bytes":             strings.Repeat("x7", 128),
		"spaces and symbols only":       "   !!! ??? ...   ",
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, dbtest.NewFake())
			_, err := r.svc.SignUp(from(ctxT(t), "198.51.100.7"), identity.Credentials{Email: mail(t, "ada.quill@example.org"), Password: pw})
			// Any outcome except a password field error proves the policy accepted it
			// (the fake store may or may not accept the insert).
			for _, fe := range validate.Fields(err) {
				if fe.Field == "password" {
					t.Fatalf("policy refused %q: %s", name, fe.Code)
				}
			}
		})
	}
}

// F14
func TestSignUp_RateLimitedByAddress_429(t *testing.T) {
	ps := newPolicySource()
	ps.set(func(p *identity.Policy) { p.LoginFailureLimit = 3 })
	r := newRig(t, dbtest.NewFake(), withPolicy(ps))
	ctx := from(ctxT(t), "203.0.113.50")
	for i := range 3 {
		_, err := r.svc.SignIn(ctx, identity.Credentials{Email: mail(t, "guess"+string(rune('a'+i))+"@example.org"), Password: pwOther})
		wantKind(t, err, errs.Unauthenticated)
	}
	_, err := r.svc.SignUp(ctx, identity.Credentials{Email: mail(t, "fresh.face@example.org"), Password: pwGood})
	wantRateLimited(t, err)
	if len(r.entries2(actCreated)) != 0 {
		t.Error("an account was recorded while rate limited")
	}
	// another client address is not limited
	_, err = r.svc.SignUp(from(ctxT(t), "203.0.113.51"), identity.Credentials{Email: mail(t, "fresh.face@example.org"), Password: pwGood})
	if errs.Is(err, errs.Unavailable) {
		t.Errorf("a different client address was limited: %v", err)
	}
}

// F16
func TestSignUp_ReadOnly_Unavailable(t *testing.T) {
	fake := dbtest.NewFake()
	fake.SetReadOnly(true)
	r := newRig(t, fake)
	_, err := r.svc.SignUp(from(ctxT(t), "198.51.100.7"), identity.Credentials{Email: mail(t, "ada.quill@example.org"), Password: pwGood})
	wantReadOnly(t, err)
	if len(r.rec.Recorded()) != 0 {
		t.Error("an entry was recorded on a read-only copy")
	}
}

// F62
func TestPolicy_SourceFailure_FailsUseCase(t *testing.T) {
	for _, tc := range []struct {
		name string
		mod  func(*policySource)
		kind errs.Kind
	}{
		{"source unavailable", func(s *policySource) { s.err = errDown }, errs.Unavailable},
		{"source timeout", func(s *policySource) { s.err = errTimeout }, errs.Timeout},
		{"invalid policy", func(s *policySource) { s.p.PasswordMinLength = 3 }, errs.Invalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ps := newPolicySource()
			tc.mod(ps)
			r := newRig(t, dbtest.NewFake(), withPolicy(ps))
			ctx := from(ctxT(t), "198.51.100.7")
			creds := identity.Credentials{Email: mail(t, "ada.quill@example.org"), Password: pwGood}
			_, err := r.svc.SignUp(ctx, creds)
			wantKind(t, err, tc.kind)
			_, err = r.svc.SignIn(ctx, creds)
			wantKind(t, err, tc.kind)
			_, err = r.svc.SessionInfo(ctx)
			wantKind(t, err, tc.kind)
		})
	}
}
