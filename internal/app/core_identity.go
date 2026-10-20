package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// identityPart is core part 2: local accounts, sessions and the break-glass superuser. It
// is built after audit, whose recorder it takes (audit.md Proposed change 1).
var identityPart = part{
	name:    "identity",
	declare: declareIdentity,
	build:   buildIdentity,
}

// accountsArea is the route set identity's routes are mounted under: /api/accounts.
const accountsArea = "accounts"

// declareIdentity adds the identity migration set, permissions, signal and the
// create-superuser command. No I/O.
func declareIdentity(a *assembly) error {
	if err := a.addCoreSet(identity.Set); err != nil {
		return err
	}
	a.corePerms = append(a.corePerms, identity.Permissions...)
	a.coreSignals = append(a.coreSignals, identity.SignalAccountCreated)
	a.commands = append(a.commands, command{
		name:    "create-superuser",
		summary: "create or reset the superuser account; its password is shown once",
		usage:   "<email>",
		run:     runCreateSuperuser,
	})
	return nil
}

// buildIdentity constructs the service (no I/O: it succeeds against db.None() and on a
// read-only copy), hands the shell its Authenticator and mounts the accounts routes. The
// permission checker, the permission lister and the name source are the lazy adapters,
// because settings, authz and people are built after identity (app.md, rows 3 to 5).
func buildIdentity(_ context.Context, a *assembly) error {
	const op = "app.identity.build"
	if a.deps.audit == nil {
		return errs.E(op, errs.Internal, errors.New("identity is built before audit"))
	}
	rec, err := a.deps.audit.Scoped("identity")
	if err != nil {
		return fmt.Errorf("%s: audit scope: %w", op, err)
	}
	bus, err := a.disp.Scope("identity", []signals.Name{identity.SignalAccountCreated}, nil)
	if err != nil {
		return fmt.Errorf("%s: signals scope: %w", op, err)
	}
	tr, m, err := a.scopes("identity")
	if err != nil {
		return err
	}
	svc, err := identity.New(identity.Options{
		DB:             a.dbc,
		Clock:          a.clock,
		Logger:         a.component("identity"),
		Tracer:         tr,
		Meter:          m,
		Key:            a.cfg.Session.Key,
		AllowPlaintext: a.cfg.HTTP.AllowPlaintext,
		Audit:          rec,
		Signals:        bus,
		Permissions:    lazyPermissions{a: a},
		Lister:         lazyLister{a: a},
		Names:          lazyNames{a: a},
		Policy:         lazyIdentityPolicy{a: a},
	})
	if err != nil {
		return err //nolint:wrapcheck // identity.New names itself
	}
	a.identity = svc
	a.authenticator = svc
	a.routeSets = append(a.routeSets, module.ModuleRoutes{
		Module:  accountsArea,
		Routes:  accountRoutes(svc.Routes()),
		OpenAPI: svc.OpenAPI(),
	})
	return nil
}

// accountRoutes converts identity's routes (identity cannot import core/module) to module
// routes: a public route needs no session, every other one does.
func accountRoutes(in []identity.Route) []module.Route {
	out := make([]module.Route, len(in))
	for i, r := range in {
		access := module.AccessSignedIn
		if r.Public {
			access = module.AccessPublic
		}
		out[i] = module.Route{Method: r.Method, Pattern: r.Pattern, Access: access, Handler: r.Handler}
	}
	return out
}

// useAnotherAddress is what the operator reads when a participant account holds the
// address given to create-superuser (identity decision 7). It never repeats the address.
const useAnotherAddress = "use another address: a participant account already holds that one, and a participant account is never promoted to superuser"

// runCreateSuperuser is `bms create-superuser <email>` (identity.md "The command"). It
// loads configuration like serve and requires the session key (F6), resolves the registry,
// builds the parts over the real database (a core schema behind exits 3), refuses a
// read-only copy (exit 1), and calls
// identity's CreateSuperuser, which prints the result and the password once. Exit 2: a
// missing or invalid argument or a participant address (Invalid, Conflict); exit 1:
// everything else. The address is never printed or logged.
func runCreateSuperuser(ctx context.Context, p *process) int {
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

	// Like every other command: the enabled modules and the registry are resolved first
	// (which runs the parts' declare), so a part's build always sees a.plan. A typo in
	// BMS_MODULES_DISABLED therefore stops this command with exit 2 too.
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
	if a.pool.ReadOnly() {
		return bail(errs.E("app.create-superuser", errs.Unavailable, errReadOnlyCopy), ExitFailure)
	}
	if err := a.newDispatcher(); err != nil {
		return bail(err, ExitFailure)
	}
	bctx, cancel := context.WithTimeout(ctx, buildTimeout)
	err = a.checkSchemas(bctx, a.migrator, nil)
	if err == nil {
		err = a.buildParts(bctx)
	}
	cancel()
	if err != nil {
		return bail(err, statusOf(err, ExitSchema))
	}
	if a.identity == nil {
		return bail(errs.E("app.create-superuser", errs.Internal, errors.New("the identity part built no service")), ExitFailure)
	}

	cctx, cancel := context.WithTimeout(ctx, checkTimeout)
	err = a.identity.CreateSuperuser(cctx, p.stdout, p.args[1])
	cancel()
	if err != nil {
		status := ExitFailure
		if errs.Is(err, errs.Invalid) || errs.Is(err, errs.Conflict) {
			status = ExitUsage
		}
		code := bail(err, status)
		if errs.Is(err, errs.Conflict) {
			sayln(p.stderr, useAnotherAddress)
		}
		return code
	}
	if err := a.closeQuiet(ctx); err != nil {
		p.problem(err)
		return ExitFailure
	}
	return ExitOK
}

// errReadOnlyCopy is the cause of create-superuser on a read-only copy.
var errReadOnlyCopy = errors.New("this copy is read-only; run it against the writing copy")
