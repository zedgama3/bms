package identity_test

import (
	"context"
	"testing"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

type adminOp struct {
	name string
	perm authz.Permission
	run  func(svc *identity.Service, ctx context.Context, id model.AccountID) error //nolint:revive // ctx second: the service is the subject
}

var adminOps = []adminOp{
	{"ListAccounts", identity.PermissionAccountsRead, func(s *identity.Service, ctx context.Context, _ model.AccountID) error {
		_, err := s.ListAccounts(ctx, identity.AccountQuery{})
		return keep(err)
	}},
	{"GetAccount", identity.PermissionAccountsRead, func(s *identity.Service, ctx context.Context, id model.AccountID) error {
		_, err := s.GetAccount(ctx, id)
		return keep(err)
	}},
	{"DisableAccount", identity.PermissionAccountsManage, func(s *identity.Service, ctx context.Context, id model.AccountID) error {
		return s.DisableAccount(ctx, id)
	}},
	{"EnableAccount", identity.PermissionAccountsManage, func(s *identity.Service, ctx context.Context, id model.AccountID) error {
		return s.EnableAccount(ctx, id)
	}},
	{"SetTemporaryPassword", identity.PermissionAccountsManage, func(s *identity.Service, ctx context.Context, id model.AccountID) error {
		_, err := s.SetTemporaryPassword(ctx, id)
		return keep(err)
	}},
}

func participantCtx(t *testing.T) context.Context {
	t.Helper()
	return identity.WithSession(from(ctxT(t), "198.51.100.7"), sessionFor(newAccountID(), newPersonID(), epoch))
}

func superuserCtx(t *testing.T) context.Context {
	t.Helper()
	s := sessionFor(newAccountID(), newPersonID(), epoch)
	s.Superuser = true
	return identity.WithSession(from(ctxT(t), "198.51.100.7"), s)
}

// F31, F37
func TestAdminOps_NoSession_Unauthenticated(t *testing.T) {
	for _, op := range adminOps {
		t.Run(op.name, func(t *testing.T) {
			r := newRig(t, dbtest.NewFake(), withPerms(allowing(identity.PermissionAccountsRead, identity.PermissionAccountsManage)))
			wantKind(t, op.run(r.svc, from(ctxT(t), "198.51.100.7"), newAccountID()), errs.Unauthenticated)
		})
	}
}

// F31, F37: a restricted session is not a full session.
func TestAdminOps_RestrictedSession_Unauthenticated(t *testing.T) {
	for _, op := range adminOps {
		t.Run(op.name, func(t *testing.T) {
			s := sessionFor(newAccountID(), newPersonID(), epoch)
			s.PasswordChangeRequired = true
			r := newRig(t, dbtest.NewFake(), withPerms(allowing(identity.PermissionAccountsRead, identity.PermissionAccountsManage)))
			wantKind(t, op.run(r.svc, identity.WithSession(from(ctxT(t), "198.51.100.7"), s), newAccountID()), errs.Unauthenticated)
		})
	}
}

// F32, F38: nil checker denies everyone but the superuser; the answer is Permission before
// any ID is looked up, so an unknown ID does not answer NotFound.
func TestAdminOps_Denied_Permission(t *testing.T) {
	for _, op := range adminOps {
		t.Run(op.name+"/nil checker", func(t *testing.T) {
			fake := dbtest.NewFake()
			r := newRig(t, fake)
			wantKind(t, op.run(r.svc, participantCtx(t), newAccountID()), errs.Permission)
			if n := len(fake.Calls()); n != 0 {
				t.Errorf("%d statements before the permission check", n)
			}
			if len(r.rec.Recorded()) != 0 {
				t.Error("a refused call was recorded as a read or change")
			}
		})
		t.Run(op.name+"/denying checker", func(t *testing.T) {
			r := newRig(t, dbtest.NewFake(), withPerms(allowing()))
			wantKind(t, op.run(r.svc, participantCtx(t), newAccountID()), errs.Permission)
		})
		t.Run(op.name+"/the other permission is not enough", func(t *testing.T) {
			other := identity.PermissionAccountsManage
			if op.perm == identity.PermissionAccountsManage {
				other = identity.PermissionAccountsRead
			}
			r := newRig(t, dbtest.NewFake(), withPerms(allowing(other)))
			wantKind(t, op.run(r.svc, participantCtx(t), newAccountID()), errs.Permission)
		})
	}
}

// The person is not special on the admin routes: a denying checker is honoured even for
// accounts.read on one's own account.
func TestGetAccount_OwnAccount_StillNeedsPermission(t *testing.T) {
	r := newRig(t, dbtest.NewFake(), withPerms(allowing()))
	s := sessionFor(newAccountID(), newPersonID(), epoch)
	err := adminOps[1].run(r.svc, identity.WithSession(from(ctxT(t), "198.51.100.7"), s), s.Account)
	wantKind(t, err, errs.Permission)
}

func TestAdminOps_CheckerAskedForTheRightPermission(t *testing.T) {
	for _, op := range adminOps {
		t.Run(op.name, func(t *testing.T) {
			pc := &recordingChecker{}
			r := newRig(t, dbtest.NewFake(), withPerms(pc))
			_ = op.run(r.svc, participantCtx(t), newAccountID()) //nolint:errcheck // the outcome is not under test, the permission asked is
			if len(pc.asked) != 1 || pc.asked[0] != op.perm {
				t.Errorf("asked %v, want [%s]", pc.asked, op.perm)
			}
		})
	}
}

type recordingChecker struct{ asked []authz.Permission }

func (c *recordingChecker) Allow(_ context.Context, p authz.Permission) error {
	c.asked = append(c.asked, p)
	return errs.E("test.recordingChecker", errs.Permission, errDown)
}

// F32: the superuser passes without a grant and the seam is not asked.
func TestAdminOps_Superuser_AllowedWithoutAskingTheChecker(t *testing.T) {
	for _, op := range adminOps {
		t.Run(op.name, func(t *testing.T) {
			pc := allowing()
			r := newRig(t, dbtest.NewFake(), withPerms(pc))
			err := op.run(r.svc, superuserCtx(t), newAccountID())
			if errs.Is(err, errs.Permission) || errs.Is(err, errs.Unauthenticated) {
				t.Fatalf("superuser refused: %v", err)
			}
			if pc.askedCount() != 0 {
				t.Error("the checker was asked for the superuser")
			}
		})
	}
}

func TestListAccounts_Superuser_Allowed(t *testing.T) {
	r := newRig(t, dbtest.NewFake()) // nil checker
	page, err := r.svc.ListAccounts(superuserCtx(t), identity.AccountQuery{})
	wantNoErr(t, err)
	if len(page.Accounts) != 0 || page.Next != "" {
		t.Errorf("page = %+v", page)
	}
}

// F33
func TestListAccounts_BadQuery_Invalid(t *testing.T) {
	for _, tc := range []struct {
		name  string
		q     identity.AccountQuery
		field string
	}{
		{"limit too big", identity.AccountQuery{Limit: identity.MaxPageSize + 1}, "limit"},
		{"limit negative", identity.AccountQuery{Limit: -5}, "limit"},
		{"bad cursor", identity.AccountQuery{After: "###"}, "after"},
		{"bad email", identity.AccountQuery{Email: "nope"}, "email"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, dbtest.NewFake())
			_, err := r.svc.ListAccounts(superuserCtx(t), tc.q)
			wantField(t, err, tc.field, "")
		})
	}
}

// F34
func TestListAccounts_StoreUnavailable_PassesKind(t *testing.T) {
	for _, tc := range []struct {
		err  error
		kind errs.Kind
	}{{errDown, errs.Unavailable}, {errTimeout, errs.Timeout}} {
		fdb := newFaultDB(dbtest.NewFake())
		fdb.set(func(f *faultDB) { f.readErr = tc.err })
		r := newRig(t, fdb)
		_, err := r.svc.ListAccounts(superuserCtx(t), identity.AccountQuery{})
		wantKind(t, err, tc.kind)
		if len(r.rec.Recorded()) != 0 {
			t.Error("a read that failed was audited")
		}
	}
}

// F36
func TestGetAccount_Unknown_NotFound(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	_, err := r.svc.GetAccount(superuserCtx(t), newAccountID())
	wantKind(t, err, errs.NotFound)
}

// F41
func TestDisableAccount_Unknown_NotFound(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	wantKind(t, r.svc.DisableAccount(superuserCtx(t), newAccountID()), errs.NotFound)
	wantKind(t, r.svc.EnableAccount(superuserCtx(t), newAccountID()), errs.NotFound)
	_, err := r.svc.SetTemporaryPassword(superuserCtx(t), newAccountID())
	wantKind(t, err, errs.NotFound)
	if len(r.rec.Recorded()) != 0 {
		t.Error("an entry was recorded for an unknown account")
	}
}

// F47
func TestExportPerson_ZeroID_Invalid(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	_, err := r.svc.ExportPerson(ctxT(t), model.PersonID{})
	wantKind(t, err, errs.Invalid)
	_, err = r.svc.DeletePerson(ctxT(t), model.PersonID{})
	wantKind(t, err, errs.Invalid)
}

// F48
func TestDeletePerson_ReadOnly_Unavailable(t *testing.T) {
	fake := dbtest.NewFake()
	fake.SetReadOnly(true)
	r := newRig(t, fake)
	_, err := r.svc.DeletePerson(ctxT(t), newPersonID())
	wantReadOnly(t, err)
}

// F44
func TestEnsureSuperuser_BadEmail_Invalid(t *testing.T) {
	for _, addr := range []string{"", "no-at-sign", "a b@example.org"} {
		fake := dbtest.NewFake()
		r := newRig(t, fake)
		_, err := r.svc.EnsureSuperuser(ctxT(t), modelEmail(addr))
		wantField(t, err, "email", "")
		if len(fake.Calls()) != 0 || len(r.rec.Recorded()) != 0 {
			t.Error("an invalid address reached the store or the audit log")
		}
	}
}

// F46
func TestEnsureSuperuser_ReadOnly_Unavailable(t *testing.T) {
	fake := dbtest.NewFake()
	fake.SetReadOnly(true)
	r := newRig(t, fake)
	_, err := r.svc.EnsureSuperuser(ctxT(t), mail(t, "root.operator@example.org"))
	wantReadOnly(t, err)
}
