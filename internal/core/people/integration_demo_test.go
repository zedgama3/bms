//go:build integration

package people_test

// F89 (D53): identity refuses to delete a protected (shared demo) account with Permission
// field "account" code "protected"; people maps that to Conflict "protected", beside
// Conflict "superuser", and nothing is deleted. plan/design/people.md amendment 2026-10-28.

import (
	"bytes"
	"errors"
	"testing"

	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

func protectedRefusal() error {
	return errs.E("identity.DeletePerson", errs.Permission,
		&validate.FieldError{Field: "account", Code: "protected", Reason: "shared demo account"})
}

func TestDelete_ProtectedAccount_ReturnsConflict_RollsBack(t *testing.T) {
	f := newDelFixture(t, false)
	f.accts.deleteErr = protectedRefusal()
	_, err := f.svc.Delete(f.ctx, f.target)
	wantCode(t, err, errs.Conflict, "protected")
	if errs.Is(err, errs.Permission) {
		t.Error("the refusal leaks as Permission; people maps it to Conflict")
	}
	f.intact()
}

func TestFulfilRequest_ProtectedAccount_ReturnsConflict_RollsBack(t *testing.T) {
	f := newDelFixture(t, false)
	f.accts.deleteErr = protectedRefusal()
	reqs, err := f.svc.ListRequests(f.ctx, people.RequestQuery{Person: f.target})
	wantNoErr(t, err)
	if len(reqs) != 1 {
		t.Fatalf("%d requests, want 1", len(reqs))
	}
	_, err = f.svc.FulfilRequest(f.ctx, reqs[0].ID)
	wantCode(t, err, errs.Conflict, "protected")
	f.intact()
}

func TestDeleteCommand_ProtectedAccount_ReturnsConflict_RollsBack(t *testing.T) {
	f := newDelFixture(t, false)
	f.accts.deleteErr = protectedRefusal()
	err := f.svc.DeleteCommand(ctxT(t), &bytes.Buffer{}, f.target.String())
	wantCode(t, err, errs.Conflict, "protected")
	f.intact()
}

// A Permission from identity that is not the protected code keeps its kind (the superuser
// case, F35).
func TestDelete_PermissionWithoutProtectedCode_StaysPermission(t *testing.T) {
	f := newDelFixture(t, false)
	f.accts.deleteErr = errs.E("identity.DeletePerson", errs.Permission, errors.New("a superuser's account"))
	_, err := f.svc.Delete(f.ctx, f.target)
	wantKind(t, err, errs.Permission)
	f.intact()
}

// D53: identity's protected refusal on LinkAccount comes back as Conflict "protected", and no
// link is recorded.
func TestLinkAccount_ProtectedAccount_ReturnsConflict(t *testing.T) {
	r := newIRig(t, false)
	r.link()
	_, ctx := r.admin(permManage)
	target := r.create(ctx, "Walk On").Person.ID
	r.accts.linkErr = errs.E("identity.LinkAccount", errs.Permission,
		&validate.FieldError{Field: "account", Code: "protected", Reason: "shared demo account"})
	_, err := r.svc.LinkAccount(ctx, target, model.Email("participant@demo.invalid"))
	wantCode(t, err, errs.Conflict, "protected")
	if errs.Is(err, errs.Permission) {
		t.Error("the refusal leaks as Permission; people maps it to Conflict")
	}
	if r.count("people.person.linked") != 0 {
		t.Error("a refused link was recorded")
	}
}
