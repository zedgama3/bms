package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/zedgama3/bms/internal/contracts/membership"
	"github.com/zedgama3/bms/internal/contracts/personpage"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// peoplePart is core part 5: profiles, contacts, consents, search and linking (people.md;
// app.md "How later core services plug in", row 5). It is built after authz, whose service
// it takes. The export and deletion of a person across the core and every module are
// datarights', which takes people as one more source (boundaries step 12).
var peoplePart = part{
	name:    "people",
	declare: declarePeople,
	build:   buildPeople,
	link:    linkPeople,
}

// peopleArea is the route set, audit scope and signal scope of people: /api/people.
const peopleArea = "people"

// declarePeople adds the people migration set, permissions, signals and the person-page
// and membership extension points. No I/O.
func declarePeople(a *assembly) error {
	if err := a.addCoreSet(people.Set); err != nil {
		return err
	}
	a.corePerms = append(a.corePerms, people.Permissions...)
	a.coreSignals = append(a.coreSignals, people.PublishedSignals...)
	a.corePoints = append(a.corePoints, module.ExtensionPointDecl{
		Name: personpage.Point,
		Accepts: func(v any) bool {
			_, ok := v.(personpage.Provider)
			return ok
		},
	})
	a.corePoints = append(a.corePoints, module.ExtensionPointDecl{
		Name: membership.Point,
		Accepts: func(v any) bool {
			_, ok := v.(membership.Provider)
			return ok
		},
	})
	return nil
}

// buildPeople constructs the service (no I/O), subscribes its identity.account-created
// handler through its scope and mounts the people routes.
// Link, which gives the service the modules' page and membership providers, comes later.
func buildPeople(_ context.Context, a *assembly) error {
	const op = "app.people.build"
	if a.deps.audit == nil || a.identity == nil || a.deps.authz == nil || a.settings == nil || a.peopleLookup == nil {
		return errs.E(op, errs.Internal, errors.New("people is built after audit, settings, identity, authz and the people lookup"))
	}
	reader, err := a.settings.Scoped(peopleSettings)
	if err != nil {
		return fmt.Errorf("%s: settings scope: %w", op, err)
	}
	rec, err := a.deps.audit.Scoped(peopleArea)
	if err != nil {
		return fmt.Errorf("%s: audit scope: %w", op, err)
	}
	bus, err := a.disp.Scope(peopleArea, people.PublishedSignals, people.SubscribedSignals)
	if err != nil {
		return fmt.Errorf("%s: signals scope: %w", op, err)
	}
	tr, m, err := a.scopes("people")
	if err != nil {
		return err
	}
	svc, err := people.New(people.Options{
		DB:       a.dbc,
		Clock:    a.clock,
		Logger:   a.component("people"),
		Tracer:   tr,
		Meter:    m,
		Audit:    rec,
		Signals:  bus,
		Authz:    a.deps.authz,
		Actor:    identity.Actor,
		Session:  identity.SessionFrom,
		Accounts: a.identity,
		Settings: reader,
		Lookup:   a.peopleLookup,
	})
	if err != nil {
		return err //nolint:wrapcheck // people.New names itself
	}
	a.people = svc
	a.deps.people = svc
	if err := a.addCoreRoutes(peopleArea, svc.Routes(), svc.OpenAPI()); err != nil {
		return err
	}
	return nil
}

// linkPeople gives the service every contribution to the person page and to the membership
// point, in build order. It runs after module Build and before api.New.
func linkPeople(_ context.Context, a *assembly) error {
	const op = "app.people.link"
	if a.people == nil || a.set == nil {
		return errs.E(op, errs.Internal, errors.New("people is linked after it and the modules are built"))
	}
	contribs, err := a.set.Contributors(personpage.Point)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	var sections []people.SectionProvider
	for _, c := range contribs {
		prov, ok := c.Value.(personpage.Provider)
		if !ok {
			return errs.E(op, errs.Internal, fmt.Errorf("module %q contributed a %T to %s, which is not a personpage.Provider", c.Module, c.Value, personpage.Point))
		}
		sections = append(sections, people.SectionProvider{Module: c.Module, Provider: prov})
	}
	mcontribs, err := a.set.Contributors(membership.Point)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	var members []people.MembershipProvider
	for _, c := range mcontribs {
		prov, ok := c.Value.(membership.Provider)
		if !ok {
			return errs.E(op, errs.Internal, fmt.Errorf("module %q contributed a %T to %s, which is not a membership.Provider", c.Module, c.Value, membership.Point))
		}
		members = append(members, people.MembershipProvider{Module: c.Module, Provider: prov})
	}
	if err := a.people.Link(sections, members); err != nil {
		return err //nolint:wrapcheck // Link names itself
	}
	return nil
}
