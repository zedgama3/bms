//go:build integration

package people_test

// F89 (D53): identity refuses to delete a protected (shared demo) account with Permission
// field "account" code "protected"; people maps that to Conflict "protected", beside
// Conflict "superuser", and nothing is deleted. plan/design/people.md amendment 2026-10-28.

import (
	"testing"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

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
