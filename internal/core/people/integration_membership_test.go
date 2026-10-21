//go:build integration

package people_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// D44 amendment, part 3: the people.membership extension point. With a provider registered,
// a department-scoped people.directory.read@D and people.contacts.read@D extent reaches
// only the people the providers report as members of D (F71). With none, decision 9 holds:
// any extent reaches every person.

type memberProvider struct {
	members map[model.PersonID][]model.DepartmentID
	err     error
	calls   atomic.Int32
}

func (p *memberProvider) DepartmentsOf(_ context.Context, ids []model.PersonID) (map[model.PersonID][]model.DepartmentID, error) {
	p.calls.Add(1)
	if p.err != nil {
		return nil, p.err
	}
	out := map[model.PersonID][]model.DepartmentID{}
	for _, id := range ids {
		if d, ok := p.members[id]; ok {
			out[id] = d
		}
	}
	return out, nil
}

type memberFixture struct {
	*irig
	d1, d2                model.DepartmentID
	one, two, both, loner model.PersonID
	prov                  *memberProvider
}

// newMemberFixture seeds four people (all playa names start "Zqx") and a provider where
// one belongs to d1, two to d2, both to both, and loner to nothing. The provider is not
// linked: tests call link.
func newMemberFixture(t *testing.T) *memberFixture {
	t.Helper()
	r := newIRig(t, false)
	f := &memberFixture{irig: r, d1: model.NewID[model.DepartmentID](), d2: model.NewID[model.DepartmentID]()}
	f.one = r.insertPerson(seed{Playa: "Zqx One", Legal: "Odalys Brindle"})
	f.two = r.insertPerson(seed{Playa: "Zqx Two", Legal: "Fenwick Tarn"})
	f.both = r.insertPerson(seed{Playa: "Zqx Both", Legal: "Marisol Quenby"})
	f.loner = r.insertPerson(seed{Playa: "Zqx Loner", Legal: "Tobiah Ravensworth"})
	f.prov = &memberProvider{members: map[model.PersonID][]model.DepartmentID{
		f.one:  {f.d1},
		f.two:  {f.d2},
		f.both: {f.d1, f.d2},
	}}
	return f
}

func (f *memberFixture) linkProviders(provs ...*memberProvider) {
	f.t.Helper()
	var ms []people.MembershipProvider
	for i, p := range provs {
		ms = append(ms, people.MembershipProvider{Module: "volunteers" + string(rune('a'+i)), Provider: p})
	}
	wantNoErr(f.t, f.svc.Link(nil, nil, ms))
}

// deptViewer is a person holding perms only for the given departments.
func (f *memberFixture) deptViewer(perm authz.Permission, depts ...model.DepartmentID) context.Context {
	f.t.Helper()
	p := f.insertPerson(seed{Playa: "Viewer"})
	for _, d := range depts {
		f.az.GrantIn(p, perm, d)
	}
	return asPerson(ctxT(f.t), p)
}

func (f *memberFixture) search(ctx context.Context, q string) ([]model.PersonID, error) {
	ms, err := f.svc.Search(ctx, people.SearchQuery{Q: q})
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	var ids []model.PersonID
	for _, m := range ms {
		ids = append(ids, m.ID)
	}
	return ids, nil
}

func sameIDs(got []model.PersonID, want ...model.PersonID) bool {
	if len(got) != len(want) {
		return false
	}
	for _, w := range want {
		if !slices.Contains(got, w) {
			return false
		}
	}
	return true
}

// F71
func TestSearch_DepartmentExtent_ReachesOnlyMembers(t *testing.T) {
	f := newMemberFixture(t)
	f.linkProviders(f.prov)
	got, err := f.search(f.deptViewer(permDirectory, f.d1), "Zqx")
	wantNoErr(t, err)
	if !sameIDs(got, f.one, f.both) {
		t.Fatalf("reached %d people, want exactly the members of the department (one, both)", len(got))
	}
}

func TestSearch_TwoDepartmentExtent_ReachesMembersOfEither(t *testing.T) {
	f := newMemberFixture(t)
	f.linkProviders(f.prov)
	got, err := f.search(f.deptViewer(permDirectory, f.d1, f.d2), "Zqx")
	wantNoErr(t, err)
	if !sameIDs(got, f.one, f.two, f.both) {
		t.Fatalf("reached %d people, want one, two and both (not the loner)", len(got))
	}
}

func TestSearch_DepartmentOfNoMembers_ReachesNobodyWithoutError(t *testing.T) {
	f := newMemberFixture(t)
	f.linkProviders(f.prov)
	got, err := f.search(f.deptViewer(permDirectory, model.NewID[model.DepartmentID]()), "Zqx")
	wantNoErr(t, err)
	if len(got) != 0 {
		t.Fatalf("reached %d people in a department with no members", len(got))
	}
}

func TestSearch_SiteExtent_WithProvider_ReachesEveryone(t *testing.T) {
	f := newMemberFixture(t)
	f.linkProviders(f.prov)
	_, ctx := f.admin(permDirectory)
	got, err := f.search(ctx, "Zqx")
	wantNoErr(t, err)
	if !sameIDs(got, f.one, f.two, f.both, f.loner) {
		t.Fatalf("reached %d people, want all four", len(got))
	}
}

// Decision 9 stands where no provider is registered.
func TestSearch_DepartmentExtent_NoProvider_ReachesEveryone(t *testing.T) {
	f := newMemberFixture(t)
	f.linkProviders() // Link with no providers
	got, err := f.search(f.deptViewer(permDirectory, f.d1), "Zqx")
	wantNoErr(t, err)
	if !sameIDs(got, f.one, f.two, f.both, f.loner) {
		t.Fatalf("reached %d people with no provider, want everyone (decision 9)", len(got))
	}
}

func TestSearch_TwoProviders_MembershipIsTheUnion(t *testing.T) {
	f := newMemberFixture(t)
	a := &memberProvider{members: map[model.PersonID][]model.DepartmentID{f.one: {f.d1}}}
	b := &memberProvider{members: map[model.PersonID][]model.DepartmentID{f.loner: {f.d1}}}
	f.linkProviders(a, b)
	got, err := f.search(f.deptViewer(permDirectory, f.d1), "Zqx")
	wantNoErr(t, err)
	if !sameIDs(got, f.one, f.loner) {
		t.Fatalf("reached %d people, want one and loner", len(got))
	}
}

func TestSearch_ProviderFails_FailsClosedWithItsKind(t *testing.T) {
	f := newMemberFixture(t)
	f.prov.err = errs.E("test.provider", errs.Unavailable, errors.New("down"))
	f.linkProviders(f.prov)
	got, err := f.search(f.deptViewer(permDirectory, f.d1), "Zqx")
	wantKind(t, err, errs.Unavailable)
	if len(got) != 0 {
		t.Errorf("a broken membership widened access: %d people", len(got))
	}
}

func TestSearch_CanceledContextWithProvider_ReturnsError(t *testing.T) {
	f := newMemberFixture(t)
	f.linkProviders(f.prov)
	ctx, cancel := context.WithCancel(f.deptViewer(permDirectory, f.d1))
	cancel()
	_, err := f.search(ctx, "Zqx")
	if err == nil {
		t.Fatal("got nil error on a canceled context")
	}
}

func TestGet_DepartmentExtent_MemberReadable_NonMemberHidden(t *testing.T) {
	f := newMemberFixture(t)
	f.linkProviders(f.prov)
	ctx := f.deptViewer(permDirectory, f.d1)
	prof, err := f.svc.Get(ctx, f.one)
	wantNoErr(t, err)
	if prof.Person.PlayaName != "Zqx One" {
		t.Errorf("profile = %+v", prof.Person)
	}
	if _, err := f.svc.Get(ctx, f.both); err != nil {
		t.Errorf("member of both departments: %v", err)
	}
	// A person outside the extent is answered as if absent (privacy-data: a 403 must not
	// confirm that an ID exists).
	for name, id := range map[string]model.PersonID{"another department": f.two, "no department": f.loner} {
		prof, err := f.svc.Get(ctx, id)
		wantKind(t, err, errs.NotFound)
		if prof.Person.PlayaName != "" {
			t.Errorf("%s: fields returned with the error: %+v", name, prof.Person)
		}
	}
}

func TestGet_DepartmentExtent_NoProvider_ReachesEveryone(t *testing.T) {
	f := newMemberFixture(t)
	f.linkProviders()
	_, err := f.svc.Get(f.deptViewer(permDirectory, f.d1), f.loner)
	wantNoErr(t, err)
}

// contacts.read@D: personal fields of D's members only; others are shown without them and
// without an audit entry.
func TestGet_ContactsDepartmentExtent_PersonalFieldsOnlyForMembers(t *testing.T) {
	f := newMemberFixture(t)
	f.linkProviders(f.prov)
	p := f.insertPerson(seed{Playa: "Viewer"})
	f.az.Grant(p, permDirectory) // site-wide directory
	f.az.GrantIn(p, permContacts, f.d1)
	ctx := asPerson(ctxT(t), p)

	member, err := f.svc.Get(ctx, f.one)
	wantNoErr(t, err)
	if member.Person.LegalName != "Odalys Brindle" {
		t.Errorf("member's legal name = %q", member.Person.LegalName)
	}
	if n := f.count("people.person.read"); n != 1 {
		t.Fatalf("%d audit entries after reading a member, want 1", n)
	}
	other, err := f.svc.Get(ctx, f.two)
	wantNoErr(t, err)
	if other.Person.PlayaName != "Zqx Two" {
		t.Errorf("internal fields of a non-member = %+v", other.Person)
	}
	if other.Person.LegalName != "" || other.Person.Phone != "" || other.Person.Email != "" || len(other.Contacts) != 0 {
		t.Errorf("personal fields of a person outside the department leaked: %+v", other.Person)
	}
	if n := f.count("people.person.read"); n != 1 {
		t.Errorf("%d audit entries, want still 1 (nothing personal was returned for the non-member)", n)
	}
}

// A personal-field match (the legal name) only counts for people the contacts extent reaches.
func TestSearch_ContactsDepartmentExtent_LegalNameMatchesOnlyMembers(t *testing.T) {
	f := newMemberFixture(t)
	f.linkProviders(f.prov)
	p := f.insertPerson(seed{Playa: "Viewer"})
	f.az.Grant(p, permDirectory)
	f.az.GrantIn(p, permContacts, f.d1)
	ctx := asPerson(ctxT(t), p)

	got, err := f.search(ctx, "Brindle") // one's legal name, one is in d1
	wantNoErr(t, err)
	if !sameIDs(got, f.one) {
		t.Errorf("legal name of a member: reached %d people, want one", len(got))
	}
	got, err = f.search(ctx, "Tarn") // two's legal name, two is not in d1
	wantNoErr(t, err)
	if len(got) != 0 {
		t.Errorf("legal name of a non-member matched %d people", len(got))
	}
}

func TestSearch_ConcurrentWithProvider_IsSafe(t *testing.T) {
	f := newMemberFixture(t)
	f.linkProviders(f.prov)
	ctx := f.deptViewer(permDirectory, f.d1)
	done := make(chan struct{})
	for range 6 {
		go func() {
			defer func() { done <- struct{}{} }()
			got, err := f.search(ctx, "Zqx")
			if err != nil || !sameIDs(got, f.one, f.both) {
				t.Errorf("got %d people, %v", len(got), err)
			}
		}()
	}
	for range 6 {
		<-done
	}
}
