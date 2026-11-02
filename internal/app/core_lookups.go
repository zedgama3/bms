package app

import (
	"fmt"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/departments"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/people"
)

// buildLookups constructs the read sides that sit below the services (boundaries.md, "The
// build order", items 2, 3 and 12): departments.Lookup, then authz.Lookup, which takes it,
// then identity.Lookup and people.Lookup. They do no I/O, so they build against db.None().
// The authz.Lookup is what audit (titles), identity, settings and the api shell take, and
// what every module reads as Deps.Authz; the identity and people Lookups are what notify
// reads to deliver mail (Accounts, Profiles). The parts that come later (authz.Service,
// departments.Service, identity.Service, people.Service) are built on top of them.
func (a *assembly) buildLookups() error {
	const op = "app.lookups.build"
	dtr, _, err := a.scopes(departmentsArea)
	if err != nil {
		return err
	}
	dl, err := departments.NewLookup(departments.LookupOptions{DB: a.dbc, Tracer: dtr})
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	atr, am, err := a.scopes("authz")
	if err != nil {
		return err
	}
	al, err := authz.NewLookup(authz.LookupOptions{
		DB:          a.dbc,
		Clock:       a.clock,
		Logger:      a.component("authz"),
		Tracer:      atr,
		Meter:       am,
		Actor:       identity.Actor,
		Declared:    declaredPermissions(a),
		Departments: dl,
	})
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	itr, _, err := a.scopes("identity")
	if err != nil {
		return err
	}
	il, err := identity.NewLookup(identity.LookupOptions{DB: a.dbc, Tracer: itr})
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	ptr, _, err := a.scopes(peopleArea)
	if err != nil {
		return err
	}
	pl, err := people.NewLookup(people.LookupOptions{DB: a.dbc, Tracer: ptr})
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	a.departmentsLookup = dl
	a.identityLookup = il
	a.peopleLookup = pl
	a.authzLookup = al
	a.deps.authz = al
	return nil
}
