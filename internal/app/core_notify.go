package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/zedgama3/bms/internal/core/datarights"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// notifyPart is core part 9: the e-mail outbox, the SMTP channel and the templates every
// core area and module sends through (notify.md; app.md "The build order"). It is built
// after settings, regional and authz, and takes the identity and people Lookups (built by
// buildLookups) to find an address, a language and a name; identity and datarights are built
// after it and scope their own Notifier from it.
var notifyPart = part{
	name:    "notify",
	declare: declareNotify,
	build:   buildNotify,
	link:    linkNotify,
}

// notifyArea is the route set, audit scope and settings area of notify: /api/notify.
const notifyArea = "notify"

// notifySettings is the settings area of notify's keys, notify.*.
const notifySettings = notifyArea

// declareNotify adds the notify migration set and permissions. No I/O.
func declareNotify(a *assembly) error {
	if err := a.addCoreSet(notify.Set); err != nil {
		return err
	}
	a.corePerms = append(a.corePerms, notify.Permissions...)
	return nil
}

// accountsOwner is the owner identity's templates and messages carry: "identity" (the
// settings and route area of identity is "accounts").
const accountsOwner = "identity"

// buildNotify constructs the service (no I/O: it succeeds against db.None() and on a
// read-only copy), registers the core areas' templates, mounts the routes and registers the sender loop as a runnable.
func buildNotify(_ context.Context, a *assembly) error {
	const op = "app.notify.build"
	if a.deps.audit == nil || a.deps.authz == nil || a.settings == nil || a.regional == nil || a.identityLookup == nil || a.peopleLookup == nil {
		return errs.E(op, errs.Internal, errors.New("notify is built after audit, settings, regional, authz and the identity and people lookups"))
	}
	rec, err := a.deps.audit.Scoped(notifyArea)
	if err != nil {
		return fmt.Errorf("%s: audit scope: %w", op, err)
	}
	reader, err := a.settings.Scoped(notifySettings)
	if err != nil {
		return fmt.Errorf("%s: settings scope: %w", op, err)
	}
	tr, m, err := a.scopes("notify")
	if err != nil {
		return err
	}
	capture, err := demoCapture(a) // nil unless demo mode: notify then sends over SMTP
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	svc, err := notify.New(notify.Options{
		DB:       a.dbc,
		Clock:    a.clock,
		Logger:   a.component("notify"),
		Tracer:   tr,
		Meter:    m,
		Audit:    rec,
		Authz:    a.deps.authz,
		Actor:    identity.Actor,
		Settings: reader,
		Regional: a.regional,
		Terms:    a.settings,
		Accounts: a.identityLookup,
		Profiles: a.peopleLookup,
		Hostname: a.cfg.Instance.Name,
		Channel:  capture,
	})
	if err != nil {
		return err //nolint:wrapcheck // notify.New names itself
	}
	for _, t := range []struct {
		owner string
		files fs.FS
	}{{accountsOwner, identity.Templates}, {datarights.Area, datarights.Templates}} {
		if err := svc.Register(t.owner, t.files); err != nil {
			return fmt.Errorf("%s: templates of %s: %w", op, t.owner, err)
		}
	}
	a.notify = svc
	a.deps.notify = svc
	if err := a.addCoreRoutes(notifyArea, svc.Routes(), svc.OpenAPI()); err != nil {
		return err
	}
	rn, err := newRunner("notify sender", svc.Run, svc.Close, a.component("notify"))
	if err != nil {
		return err
	}
	a.runnables = append(a.runnables, rn.runnable("notify"))
	return nil
}

// linkNotify registers the template defaults of every built module that ships any. It runs
// after module Build, so a module's templates are registered before Run starts; a bad
// template fails the build with errs.Invalid naming the file. The outbox is datarights'
// notify source (Options.Outbox), passed as the service itself.
func linkNotify(_ context.Context, a *assembly) error {
	const op = "app.notify.link"
	if a.notify == nil || a.plan == nil {
		return errs.E(op, errs.Internal, errors.New("notify is linked after it is built and the modules are resolved"))
	}
	for _, m := range a.plan.Order() {
		if m.Templates == nil {
			continue
		}
		if err := a.notify.Register(m.Name, m.Templates); err != nil {
			return fmt.Errorf("%s: templates of module %s: %w", op, m.Name, err)
		}
	}
	return nil
}
