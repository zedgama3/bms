package people

import (
	"context"
	"slices"
	"testing"

	"github.com/zedgama3/bms/internal/core/people/rows"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// F73 (D45): an actor who holds people.contacts.read only in department scope (a
// department-admin) while no people.membership provider is registered reaches nobody with
// it: no personal fields, no personal-field search match, no legal names, and no audit
// entry for a read that returned nothing personal. All names and numbers are invented.

const (
	f73Legal = "Quillon Ferndale-Example"
	f73Phone = "+1 555 0142"
)

// searchSpy is a store whose Search answers like the real one: a person who matches only
// on a personal column is returned only when the query selected that column. It records
// the query so the test can see which columns the service asked for.
type searchSpy struct {
	fakeStore
	person  model.PersonID
	queries []rows.SearchQuery
	legal   map[model.PersonID]string
	legalQs int
}

func (s *searchSpy) Search(_ context.Context, q rows.SearchQuery) ([]rows.Match, error) {
	s.queries = append(s.queries, q)
	if !q.LegalName {
		return nil, nil // the only match is on the legal name
	}
	return []rows.Match{{ID: s.person, PlayaName: "Ash Example", Legal: true}}, nil
}

func (s *searchSpy) LegalNamesOf(_ context.Context, ids []model.PersonID) (map[model.PersonID]string, error) {
	s.legalQs++
	out := map[model.PersonID]string{}
	for _, id := range ids {
		if n, ok := s.legal[id]; ok {
			out[id] = n
		}
	}
	return out, nil
}

func personalAudits(r *wbRig) int {
	n := 0
	for _, e := range r.rec.Recorded() {
		if e.Action == actionRead || e.Action == actionSearchRead {
			n++
		}
	}
	return n
}

func TestGet_DepartmentContactsReadNoProvider_OmitsPersonalFields(t *testing.T) { // F73
	dept := model.NewID[model.DepartmentID]()
	other := model.NewID[model.PersonID]()
	row := rows.Person{ID: other, PlayaName: "Ash Example", Pronouns: "they/them", LegalName: f73Legal, Phone: f73Phone}

	t.Run("department scope, no provider", func(t *testing.T) {
		r := newWB(t)
		r.az.GrantIn(r.me, PermissionDirectoryRead, dept)
		r.az.GrantIn(r.me, PermissionContactsRead, dept)
		if err := r.svc.Link(nil, nil); err != nil {
			t.Fatalf("Link: %v", err)
		}
		r.st.person = row
		got, err := r.svc.Get(r.ctx, other)
		if err != nil {
			t.Fatalf("Get: %v (directory.read still reaches everyone, decision 9)", err)
		}
		if got.Person.PlayaName != "Ash Example" {
			t.Errorf("internal fields must still be returned: %+v", got.Person)
		}
		if got.Person.LegalName != "" || got.Person.Phone != "" || got.Person.Email != "" || got.Person.ContactEmail != "" {
			t.Errorf("personal fields reached a department-scoped holder with no provider: %+v", got.Person)
		}
		if len(got.Contacts) != 0 {
			t.Errorf("emergency contacts returned: %d", len(got.Contacts))
		}
		for _, f := range personalFields {
			if slices.Contains(got.Fields, f) {
				t.Errorf("Fields lists personal field %q", f)
			}
		}
		if n := personalAudits(r); n != 0 {
			t.Errorf("audit entries = %d, want none (nothing personal was included)", n)
		}
	})

	t.Run("control: site scope shows and audits them", func(t *testing.T) {
		r := newWB(t, PermissionDirectoryRead, PermissionContactsRead)
		r.st.person = row
		got, err := r.svc.Get(r.ctx, other)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.Person.LegalName != f73Legal || got.Person.Phone != f73Phone {
			t.Errorf("a site holder must see personal fields: %+v", got.Person)
		}
		if n := personalAudits(r); n != 1 {
			t.Errorf("audit entries = %d, want 1", n)
		}
	})
}

func TestSearch_DepartmentContactsReadNoProvider_NoPersonalMatch(t *testing.T) { // F73
	dept := model.NewID[model.DepartmentID]()
	other := model.NewID[model.PersonID]()

	t.Run("department scope, no provider", func(t *testing.T) {
		r := newWB(t)
		spy := &searchSpy{fakeStore: fakeStore{person: rows.Person{ID: r.me}}, person: other}
		r.svc.store = spy
		r.az.GrantIn(r.me, PermissionDirectoryRead, dept)
		r.az.GrantIn(r.me, PermissionContactsRead, dept)
		if err := r.svc.Link(nil, nil); err != nil {
			t.Fatalf("Link: %v", err)
		}
		got, err := r.svc.Search(r.ctx, SearchQuery{Q: "ferndale"})
		if err != nil {
			t.Fatalf("Search: %v (no error, no leak)", err)
		}
		if len(got) != 0 {
			t.Errorf("results = %+v; a legal-name-only match must not be found", got)
		}
		for _, q := range spy.queries {
			if q.LegalName || q.Phone || q.ContactEmail {
				t.Errorf("the service searched personal columns for someone who reaches nobody with contacts.read: %+v", q)
			}
		}
		if n := personalAudits(r); n != 0 {
			t.Errorf("people.search.read entries = %d, want none", n)
		}
	})

	t.Run("control: site scope finds it and audits it", func(t *testing.T) {
		r := newWB(t, PermissionDirectoryRead, PermissionContactsRead)
		r.svc.store = &searchSpy{fakeStore: fakeStore{person: rows.Person{ID: r.me}}, person: other}
		got, err := r.svc.Search(r.ctx, SearchQuery{Q: "ferndale"})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if len(got) != 1 || got[0].ID != other {
			t.Fatalf("results = %+v, want the legal-name match", got)
		}
		if n := personalAudits(r); n != 1 {
			t.Errorf("people.search.read entries = %d, want 1", n)
		}
	})
}

func TestLegalNamesOf_DepartmentContactsReadNoProvider_ReturnsSelfOnly(t *testing.T) { // F73 (A2.8, D46)
	dept := model.NewID[model.DepartmentID]()
	a, b := model.NewID[model.PersonID](), model.NewID[model.PersonID]()
	legal := map[model.PersonID]string{a: f73Legal, b: "Rowan Example"}

	t.Run("department scope, no provider", func(t *testing.T) {
		r := newWB(t)
		r.svc.store = &searchSpy{fakeStore: fakeStore{person: rows.Person{ID: r.me}}, legal: legal}
		r.az.GrantIn(r.me, PermissionContactsRead, dept)
		if err := r.svc.Link(nil, nil); err != nil {
			t.Fatalf("Link: %v", err)
		}
		got, err := r.svc.LegalNamesOf(r.ctx, []model.PersonID{a, b})
		if errs.Is(err, errs.Permission) {
			t.Fatalf("the permission is held, so Permission is wrong: %v", err)
		}
		if err != nil {
			t.Fatalf("LegalNamesOf: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("names = %v, want an empty map for others", got)
		}
		if n := personalAudits(r); n != 0 {
			t.Errorf("people.person.read entries = %d, want none", n)
		}
	})

	t.Run("department scope, no provider: the actor's own name is still answered, unaudited", func(t *testing.T) {
		r := newWB(t)
		own := map[model.PersonID]string{a: f73Legal, b: "Rowan Example", r.me: "Wren Example"}
		r.svc.store = &searchSpy{fakeStore: fakeStore{person: rows.Person{ID: r.me}}, legal: own}
		r.az.GrantIn(r.me, PermissionContactsRead, dept)
		if err := r.svc.Link(nil, nil); err != nil {
			t.Fatalf("Link: %v", err)
		}
		got, err := r.svc.LegalNamesOf(r.ctx, []model.PersonID{a, r.me, b})
		if err != nil {
			t.Fatalf("LegalNamesOf: %v", err)
		}
		if len(got) != 1 || got[r.me] != "Wren Example" {
			t.Errorf("names = %v, want only the actor's own", got)
		}
		if n := personalAudits(r); n != 0 {
			t.Errorf("people.person.read entries = %d, want none (a self read, decision 15)", n)
		}
		// Asking only for oneself works too.
		got, err = r.svc.LegalNamesOf(r.ctx, []model.PersonID{r.me})
		if err != nil || got[r.me] != "Wren Example" {
			t.Errorf("self only: %v, %v", got, err)
		}
	})

	t.Run("site scope, own ID among ids: own name returned, only the others audited", func(t *testing.T) { // C3
		r := newWB(t, PermissionContactsRead)
		own := map[model.PersonID]string{a: f73Legal, b: "Rowan Example", r.me: "Wren Example"}
		r.svc.store = &searchSpy{fakeStore: fakeStore{person: rows.Person{ID: r.me}}, legal: own}
		got, err := r.svc.LegalNamesOf(r.ctx, []model.PersonID{a, r.me, b})
		if err != nil {
			t.Fatalf("LegalNamesOf: %v", err)
		}
		if len(got) != 3 || got[r.me] != "Wren Example" {
			t.Errorf("names = %v, want all three including the actor's own", got)
		}
		if n := personalAudits(r); n != 2 {
			t.Errorf("people.person.read entries = %d, want 2 (a and b; a self read is not audited)", n)
		}
		for _, e := range r.rec.Recorded() {
			if e.Subject == r.me {
				t.Errorf("an audit entry names the actor's own record: %+v", e)
			}
		}
	})

	t.Run("control: site scope returns and audits", func(t *testing.T) {
		r := newWB(t, PermissionContactsRead)
		r.svc.store = &searchSpy{fakeStore: fakeStore{person: rows.Person{ID: r.me}}, legal: legal}
		got, err := r.svc.LegalNamesOf(r.ctx, []model.PersonID{a, b})
		if err != nil {
			t.Fatalf("LegalNamesOf: %v", err)
		}
		if len(got) != 2 {
			t.Errorf("names = %v, want both", got)
		}
		if n := personalAudits(r); n != 2 {
			t.Errorf("people.person.read entries = %d, want 2", n)
		}
	})
}

// A department-scoped contacts.read holder with a membership provider gets legal names for
// the members of the extent's departments only, and only those are audited (D45).
func TestLegalNamesOf_DepartmentContactsReadWithProvider_ReturnsAndAuditsOnlyMembers(t *testing.T) {
	dept, other := model.NewID[model.DepartmentID](), model.NewID[model.DepartmentID]()
	member, stranger, loner := model.NewID[model.PersonID](), model.NewID[model.PersonID](), model.NewID[model.PersonID]()
	r := newWB(t)
	r.svc.store = &searchSpy{fakeStore: fakeStore{person: rows.Person{ID: r.me}}, legal: map[model.PersonID]string{
		member: "Odalys Example", stranger: "Marisol Example", loner: "Rowan Example",
	}}
	r.az.GrantIn(r.me, PermissionContactsRead, dept)
	prov := deptMembers{of: map[model.PersonID][]model.DepartmentID{member: {dept}, stranger: {other}}}
	if err := r.svc.Link(nil, []MembershipProvider{{Module: "m", Provider: prov}}); err != nil {
		t.Fatalf("Link: %v", err)
	}
	got, err := r.svc.LegalNamesOf(r.ctx, []model.PersonID{member, stranger, loner})
	if err != nil {
		t.Fatalf("LegalNamesOf: %v", err)
	}
	if len(got) != 1 || got[member] != "Odalys Example" {
		t.Errorf("names = %v, want only the member", got)
	}
	if n := personalAudits(r); n != 1 {
		t.Errorf("people.person.read entries = %d, want 1 (the member only)", n)
	}
}
