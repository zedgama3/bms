package app

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// authzPart is the authz core part: roles, grants, separation-of-duties rules and the admin service
// (authz.md; boundaries.md "The build order", item 5). It is built after audit, whose
// recorder it takes, and before identity and settings, which take the authz.Lookup built
// earlier; the decisions are the Lookup's.
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
// read-only copy) on top of the authz.Lookup, and mounts the access routes. The permission
// catalogue, the db, the clock and the actor are the Lookup's; NewLookup validated them.
func buildAuthz(_ context.Context, a *assembly) error {
	const op = "app.authz.build"
	if a.deps.audit == nil || a.authzLookup == nil {
		return errs.E(op, errs.Internal, errors.New("authz is built after audit and the authz lookup"))
	}
	rec, err := a.deps.audit.Scoped(accessArea)
	if err != nil {
		return fmt.Errorf("%s: audit scope: %w", op, err)
	}
	bus, err := a.disp.Scope(accessArea, authzSignals, nil)
	if err != nil {
		return fmt.Errorf("%s: signals scope: %w", op, err)
	}
	tr, _, err := a.scopes("authz")
	if err != nil {
		return err
	}
	svc, err := authz.New(authz.Options{
		Logger:  a.component("authz"),
		Tracer:  tr,
		Audit:   rec,
		Signals: bus,
		Lookup:  a.authzLookup,
	})
	if err != nil {
		return err //nolint:wrapcheck // authz.New names itself
	}
	a.authz = svc
	if err := a.addCoreRoutes(accessArea, svc.Routes(), svc.OpenAPI()); err != nil {
		return err
	}
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
