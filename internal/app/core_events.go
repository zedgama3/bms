package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/zedgama3/bms/internal/contracts/eventsetup"
	"github.com/zedgama3/bms/internal/core/events"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/lib/errs"

	// The embedded time zone database, so an event's zone name resolves on a host without
	// system zone data (departments-events.md, "Proposed changes elsewhere" item 3).
	_ "time/tzdata"
)

// eventsPart is core part 7: the regional's events, kinds and schedule, the Directory
// modules label their records with, and the event-setup extension point (departments-events.md;
// app.md "The build order"; D37). It is built after authz.
var eventsPart = part{
	name:    "events",
	declare: declareEvents,
	build:   buildEvents,
	link:    linkEvents,
}

// eventsArea is the route set, audit scope and signal scope of events: /api/events.
const eventsArea = "events"

// declareEvents adds the events migration set, permissions, signals and the event-setup
// extension point. No I/O.
func declareEvents(a *assembly) error {
	if err := a.addCoreSet(events.Set); err != nil {
		return err
	}
	a.corePerms = append(a.corePerms, events.Permissions...)
	a.coreSignals = append(a.coreSignals, events.PublishedSignals...)
	a.corePoints = append(a.corePoints, module.ExtensionPointDecl{
		Name: eventsetup.Point,
		Accepts: func(v any) bool {
			_, ok := v.(eventsetup.Provider)
			return ok
		},
	})
	return nil
}

// buildEvents constructs the service (no I/O), shares it as Deps.Events and mounts the
// events routes. Link, which gives the service the modules' set-up providers, comes later.
func buildEvents(_ context.Context, a *assembly) error {
	const op = "app.events.build"
	if a.deps.audit == nil || a.deps.authz == nil {
		return errs.E(op, errs.Internal, errors.New("events is built after audit and authz"))
	}
	rec, err := a.deps.audit.Scoped(eventsArea)
	if err != nil {
		return fmt.Errorf("%s: audit scope: %w", op, err)
	}
	bus, err := a.disp.Scope(eventsArea, events.PublishedSignals, nil)
	if err != nil {
		return fmt.Errorf("%s: signals scope: %w", op, err)
	}
	tr, m, err := a.scopes(eventsArea)
	if err != nil {
		return err
	}
	svc, err := events.New(events.Options{
		DB:           a.dbc,
		Clock:        a.clock,
		Logger:       a.component(eventsArea),
		Tracer:       tr,
		Meter:        m,
		Audit:        rec,
		Signals:      bus,
		Authz:        a.deps.authz,
		Actor:        identity.Actor,
		SetupTimeout: events.DefaultSetupTimeout,
	})
	if err != nil {
		return err //nolint:wrapcheck // events.New names itself
	}
	a.events = svc
	a.deps.events = svc
	if err := a.addCoreRoutes(eventsArea, svc.Routes(), svc.OpenAPI()); err != nil {
		return err
	}
	return nil
}

// linkEvents gives the service every module's contribution to events.event-setup, in build
// order. It runs after module Build and before api.New.
func linkEvents(_ context.Context, a *assembly) error {
	const op = "app.events.link"
	if a.events == nil || a.set == nil {
		return errs.E(op, errs.Internal, errors.New("events is linked after it and the modules are built"))
	}
	contribs, err := a.set.Contributors(eventsetup.Point)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	var providers []events.SetupProvider
	for _, c := range contribs {
		prov, ok := c.Value.(eventsetup.Provider)
		if !ok {
			return errs.E(op, errs.Internal, fmt.Errorf("module %q contributed a %T to %s, which is not an eventsetup.Provider", c.Module, c.Value, eventsetup.Point))
		}
		providers = append(providers, events.SetupProvider{Module: c.Module, Provider: prov})
	}
	if err := a.events.Link(providers); err != nil {
		return err //nolint:wrapcheck // Link names itself
	}
	return nil
}
