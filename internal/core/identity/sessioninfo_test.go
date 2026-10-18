package identity_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/lib/errs"
)

func signedIn(t *testing.T, superuser, restricted bool) (identity.Session, func() context.Context) {
	t.Helper()
	s := sessionFor(newAccountID(), newPersonID(), epoch)
	s.Superuser, s.PasswordChangeRequired = superuser, restricted
	return s, func() context.Context { return identity.WithSession(from(ctxT(t), "198.51.100.7"), s) }
}

func TestSessionInfo_SignedOut_OpenSignup(t *testing.T) {
	fake := dbtest.NewFake()
	r := newRig(t, fake)
	info, err := r.svc.SessionInfo(from(ctxT(t), "198.51.100.7"))
	wantNoErr(t, err)
	if info.SignedIn || !info.SignupOpen {
		t.Errorf("info = %+v, want signed out with signup open", info)
	}
	if info.Permissions == nil || len(info.Permissions) != 0 {
		t.Errorf("Permissions = %#v, want an empty non-nil list", info.Permissions)
	}
	if n := len(fake.Calls()); n != 0 {
		t.Errorf("%d statements for GET /session", n)
	}
	if len(r.rec.Recorded()) != 0 {
		t.Error("GET /session was audited")
	}
	body, err := json.Marshal(info)
	wantNoErr(t, err)
	if got, want := string(body), `{"signed_in":false,"superuser":false,"password_change_required":false,"signup_open":true,"permissions":[]}`; got != want {
		t.Errorf("wire shape\n got %s\nwant %s", got, want)
	}
}

func TestSessionInfo_SignedOut_InvitationMode_SignupClosed(t *testing.T) {
	ps := newPolicySource()
	ps.set(func(p *identity.Policy) { p.SignupMode = identity.SignupInvitation })
	r := newRig(t, dbtest.NewFake(), withPolicy(ps))
	info, err := r.svc.SessionInfo(from(ctxT(t), "198.51.100.7"))
	wantNoErr(t, err)
	if info.SignupOpen {
		t.Error("SignupOpen in invitation mode")
	}
}

func TestSessionInfo_SignedIn_FillsFromSeams(t *testing.T) {
	s, ctx := signedIn(t, true, false)
	r := newRig(t, dbtest.NewFake(), func(o *identity.Options) {
		o.Names = &nameSource{name: "Dusty Comet"}
		o.Lister = &lister{perms: []string{"accounts.read", "hello.greet"}}
	})
	info, err := r.svc.SessionInfo(ctx())
	wantNoErr(t, err)
	if !info.SignedIn || info.PersonID != s.Person || info.AccountID != s.Account || !info.Superuser || info.PasswordChangeRequired {
		t.Errorf("info = %+v", info)
	}
	if !info.ExpiresAt.Equal(s.ExpiresAt) {
		t.Errorf("ExpiresAt = %v, want %v", info.ExpiresAt, s.ExpiresAt)
	}
	if info.PlayaName != "Dusty Comet" || len(info.Permissions) != 2 {
		t.Errorf("seams not applied: %+v", info)
	}
}

func TestSessionInfo_NilSeams_M1Behaviour(t *testing.T) {
	_, ctx := signedIn(t, false, false)
	r := newRig(t, dbtest.NewFake())
	info, err := r.svc.SessionInfo(ctx())
	wantNoErr(t, err)
	if info.PlayaName != "" || info.Permissions == nil || len(info.Permissions) != 0 {
		t.Errorf("info = %+v", info)
	}
}

func TestSessionInfo_Restricted_ReportsFlag(t *testing.T) {
	_, ctx := signedIn(t, false, true)
	r := newRig(t, dbtest.NewFake())
	info, err := r.svc.SessionInfo(ctx())
	wantNoErr(t, err)
	if !info.SignedIn || !info.PasswordChangeRequired {
		t.Errorf("info = %+v", info)
	}
}

func TestSessionInfo_WorksOnReadOnlyCopy(t *testing.T) {
	fake := dbtest.NewFake()
	fake.SetReadOnly(true)
	_, ctx := signedIn(t, false, false)
	r := newRig(t, fake)
	if _, err := r.svc.SessionInfo(ctx()); err != nil {
		t.Fatalf("SessionInfo on a read-only copy: %v", err)
	}
}

// F8
func TestSessionInfo_SeamFailure_PassesKind(t *testing.T) {
	for _, tc := range []struct {
		name string
		mod  func(*identity.Options)
		kind errs.Kind
	}{
		{"names unavailable", func(o *identity.Options) { o.Names = &nameSource{err: errDown} }, errs.Unavailable},
		{"names timeout", func(o *identity.Options) { o.Names = &nameSource{err: errTimeout} }, errs.Timeout},
		{"lister unavailable", func(o *identity.Options) { o.Lister = &lister{err: errDown} }, errs.Unavailable},
		{"policy unavailable", func(o *identity.Options) {
			ps := newPolicySource()
			ps.err = errDown
			o.Policy = ps
		}, errs.Unavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, ctx := signedIn(t, false, false)
			r := newRig(t, dbtest.NewFake(), tc.mod)
			_, err := r.svc.SessionInfo(ctx())
			wantKind(t, err, tc.kind)
		})
	}
}

// F8
func TestSessionInfo_NameNotFound_EmptyName(t *testing.T) {
	_, ctx := signedIn(t, false, false)
	r := newRig(t, dbtest.NewFake(), func(o *identity.Options) {
		o.Names = &nameSource{err: errs.E("test.names", errs.NotFound, errDown)}
	})
	info, err := r.svc.SessionInfo(ctx())
	wantNoErr(t, err)
	if info.PlayaName != "" {
		t.Errorf("PlayaName = %q", info.PlayaName)
	}
}
