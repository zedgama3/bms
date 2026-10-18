package app

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// authzPart is core part 4: roles, grants, separation-of-duties rules and the decisions
// (authz.md; app.md "How later core services plug in", row 4). It is built after identity
// and audit, whose lazy seams it fills.
var authzPart = part{
	name:    "authz",
	declare: declareAuthz,
	build:   buildAuthz,
	seed:    seedAuthz,
}

// accessArea is the route set authz's routes are mounted under: /api/access. It is also
// the scope of authz's audit entries and signals.
const accessArea = "access"

// authzSignals are the signals authz publishes.
var authzSignals = []signals.Name{authz.SignalGrantCreated, authz.SignalGrantRevoked}

// declareAuthz adds the authz migration set, permissions and signals. No I/O.
func declareAuthz(a *assembly) error {
	if err := a.addCoreSet(authz.Set); err != nil {
		return err
	}
	a.corePerms = append(a.corePerms, authz.Permissions...)
	a.coreSignals = append(a.coreSignals, authzSignals...)
	return nil
}

// buildAuthz constructs the service (no I/O: it succeeds against db.None() and on a
// read-only copy), fills the seams of the shell, audit and identity, and mounts the access
// routes. The declared permissions are the core's and the enabled modules'.
func buildAuthz(_ context.Context, a *assembly) error {
	const op = "app.authz.build"
	if a.deps.audit == nil {
		return errs.E(op, errs.Internal, errors.New("authz is built before audit"))
	}
	rec, err := a.deps.audit.Scoped(accessArea)
	if err != nil {
		return fmt.Errorf("%s: audit scope: %w", op, err)
	}
	bus, err := a.disp.Scope(accessArea, authzSignals, nil)
	if err != nil {
		return fmt.Errorf("%s: signals scope: %w", op, err)
	}
	tr, m, err := a.scopes("authz")
	if err != nil {
		return err
	}
	svc, err := authz.New(authz.Options{
		DB:       a.dbc,
		Clock:    a.clock,
		Logger:   a.component("authz"),
		Tracer:   tr,
		Meter:    m,
		Audit:    rec,
		Signals:  bus,
		Actor:    authzActor,
		Declared: declaredPermissions(a),
	})
	if err != nil {
		return err //nolint:wrapcheck // authz.New names itself
	}
	a.authz = svc
	a.permissions = svc
	a.titles = svc
	a.lister = svc
	a.deps.authz = svc
	a.routeSets = append(a.routeSets, module.ModuleRoutes{
		Module:  accessArea,
		Routes:  accessRoutes(svc.Routes()),
		OpenAPI: svc.OpenAPI(),
	})
	return nil
}

// declaredPermissions is every permission of this installation: the core's, then the
// enabled modules' in build order.
func declaredPermissions(a *assembly) []authz.PermissionDecl {
	var fromModules []authz.PermissionDecl
	if a.plan != nil {
		fromModules = a.plan.Permissions()
	}
	return slices.Concat(a.corePerms, fromModules)
}

// accessRoutes converts authz's routes (authz cannot import core/module) to module routes:
// all signed-in, the permission being checked inside each use case.
func accessRoutes(in []authz.Route) []module.Route {
	out := make([]module.Route, len(in))
	for i, r := range in {
		out[i] = module.Route{Method: r.Method, Pattern: r.Pattern, Access: module.AccessSignedIn, Handler: r.Handler}
	}
	return out
}

// seedAuthz is authz's step of `bms migrate`: it applies the declarations' StarterRoles once
// and says how many permissions it added. It needs the service, so migrate builds the parts
// first.
func seedAuthz(ctx context.Context, a *assembly) error {
	const op = "app.authz.seed"
	if a.authz == nil {
		return errs.E(op, errs.Internal, errors.New("the authz part built no service"))
	}
	res, err := a.authz.Seed(ctx, declaredPermissions(a))
	if err != nil {
		return err //nolint:wrapcheck // Seed names itself
	}
	if res.Added > 0 {
		say(a.stdout, "%s: %d starter permissions seeded\n", accessArea, res.Added)
	}
	return nil
}
