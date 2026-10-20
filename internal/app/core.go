package app

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/departments"
	"github.com/zedgama3/bms/internal/core/events"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// coreDeps holds the Deps fields the core provides to every module beyond Logger, Tracer,
// Clock, Links, DB and Signals (module-contract "Deps fields by spec"). Each field is
// added here, and copied in provision, when its spec is Approved:
//
//	Jobs     jobs.Registrar    (scoped per module by provision)    jobs.md
//	Files    files.Store                                           files.md
//
// Audit (audit.md, M1) is held unscoped: provision scopes it per module with Scoped(m.Name),
// and so is Settings (settings.md, M4: the service, scoped to settings.Reader per module).
// Notify (notify.md, M4) is held as the service and scoped per module by provision.
// Authz, People, Departments and Events (M2, M3) are shared: provision copies them to every module's Deps as they are.
type coreDeps struct {
	audit       *audit.Log            // set by the audit part's build; nil when no part builds one (tests)
	settings    *settings.Service     // set by the settings part's build; nil when no part builds one
	authz       authz.Authorizer      // set by the authz part's build; nil when no part builds one
	people      people.Directory      // set by the people part's build; nil when no part builds one
	departments departments.Directory // set by the departments part's build; nil when no part builds one
	events      events.Directory      // set by the events part's build; nil when no part builds one
	notify      *notify.Service       // set by the notify part's build; provision scopes it per module; nil when no part builds one
}

// coreSetOrder is the order of the core's migration sets (db.md Proposed change 10),
// whatever the order of the parts that declare them: a later set may hold a foreign key to
// an earlier one.
var coreSetOrder = []string{"identity", "people", "authz", "departments", "events", "audit", "settings", "jobs", "notify", "files"}

// addCoreSet inserts s into a.coreSets at its place in coreSetOrder. It fails with
// errs.Internal for a set that is not in the order or is declared twice (F28: a programming
// error).
func (a *assembly) addCoreSet(s db.Set) error {
	const op = "app.addCoreSet"
	rank := slices.Index(coreSetOrder, s.Name)
	if rank < 0 {
		return errs.E(op, errs.Internal, fmt.Errorf("core migration set %q is not in the fixed order", s.Name))
	}
	at := len(a.coreSets)
	for i, have := range a.coreSets {
		switch r := slices.Index(coreSetOrder, have.Name); {
		case r == rank:
			return errs.E(op, errs.Internal, fmt.Errorf("core migration set %q is declared twice", s.Name))
		case r > rank:
			at = min(at, i)
		}
	}
	a.coreSets = slices.Insert(a.coreSets, at, s)
	return nil
}

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
	// seed runs in `bms migrate`, after the migrations, on the writing copy, over parts that
	// were built the way serve builds them: authz applies the declarations' StarterRoles.
	seed func(ctx context.Context, a *assembly) error
}

// runnable is a core component with background work, started after the modules and
// stopped before them. jobs is the first (M4).
type runnable struct {
	name  string
	start func(ctx context.Context) error
	stop  func(ctx context.Context) error
}

// coreParts are the core services that plug in, in dependency order (audit, identity,
// settings, authz, people, departments, events, jobs, notify, files; app.md "How later core
// services plug in"). Each later core spec adds one file core_<name>.go and one line here.
var coreParts = []part{auditPart, identityPart, settingsPart, authzPart, peoplePart, departmentsPart, eventsPart, notifyPart}

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

// seedParts runs every part's seed, in order.
func (a *assembly) seedParts(ctx context.Context) error {
	for _, pt := range a.parts {
		if pt.seed == nil {
			continue
		}
		if err := pt.seed(ctx, a); err != nil {
			return fmt.Errorf("core part %s: seed: %w", pt.name, err)
		}
	}
	return nil
}

// hasSeed reports whether any part has a seed step.
func (a *assembly) hasSeed() bool {
	return slices.ContainsFunc(a.parts, func(pt part) bool { return pt.seed != nil })
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
