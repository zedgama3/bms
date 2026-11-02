package app

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/datarights"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// datarightsPart is the per-person export and deletion across the core and every module,
// the deletion requests and their notice (boundaries.md, "datarights"). It is built after
// people (its own rows are one more source), identity, authz, notify, auditlog and files,
// and linked with the modules' PersonData once the modules are built.
var datarightsPart = part{
	name:    datarights.Area,
	declare: declareDatarights,
	build:   buildDatarights,
	link:    linkDatarights,
}

// declareDatarights adds datarights' permissions, its signal and the export and delete
// commands. No I/O. Its requests table, core.data_request, is created by people's migration
// set, so there is no set of its own.
func declareDatarights(a *assembly) error {
	a.corePerms = append(a.corePerms, datarights.Permissions...)
	a.coreSignals = append(a.coreSignals, datarights.PublishedSignals...)
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

// buildDatarights constructs the service (no I/O: it succeeds against db.None() and on a
// read-only copy) and mounts the "datarights" route set. The services are passed in their
// own shapes. Link, which gives the service the modules' PersonData, comes later.
func buildDatarights(_ context.Context, a *assembly) error {
	const op = "app.datarights.build"
	if a.deps.audit == nil || a.deps.authz == nil || a.authzLookup == nil || a.identity == nil || a.authz == nil ||
		a.auditlog == nil || a.files == nil || a.notify == nil || a.people == nil {
		return errs.E(op, errs.Internal, errors.New("datarights is built after audit, authz, identity, notify, auditlog, files and people"))
	}
	notifier, err := a.notify.Scoped(datarights.Area)
	if err != nil {
		return fmt.Errorf("%s: notify scope: %w", op, err)
	}
	rec, err := a.deps.audit.Scoped(datarights.Area)
	if err != nil {
		return fmt.Errorf("%s: audit scope: %w", op, err)
	}
	bus, err := a.disp.Scope(datarights.Area, datarights.PublishedSignals, nil)
	if err != nil {
		return fmt.Errorf("%s: signals scope: %w", op, err)
	}
	tr, m, err := a.scopes(datarights.Area)
	if err != nil {
		return err
	}
	svc, err := datarights.New(datarights.Options{
		DB:       a.dbc,
		Clock:    a.clock,
		Logger:   a.component(datarights.Area),
		Tracer:   tr,
		Meter:    m,
		Audit:    rec,
		Signals:  bus,
		Authz:    a.deps.authz,
		Actor:    identity.Actor,
		Holders:  a.authzLookup,
		Notify:   notifier,
		AuditLog: a.auditlog,
		Accounts: a.identity,
		Access:   a.authz,
		Outbox:   a.notify,
		Files:    a.files,
		People:   a.people,
	})
	if err != nil {
		return err //nolint:wrapcheck // datarights.New names itself
	}
	a.datarights = svc
	return a.addCoreRoutes(datarights.Area, svc.Routes(), svc.OpenAPI())
}

// linkDatarights gives the service every built module's PersonData, in build order. It runs
// after module Build and before api.New.
func linkDatarights(_ context.Context, a *assembly) error {
	const op = "app.datarights.link"
	if a.datarights == nil || a.set == nil {
		return errs.E(op, errs.Internal, errors.New("datarights is linked after it and the modules are built"))
	}
	var sources []datarights.Source
	for _, pd := range a.set.PersonData() {
		sources = append(sources, datarights.Source{Name: pd.Module, PersonData: pd.PersonData})
	}
	if err := a.datarights.Link(sources); err != nil {
		return err //nolint:wrapcheck // Link names itself
	}
	return nil
}

// runExport is `bms export <person-id>` (people.md "The commands"): the document goes to
// stdout, the log to stderr. Exit 2: a malformed id or configuration; 3: a schema behind;
// 1: everything else, an unknown person and an Invalid from a module included. It works on a read-only copy (the audit
// entry goes to the spool).
func runExport(ctx context.Context, p *process) int {
	return runPersonCommand(ctx, p, "export", false, func(ctx context.Context, svc *datarights.Service, out io.Writer, id string) error {
		return svc.ExportCommand(ctx, out, id)
	})
}

// runDelete is `bms delete <person-id>`: the report goes to stdout. Exit codes as export;
// a read-only copy exits 1, and so does the last access admin (Conflict last_access_admin).
func runDelete(ctx context.Context, p *process) int {
	return runPersonCommand(ctx, p, "delete", true, func(ctx context.Context, svc *datarights.Service, out io.Writer, id string) error {
		return svc.DeleteCommand(ctx, out, id)
	})
}

// runPersonCommand builds the core and the modules (not started), links datarights, and runs
// fn as the operator. It loads configuration like serve and requires the session key.
func runPersonCommand(ctx context.Context, p *process, name string, writes bool, fn func(context.Context, *datarights.Service, io.Writer, string) error) int {
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
	ctx = audit.WithOperator(ctx) // the command acts with the operator's hands
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
	if a.datarights == nil {
		return bail(errs.E(op, errs.Internal, errors.New("the datarights part built no service")), ExitFailure)
	}
	cctx, cancel := context.WithTimeout(ctx, checkTimeout)
	err = fn(cctx, a.datarights, p.stdout, p.args[1])
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
