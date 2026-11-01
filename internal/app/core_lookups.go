package app

import (
	"fmt"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/departments"
	"github.com/zedgama3/bms/internal/core/identity"
)

// buildLookups constructs the read sides that sit below audit (boundaries.md, "The build
// order", items 2 and 3): departments.Lookup, then authz.Lookup, which takes it. They do no
// I/O, so they build against db.None(). The authz.Lookup is what audit (titles), identity,
// settings and the api shell take, and what every module reads as Deps.Authz; the parts that
// come later (authz.Service, departments.Service) are built on top of them.
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
	a.departmentsLookup = dl
	a.authzLookup = al
	a.permissions = al
	a.deps.authz = al
	return nil
}
