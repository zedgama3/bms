package people_test

import (
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/contracts/membership"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/core/people/peopletest"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// D44 amendment: Directory.LegalNamesOf (F67, F68) and the membership contract's name.
// The fake DB has no rows; the cases here fail before any row is read.

var _ people.Directory = (*people.Service)(nil)
var _ people.Directory = (*peopletest.Fake)(nil)

func TestMembershipPoint_IsPeopleMembership(t *testing.T) {
	if membership.Point != "people.membership" {
		t.Fatalf("Point = %q", membership.Point)
	}
}

// F67
func TestLegalNamesOf_NoContactsRead_ReturnsPermission(t *testing.T) {
	tests := []struct {
		name  string
		grant []authz.Permission
	}{
		{"holds nothing", nil},
		{"directory read only", []authz.Permission{permDirectory}},
		{"profiles manage, export and delete but not contacts", []authz.Permission{permManage, permExport, permDelete}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u := newUnit(t)
			me := newPerson()
			for _, g := range tc.grant {
				u.az.Grant(me, g)
			}
			got, err := u.svc.LegalNamesOf(asPerson(ctxT(t), me), []model.PersonID{newPerson()})
			wantKind(t, err, errs.Permission)
			if len(got) != 0 {
				t.Errorf("names returned with the error: %v", got)
			}
			if n := len(u.rec.Recorded()); n != 0 {
				t.Errorf("%d audit entries for a refused read", n)
			}
		})
	}
}

// F67: the permission is checked before the input, so a refused caller learns nothing
// about the batch limit either.
func TestLegalNamesOf_NoContactsRead_WithTooManyIDs_ReturnsPermission(t *testing.T) {
	u := newUnit(t)
	me := newPerson()
	u.az.Grant(me, permDirectory)
	ids := make([]model.PersonID, people.MaxNames+1)
	for i := range ids {
		ids[i] = newPerson()
	}
	_, err := u.svc.LegalNamesOf(asPerson(ctxT(t), me), ids)
	wantKind(t, err, errs.Permission)
}

// F68
func TestLegalNamesOf_TooManyIDs_ReturnsInvalid(t *testing.T) {
	u := newUnit(t)
	me := newPerson()
	u.az.Grant(me, permContacts)
	ids := make([]model.PersonID, people.MaxNames+1)
	for i := range ids {
		ids[i] = newPerson()
	}
	_, err := u.svc.LegalNamesOf(asPerson(ctxT(t), me), ids)
	wantKind(t, err, errs.Invalid)
	if n := len(u.rec.Recorded()); n != 0 {
		t.Errorf("%d audit entries for a rejected batch", n)
	}
}

func TestLegalNamesOf_NoSession_ReturnsUnauthenticated(t *testing.T) {
	u := newUnit(t)
	_, err := u.svc.LegalNamesOf(ctxT(t), []model.PersonID{newPerson()})
	wantKind(t, err, errs.Unauthenticated)
}

// A signal handler or job runs as the system: LegalNamesOf is for a person with a
// permission, not a system path.
func TestLegalNamesOf_InSignalDelivery_ReturnsUnauthenticated(t *testing.T) {
	u := newUnit(t)
	_, err := u.svc.LegalNamesOf(inDelivery(t), []model.PersonID{newPerson()})
	wantKind(t, err, errs.Unauthenticated)
}

// "Never on a route": no handler serves legal names, and the API document does not mention
// the method.
func TestLegalNamesOf_IsNeverOnARoute(t *testing.T) {
	u := newUnit(t)
	for _, rt := range u.svc.Routes() {
		if strings.Contains(strings.ToLower(rt.Pattern), "legal") {
			t.Errorf("route %s %s exposes legal names", rt.Method, rt.Pattern)
		}
	}
	doc := strings.ToLower(string(u.svc.OpenAPI()))
	if strings.Contains(doc, "legalnamesof") || strings.Contains(doc, "legal-names") || strings.Contains(doc, "legalnames") {
		t.Error("the OpenAPI document mentions a legal-names operation")
	}
}
