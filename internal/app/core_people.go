package app

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/zedgama3/bms/internal/contracts/personpage"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// peoplePart is core part 5: profiles, contacts, consents, and the export and deletion of
// one person across the core and every module (people.md; app.md "How later core services
// plug in", row 5). It is built after authz, whose service it takes.
var peoplePart = part{
	name:    "people",
	declare: declarePeople,
	build:   buildPeople,
	link:    linkPeople,
}

// peopleArea is the route set, audit scope and signal scope of people: /api/people.
const peopleArea = "people"

// declarePeople adds the people migration set, permissions, signals, the person-page
// extension point and the export and delete commands. No I/O.
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
	a.commands = append(a.commands,
		command{
			name:    "export",
			summary: "print everything held about one person as JSON (personal data)",
			usage:   "<person-id>",
			run:     runExport,
		},
		command{
			name:    "delete",
			summary: "delete one person everywhere and print what was deleted",
			usage:   "<person-id>",
			run:     runDelete,
		},
	)
	return nil
}

// buildPeople constructs the service (no I/O), subscribes its identity.account-created
// handler through its scope, fills identity's NameSource and mounts the people routes.
// Link, which gives the service the modules' PersonData and page providers, comes later.
func buildPeople(_ context.Context, a *assembly) error {
	const op = "app.people.build"
	if a.deps.audit == nil || a.identity == nil || a.authz == nil || a.deps.authz == nil {
		return errs.E(op, errs.Internal, errors.New("people is built after audit, identity and authz"))
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
		DB:         a.dbc,
		Clock:      a.clock,
		Logger:     a.component("people"),
		Tracer:     tr,
		Meter:      m,
		Audit:      rec,
		Signals:    bus,
		Authz:      a.deps.authz,
		Actor:      authzActor,
		Accounts:   a.identity,
		Access:     a.authz,
		AuditCount: a.deps.audit,
		Policy:     lazyPeoplePolicy{a: a},
		Notify:     lazyNotifier{a: a, owner: peopleArea},
		Holders:    a.authz,
	})
	if err != nil {
		return err //nolint:wrapcheck // people.New names itself
	}
	a.people = svc
	a.deps.people = svc
	a.names = svc
	a.routeSets = append(a.routeSets, module.ModuleRoutes{
		Module:  peopleArea,
		Routes:  peopleRoutes(svc.Routes()),
		OpenAPI: svc.OpenAPI(),
	})
	return nil
}

// peopleRoutes converts people's routes (people cannot import core/module) to module
// routes: all signed-in, the permission being checked inside each use case.
func peopleRoutes(in []people.Route) []module.Route {
	out := make([]module.Route, len(in))
	for i, r := range in {
		out[i] = module.Route{Method: r.Method, Pattern: r.Pattern, Access: module.AccessSignedIn, Handler: r.Handler}
	}
	return out
}

// linkPeople gives the service every built module's PersonData and every contribution to the
// person page, in build order. It runs after module Build and before api.New.
func linkPeople(_ context.Context, a *assembly) error {
	const op = "app.people.link"
	if a.people == nil || a.set == nil {
		return errs.E(op, errs.Internal, errors.New("people is linked after it and the modules are built"))
	}
	var sources []people.Source
	for _, pd := range a.set.PersonData() {
		sources = append(sources, people.Source{Name: pd.Module, Exporter: personData{pd.PersonData}})
	}
	if a.notify != nil {
		sources = append(sources, notifySource(a.notify))
	}
	if a.files != nil { // after the modules, which delete their files first
		sources = append(sources, filesSource(a.files))
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
	if err := a.people.Link(sources, sections); err != nil {
		return err //nolint:wrapcheck // Link names itself
	}
	return nil
}

// personData adapts a module's PersonData to people's twin interface (people cannot import
// core/module, so the two sets of structural twins meet here).
type personData struct{ module.PersonData }

// ExportPerson is the module's export as people's Export.
func (p personData) ExportPerson(ctx context.Context, person model.PersonID) (people.Export, error) {
	e, err := p.PersonData.ExportPerson(ctx, person)
	if err != nil {
		return people.Export{}, err //nolint:wrapcheck // people names the module
	}
	return people.Export{Data: e.Data}, nil
}

// DeletePerson is the module's deletion as people's ModuleDeletion.
func (p personData) DeletePerson(ctx context.Context, person model.PersonID) (people.ModuleDeletion, error) {
	d, err := p.PersonData.DeletePerson(ctx, person)
	if err != nil {
		return people.ModuleDeletion{}, err //nolint:wrapcheck // people names the module
	}
	out := people.ModuleDeletion{Deleted: d.Deleted, Anonymized: d.Anonymized}
	for _, k := range d.Kept {
		out.Kept = append(out.Kept, people.Kept{Kind: k.Kind, Count: k.Count})
	}
	return out, nil
}

// runExport is `bms export <person-id>` (people.md "The commands"): the document goes to
// stdout, the log to stderr. Exit 2: a malformed id or configuration; 3: a schema behind;
// 1: everything else, an unknown person and an Invalid from a module included. It works on a read-only copy (the audit
// entry goes to the spool).
func runExport(ctx context.Context, p *process) int {
	return runPersonCommand(ctx, p, "export", false, func(ctx context.Context, svc *people.Service, out io.Writer, id string) error {
		return svc.ExportCommand(ctx, out, id)
	})
}

// runDelete is `bms delete <person-id>`: the report goes to stdout. Exit codes as export;
// a read-only copy exits 1, and so does the last access admin (Conflict last_access_admin).
func runDelete(ctx context.Context, p *process) int {
	return runPersonCommand(ctx, p, "delete", true, func(ctx context.Context, svc *people.Service, out io.Writer, id string) error {
		return svc.DeleteCommand(ctx, out, id)
	})
}

// runPersonCommand builds the core and the modules (not started), links people, and runs fn
// as the operator. It loads configuration like serve and requires the session key.
func runPersonCommand(ctx context.Context, p *process, name string, writes bool, fn func(context.Context, *people.Service, io.Writer, string) error) int {
	op := "app." + name
	// A malformed person ID is the operator's input and the only reason these commands exit
	// 2 once the configuration is right (people.md F50); the service checks it again.
	if id, err := model.ParseID[model.PersonID](p.args[1]); err != nil || id.IsZero() {
		sayln(p.stderr, "person: must be a UUID")
		return ExitUsage
	}
	cfg, err := p.loadConfig(ctx)
	if err != nil {
		return p.earlyFail(ctx, err, ExitUsage)
	}
	p.warnDebugToStderr(cfg)
	if err := requireSessionKey(cfg); err != nil {
		p.problem(err)
		return ExitUsage
	}
	a := newAssembly(p, cfg)
	a.operator = true
	if err := a.startTelemetry(ctx, p.lookup, p.stderr); err != nil {
		return p.earlyFail(ctx, err, ExitUsage)
	}
	bail := func(err error, status int) int {
		code := a.fail(ctx, p, err, status)
		if cerr := a.closeQuiet(ctx); cerr != nil {
			p.problem(cerr)
		}
		return code
	}
	enabled, err := enabledModules(p.registry, cfg.Modules.Disabled)
	if err != nil {
		return bail(err, ExitUsage)
	}
	if err := a.resolve(ctx, p.registry, enabled); err != nil {
		return bail(err, ExitUsage)
	}
	if err := a.openDatabase(ctx); err != nil {
		return bail(err, statusOf(err, ExitFailure))
	}
	if writes && a.pool.ReadOnly() {
		return bail(errs.E(op, errs.Unavailable, errReadOnlyCopy), ExitFailure)
	}
	if err := a.newDispatcher(); err != nil {
		return bail(err, ExitFailure)
	}
	if err := a.buildModules(ctx, a.migrator); err != nil {
		return bail(err, statusOf(err, ExitSchema))
	}
	if a.people == nil {
		return bail(errs.E(op, errs.Internal, errors.New("the people part built no service")), ExitFailure)
	}
	cctx, cancel := context.WithTimeout(ctx, checkTimeout)
	err = fn(cctx, a.people, p.stdout, p.args[1])
	cancel()
	if err != nil {
		return bail(err, ExitFailure)
	}
	if err := a.closeQuiet(ctx); err != nil {
		p.problem(err)
		return ExitFailure
	}
	return ExitOK
}
