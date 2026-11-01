package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/zedgama3/bms/internal/core/auditlog"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// auditlogPart reads and expires the audit log (boundaries.md, "auditlog"). It is built after
// audit (whose recorder it takes), the authz.Lookup, settings and jobs, and before every part
// that counts or searches the log (people).
var auditlogPart = part{
	name:    "auditlog",
	declare: declareAuditlog,
	build:   buildAuditlog,
	link:    linkAuditlog,
}

// declareAuditlog adds auditlog's permissions. No I/O.
func declareAuditlog(a *assembly) error {
	a.corePerms = append(a.corePerms, auditlog.Permissions...)
	return nil
}

// buildAuditlog constructs the service (no I/O: it succeeds against db.None() and on a
// read-only copy) and mounts the "audit" route set. Settings and jobs are scoped "audit".
func buildAuditlog(_ context.Context, a *assembly) error {
	const op = "app.auditlog.build"
	if a.deps.audit == nil || a.deps.authz == nil || a.settings == nil || a.jobs == nil {
		return errs.E(op, errs.Internal, errors.New("auditlog is built after audit, authz, settings and jobs"))
	}
	rec, err := a.deps.audit.Scoped(auditlog.Area)
	if err != nil {
		return fmt.Errorf("%s: audit scope: %w", op, err)
	}
	reader, err := a.settings.Scoped(auditlog.Area)
	if err != nil {
		return fmt.Errorf("%s: settings scope: %w", op, err)
	}
	reg, err := a.jobs.Scoped(auditlog.Area)
	if err != nil {
		return fmt.Errorf("%s: jobs scope: %w", op, err)
	}
	tr, _, err := a.scopes(auditlog.Area)
	if err != nil {
		return err
	}
	svc, err := auditlog.New(auditlog.Options{
		DB:       a.dbc,
		Clock:    a.clock,
		Logger:   a.component(auditlog.Area),
		Tracer:   tr,
		Authz:    a.deps.authz,
		Actor:    identity.Actor,
		Settings: reader,
		Jobs:     reg,
		Audit:    rec,
	})
	if err != nil {
		return err //nolint:wrapcheck // auditlog.New names itself
	}
	a.auditlog = svc
	return a.addCoreRoutes(auditlog.Area, svc.Routes(), svc.OpenAPI())
}

// linkAuditlog registers audit.purge with the jobs runner (Start never writes). It runs
// after the modules were built, before the jobs runner starts, as every registration does.
func linkAuditlog(ctx context.Context, a *assembly) error {
	const op = "app.auditlog.link"
	if a.auditlog == nil {
		return errs.E(op, errs.Internal, errors.New("auditlog is linked after it is built"))
	}
	if err := a.auditlog.Start(ctx); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}
