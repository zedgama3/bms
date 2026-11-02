package people

import (
	"context"
	"errors"
	"testing"

	"github.com/zedgama3/bms/internal/contracts/membership"
	"github.com/zedgama3/bms/internal/core/people/rows"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// memberStore answers the searches and legal-name reads the membership tests make.
type memberStore struct {
	fakeStore
	matches []rows.Match
	legal   map[model.PersonID]string
}

func (m *memberStore) Search(_ context.Context, q rows.SearchQuery) ([]rows.Match, error) {
	out := make([]rows.Match, 0, len(m.matches))
	for _, mt := range m.matches {
		if !q.LegalName {
			mt.Legal = false
		}
		out = append(out, mt)
	}
	return out, nil
}

func (m *memberStore) LegalNamesOf(_ context.Context, ids []model.PersonID) (map[model.PersonID]string, error) {
	out := map[model.PersonID]string{}
	for _, id := range ids {
		if n, ok := m.legal[id]; ok {
			out[id] = n
		}
	}
	return out, nil
}

// deptMembers is a membership.Provider over a fixed table.
type deptMembers struct {
	of  map[model.PersonID][]model.DepartmentID
	err error
}

func (d deptMembers) DepartmentsOf(_ context.Context, ids []model.PersonID) (map[model.PersonID][]model.DepartmentID, error) {
	if d.err != nil {
		return nil, d.err
	}
	out := map[model.PersonID][]model.DepartmentID{}
	for _, id := range ids {
		if ds, ok := d.of[id]; ok {
			out[id] = ds
		}
	}
	return out, nil
}

var _ membership.Provider = deptMembers{}

func TestReachOf_ExtentAndProviders(t *testing.T) {
	dept := model.NewID[model.DepartmentID]()
	other := model.NewID[model.DepartmentID]()
	in, out := model.NewID[model.PersonID](), model.NewID[model.PersonID]()
	prov := deptMembers{of: map[model.PersonID][]model.DepartmentID{in: {dept}, out: {other}}}
	tests := []struct {
		name      string
		site      bool
		dept      bool
		contacts  bool // ask for contacts.read instead of directory.read
		providers []MembershipProvider
		wantIn    bool
		wantOut   bool
	}{
		{name: "nowhere reaches nobody", providers: []MembershipProvider{{Module: "m", Provider: prov}}},
		{name: "site reaches everyone", site: true, providers: []MembershipProvider{{Module: "m", Provider: prov}}, wantIn: true, wantOut: true},
		{name: "directory.read, department without a provider reaches everyone (decision 9)", dept: true, wantIn: true, wantOut: true},
		{name: "contacts.read, department without a provider reaches nobody (D45)", dept: true, contacts: true},
		{name: "contacts.read, site without a provider reaches everyone", site: true, contacts: true, wantIn: true, wantOut: true},
		{name: "contacts.read, department with a provider reaches its members", dept: true, contacts: true, providers: []MembershipProvider{{Module: "m", Provider: prov}}, wantIn: true},
		{name: "department with a provider reaches its members", dept: true, providers: []MembershipProvider{{Module: "m", Provider: prov}}, wantIn: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newWB(t)
			perm := PermissionDirectoryRead
			if tc.contacts {
				perm = PermissionContactsRead
			}
			switch {
			case tc.site:
				r.az.Grant(r.me, perm)
			case tc.dept:
				r.az.GrantIn(r.me, perm, dept)
			}
			if err := r.svc.Link(nil, tc.providers); err != nil {
				t.Fatalf("Link: %v", err)
			}
			got, err := r.svc.reachOf(r.ctx, perm, []model.PersonID{in, out})
			if err != nil {
				t.Fatalf("reachOf: %v", err)
			}
			if got.has(in) != tc.wantIn || got.has(out) != tc.wantOut {
				t.Errorf("has(in) = %v, has(out) = %v; want %v, %v", got.has(in), got.has(out), tc.wantIn, tc.wantOut)
			}
		})
	}
}

func TestReachOf_ProviderFails_FailsClosed(t *testing.T) {
	dept := model.NewID[model.DepartmentID]()
	r := newWB(t)
	r.az.GrantIn(r.me, PermissionDirectoryRead, dept)
	boom := errs.E("volunteers.DepartmentsOf", errs.Unavailable, errors.New("down"))
	if err := r.svc.Link(nil, []MembershipProvider{{Module: "volunteers", Provider: deptMembers{err: boom}}}); err != nil {
		t.Fatalf("Link: %v", err)
	}
	_, err := r.svc.reachOf(r.ctx, PermissionDirectoryRead, []model.PersonID{model.NewID[model.PersonID]()})
	if !errs.Is(err, errs.Unavailable) {
		t.Errorf("kind = %v, want Unavailable (err %v)", errs.KindOf(err), err)
	}
}

func TestLink_MembershipProviderWithoutName_ReturnsInternal(t *testing.T) {
	r := newWB(t)
	err := r.svc.Link(nil, []MembershipProvider{{Provider: deptMembers{}}})
	if !errs.Is(err, errs.Internal) {
		t.Errorf("kind = %v, want Internal", errs.KindOf(err))
	}
}

func TestSearch_DepartmentExtentWithProvider_ReturnsOnlyMembersAndAuditsLegalNameMatch(t *testing.T) {
	dept := model.NewID[model.DepartmentID]()
	member, stranger := model.NewID[model.PersonID](), model.NewID[model.PersonID]()
	r := newWB(t)
	st := &memberStore{fakeStore: fakeStore{person: rows.Person{ID: r.me}}, matches: []rows.Match{
		{ID: member, PlayaName: "Dusty Ember", Legal: true},
		{ID: stranger, PlayaName: "Ash Example"},
	}}
	r.svc.store = st
	r.az.GrantIn(r.me, PermissionDirectoryRead, dept)
	r.az.GrantIn(r.me, PermissionContactsRead, dept)
	prov := deptMembers{of: map[model.PersonID][]model.DepartmentID{member: {dept}}}
	if err := r.svc.Link(nil, []MembershipProvider{{Module: "m", Provider: prov}}); err != nil {
		t.Fatalf("Link: %v", err)
	}
	got, err := r.svc.Search(r.ctx, SearchQuery{Q: "ember"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 || got[0].ID != member {
		t.Fatalf("results = %+v, want only the member", got)
	}
	var searchReads int
	for _, e := range r.rec.Recorded() {
		if e.Action == actionSearchRead {
			searchReads++
		}
	}
	if searchReads != 1 {
		t.Errorf("people.search.read entries = %d, want 1 (the member matched on legal name)", searchReads)
	}
}

func TestGet_DepartmentExtentWithProvider_NonMemberIsNotFound(t *testing.T) {
	dept := model.NewID[model.DepartmentID]()
	member, stranger := model.NewID[model.PersonID](), model.NewID[model.PersonID]()
	r := newWB(t)
	r.az.GrantIn(r.me, PermissionDirectoryRead, dept)
	prov := deptMembers{of: map[model.PersonID][]model.DepartmentID{member: {dept}}}
	if err := r.svc.Link(nil, []MembershipProvider{{Module: "m", Provider: prov}}); err != nil {
		t.Fatalf("Link: %v", err)
	}
	r.st.person = rows.Person{ID: member, PlayaName: "Dusty Ember"}
	if _, err := r.svc.Get(r.ctx, member); err != nil {
		t.Errorf("Get(member): %v", err)
	}
	if _, err := r.svc.Get(r.ctx, stranger); !errs.Is(err, errs.NotFound) {
		t.Errorf("Get(stranger) kind = %v, want NotFound", errs.KindOf(err))
	}
}

func TestLegalNamesOf_HoldsContactsRead_ReturnsAndAuditsEachPerson(t *testing.T) {
	a, b, none := model.NewID[model.PersonID](), model.NewID[model.PersonID](), model.NewID[model.PersonID]()
	r := newWB(t, PermissionContactsRead)
	r.svc.store = &memberStore{fakeStore: fakeStore{}, legal: map[model.PersonID]string{a: "Dusty Example", b: "Ash Example"}}
	got, err := r.svc.LegalNamesOf(r.ctx, []model.PersonID{a, b, none, a})
	if err != nil {
		t.Fatalf("LegalNamesOf: %v", err)
	}
	if len(got) != 2 || got[a] != "Dusty Example" || got[b] != "Ash Example" {
		t.Errorf("names = %v", got)
	}
	var reads int
	for _, e := range r.rec.Recorded() {
		if e.Action == actionRead && len(e.Fields) == 1 && e.Fields[0] == FieldLegalName {
			reads++
		}
	}
	if reads != 2 {
		t.Errorf("people.person.read entries = %d, want 2", reads)
	}
}
