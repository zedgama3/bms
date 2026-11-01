package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/regional"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// regionalPart is the regional's profile: the public document, theme.css and the logo
// (boundaries.md, "regional"). It is built after audit, the authz.Lookup, settings (its
// "regional" area, the revision and the terms) and files (its "settings" store), and before
// notify, whose site source reads the profile.
var regionalPart = part{
	name:  "regional",
	build: buildRegional,
}

// filesOwner is the files owner and bucket of the logo: "settings", as deployed logos have
// always had (boundaries.md decision 12), although regional now holds the record.
const filesOwner = settingsArea

// buildRegional constructs the service (no I/O: it succeeds against db.None() and on a
// read-only copy) and mounts the "regional" route set.
func buildRegional(_ context.Context, a *assembly) error {
	const op = "app.regional.build"
	if a.deps.audit == nil || a.deps.authz == nil || a.settings == nil || a.files == nil {
		return errs.E(op, errs.Internal, errors.New("regional is built after audit, authz, settings and files"))
	}
	rec, err := a.deps.audit.Scoped(regional.Area)
	if err != nil {
		return fmt.Errorf("%s: audit scope: %w", op, err)
	}
	reader, err := a.settings.Scoped(regional.Area)
	if err != nil {
		return fmt.Errorf("%s: settings scope: %w", op, err)
	}
	logo, err := a.files.Scoped(filesOwner)
	if err != nil {
		return fmt.Errorf("%s: files scope: %w", op, err)
	}
	tr, _, err := a.scopes(regional.Area)
	if err != nil {
		return err
	}
	svc, err := regional.New(regional.Options{
		DB:       a.dbc,
		Clock:    a.clock,
		Logger:   a.component(regional.Area),
		Tracer:   tr,
		Audit:    rec,
		Authz:    a.deps.authz,
		Actor:    identity.Actor,
		Settings: reader,
		Revision: a.settings,
		Files:    logo,
		Terms:    a.settings,
	})
	if err != nil {
		return err //nolint:wrapcheck // regional.New names itself
	}
	a.regional = svc
	return a.addCoreRoutes(regional.Area, svc.Routes(), svc.OpenAPI())
}
