package privacy_test

import (
	"context"
	"testing"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/authz/authztest"
	"github.com/zedgama3/bms/internal/core/privacy"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// Invented people and departments only.
var (
	alice  = model.NewID[model.PersonID]() // the record's subject
	bob    = model.NewID[model.PersonID]() // another signed-in person
	carol  = model.NewID[model.PersonID]() // a holder of the group permissions
	deptA  = model.NewID[model.DepartmentID]()
	deptB  = model.NewID[model.DepartmentID]()
	marker = "marker-quillfeather-0773"
)

const (
	permContacts authz.Permission = "people.contacts.read"
	permMedical  authz.Permission = "volunteers.medical.read"
	permRoster   authz.Permission = "volunteers.rosters.read"
)

// groups used by the Record fixture.
var (
	gContact = privacy.Group{
		Name: "contact", Permission: permContacts, Default: privacy.AudienceHolders,
		Options: []privacy.Audience{privacy.AudienceNobody, privacy.AudienceSelf, privacy.AudienceHolders, privacy.AudienceSignedIn},
	}
	gMedical = privacy.Group{
		Name: "medical", Permission: permMedical, Default: privacy.AudienceSelf,
		Options: []privacy.Audience{privacy.AudienceNobody, privacy.AudienceSelf, privacy.AudienceHolders},
	}
	gRoster = privacy.Group{
		Name: "roster", Permission: permRoster, Default: "booked",
		Options: []privacy.Audience{privacy.AudienceNobody, privacy.AudienceHolders, "booked"},
		Extra:   []privacy.Audience{"booked"},
	}
	gNick = privacy.Group{
		Name: "nick", Default: privacy.AudienceSignedIn,
		Options: []privacy.Audience{privacy.AudienceNobody, privacy.AudienceSignedIn},
	}
	allGroups = []privacy.Group{gContact, gMedical, gRoster, gNick}
)

// Record is a well-tagged response type with one field per interesting combination.
type Record struct {
	Pub    string  `json:"pub" privacy:"public"`
	Int    string  `json:"int,omitempty" privacy:"internal"`
	Phone  *string `json:"phone,omitempty" privacy:"personal,group=contact"`
	Dx     *string `json:"dx,omitempty" privacy:"sensitive,group=medical"`
	Note   *string `json:"note,omitempty" privacy:"personal,group=roster"`
	Nick   *string `json:"nick,omitempty" privacy:"internal,group=nick"`
	Hidden string  `json:"-"`
}

func sp(s string) *string { return &s }

func newRecord() Record {
	return Record{
		Pub: "pub", Int: "int", Phone: sp("555-0100"), Dx: sp("asthma"), Note: sp("note"),
		Nick: sp("al"), Hidden: "kept",
	}
}

// policy builds a Policy for the four fixture groups.
func policy(contact, medical, roster, nick privacy.Audience) privacy.Policy {
	return privacy.Policy{"contact": contact, "medical": medical, "roster": roster, "nick": nick}
}

// allSame returns a policy with the same audience where the group allows it; nick only
// allows nobody and signed_in so the caller passes it separately.
func uniform(a privacy.Audience) privacy.Policy { return policy(a, a, a, a) }

func wantKind(t testing.TB, err error, kind errs.Kind) {
	t.Helper()
	if !errs.Is(err, kind) {
		t.Fatalf("error kind = %v (err %v), want %v", errs.KindOf(err), err, kind)
	}
}

// hasField reports whether err carries a FieldError with this exact path and code.
func hasField(err error, field, code string) bool {
	for _, fe := range validate.Fields(err) {
		if fe.Field == field && fe.Code == code {
			return true
		}
	}
	return false
}

func wantField(t testing.TB, err error, field, code string) {
	t.Helper()
	wantKind(t, err, errs.Invalid)
	if !hasField(err, field, code) {
		var got []string
		for _, fe := range validate.Fields(err) {
			got = append(got, fe.Field+"/"+fe.Code)
		}
		t.Fatalf("no field error %s/%s; got %v", field, code, got)
	}
}

// actorCtx returns a ctx whose actor is p for an authztest.Fake.
func actorCtx(p model.PersonID) context.Context {
	return authztest.WithActor(context.Background(), authz.Actor{Person: p})
}

func signedIn(p model.PersonID) privacy.Viewer { return privacy.Viewer{Person: p} }

// groupsOf returns the names of the fixture groups whose Permission is one of perms: the
// viewer's Extents are keyed by group name (Amendment A1), and a permission shared by two
// groups gives each its own entry.
func groupsOf(perms ...authz.Permission) []string {
	var out []string
	for _, g := range allGroups {
		for _, pm := range perms {
			if g.Permission == pm {
				out = append(out, g.Name)
			}
		}
	}
	return out
}

// holderSite is a viewer who holds the permissions site-wide, as an entry per group.
func holderSite(p model.PersonID, perms ...authz.Permission) privacy.Viewer {
	v := privacy.Viewer{Person: p, Extents: map[string]authz.Extent{}}
	for _, name := range groupsOf(perms...) {
		v.Extents[name] = authz.Extent{Site: true}
	}
	return v
}

// holderIn is a viewer who holds the permissions at department d only.
func holderIn(p model.PersonID, d model.DepartmentID, perms ...authz.Permission) privacy.Viewer {
	v := privacy.Viewer{Person: p, Extents: map[string]authz.Extent{}}
	for _, name := range groupsOf(perms...) {
		v.Extents[name] = authz.Extent{Departments: []model.DepartmentID{d}}
	}
	return v
}

func fieldsOf(err error) []*validate.FieldError { return validate.Fields(err) }
