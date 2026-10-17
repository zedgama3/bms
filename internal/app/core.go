package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/zedgama3/bms/internal/lib/errs"
)

// coreDeps holds the Deps fields the core provides to every module beyond Logger, Tracer,
// Clock, Links, DB and Signals (module-contract "Deps fields by spec"). Each field is
// added here, and copied in provision, when its spec is Approved:
//
//	Settings settings.Reader   (scoped per module by provision)   settings.md
//	Authz    authz.Authorizer                                      authz.md
//	People   people.Directory                                      people.md
//	Audit    audit.Recorder                                        audit.md
//	Jobs     jobs.Registrar    (scoped per module by provision)    jobs.md
//	Notify   notify.Notifier   (scoped per module by provision)    notify.md
//	Files    files.Store                                           files.md
type coreDeps struct{} //nolint:unused // the first part with a Deps field adds it

// part is how one core service plugs in. Every func is optional (nil means nothing to
// do). The order of coreParts is fixed (app.md "How later core services plug in") and a
// part never reads another part's output except through the assembly fields.
type part struct {
	name string // a reserved core area name (module-contract "Reserved module names"); unique
	// declare runs for every command that resolves the registry, before Resolve, with no
	// I/O: it appends to coreSignals, corePoints, coreSets, corePerms and commands.
	declare func(a *assembly) error
	// build runs after Resolve and before module Build, in serve, check and openapi: it
	// constructs the service from a.cfg, a.dbc, a.disp (a scope for its own signals),
	// a.plan (Permissions, Settings) and a.tel, and sets deps, authenticator, permissions,
	// routeSets and runnables. It does no I/O beyond reads and succeeds against db.None()
	// (decision 7).
	build func(ctx context.Context, a *assembly) error
	// link runs after module Build, before api.New, in serve and openapi: it receives
	// a.set (people: PersonData; definers: contributions) and may add route sets.
	link func(ctx context.Context, a *assembly) error
}

// runnable is a core component with background work, started after the modules and
// stopped before them. jobs is the first (M4).
type runnable struct {
	name  string
	start func(ctx context.Context) error
	stop  func(ctx context.Context) error
}

// coreParts are the core services that plug in, in dependency order (identity, audit,
// settings, authz, people, departments-events, jobs, notify, files). M0 has none: each
// later core spec adds one file core_<name>.go and one line here.
var coreParts = []part{}

// declare runs every part's declare, in order (F28).
func (a *assembly) declare() error {
	for _, pt := range a.parts {
		if pt.declare == nil {
			continue
		}
		if err := pt.declare(a); err != nil {
			return fmt.Errorf("core part %s: declare: %w", pt.name, err)
		}
	}
	return nil
}

// buildParts runs every part's build, in order.
func (a *assembly) buildParts(ctx context.Context) error {
	for _, pt := range a.parts {
		if pt.build == nil {
			continue
		}
		if err := pt.build(ctx, a); err != nil {
			return fmt.Errorf("core part %s: build: %w", pt.name, err)
		}
	}
	return nil
}

// linkParts runs every part's link, in order.
func (a *assembly) linkParts(ctx context.Context) error {
	for _, pt := range a.parts {
		if pt.link == nil {
			continue
		}
		if err := pt.link(ctx, a); err != nil {
			return fmt.Errorf("core part %s: link: %w", pt.name, err)
		}
	}
	return nil
}

// startRunnables starts each runnable in order and records it in a.started. On a failure it
// stops nothing itself: it returns the failure (F11) and the caller's closers stop the
// runnables already started, in reverse, then the modules, the database and telemetry.
func (a *assembly) startRunnables(ctx context.Context) error {
	for _, r := range a.runnables {
		if r.start != nil {
			if err := r.start(ctx); err != nil {
				return errs.E("app.startRunnables", errs.KindOf(err), fmt.Errorf("runnable %s: start: %w", r.name, err))
			}
		}
		a.started = append(a.started, r)
	}
	return nil
}

// stopRunnables stops the started runnables in reverse order, continuing past failures,
// and returns them joined.
func (a *assembly) stopRunnables(ctx context.Context) error {
	var failed []error
	for i := len(a.started) - 1; i >= 0; i-- {
		r := a.started[i]
		if r.stop == nil {
			continue
		}
		if err := r.stop(ctx); err != nil {
			failed = append(failed, fmt.Errorf("runnable %s: stop: %w", r.name, err))
		}
	}
	a.started = nil
	return errors.Join(failed...)
}
