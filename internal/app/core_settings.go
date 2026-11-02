package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/zedgama3/bms/internal/core/auditlog"
	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/core/regional"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// settingsPart is the settings core part: the typed, validated, audited values a regional configures
// while bms runs (settings.md; app.md "The build order"). It is
// built after audit, whose recorder it takes, and after the authz.Lookup, which it takes as
// its Authz.
var settingsPart = part{
	name:    "settings",
	declare: declareSettings,
	build:   buildSettings,
}

// settingsArea is the route set and audit scope of settings: /api/settings.
const settingsArea = "settings"

// The core settings areas whose declarations other core packages export.
const (
	accountsSettings = accountsArea // identity's keys, accounts.*
	peopleSettings   = peopleArea   // people's keys, people.*
	auditSettings    = auditlog.Area
)

// declareSettings adds the settings migration set and permissions. No I/O.
func declareSettings(a *assembly) error {
	if err := a.addCoreSet(settings.Set); err != nil {
		return err
	}
	a.corePerms = append(a.corePerms, settings.Permissions...)
	return nil
}

// buildSettings constructs the service (no I/O: it succeeds against db.None() and on a
// read-only copy) over the core areas and the enabled modules' declarations, mounts the
// settings routes and registers the refresher as a runnable. Identity, people and notify,
// built after it, read their own areas through Readers scoped from the service.
func buildSettings(_ context.Context, a *assembly) error {
	const op = "app.settings.build"
	if a.deps.audit == nil || a.authzLookup == nil {
		return errs.E(op, errs.Internal, errors.New("settings is built after audit and the authz lookup"))
	}
	rec, err := a.deps.audit.Scoped(settingsArea)
	if err != nil {
		return fmt.Errorf("%s: audit scope: %w", op, err)
	}
	tr, m, err := a.scopes("settings")
	if err != nil {
		return err
	}
	svc, err := settings.New(settings.Options{
		DB:         a.dbc,
		Clock:      a.clock,
		Logger:     a.component("settings"),
		Tracer:     tr,
		Meter:      m,
		Audit:      rec,
		Authz:      a.authzLookup,
		Actor:      identity.Actor,
		Areas:      settingsAreas(a),
		SealingKey: a.cfg.Settings.SealingKey,
	})
	if err != nil {
		return err //nolint:wrapcheck // settings.New names itself
	}
	a.settings = svc
	a.deps.settings = svc
	if err := a.addCoreRoutes(settingsArea, svc.Routes(), svc.OpenAPI()); err != nil {
		return err
	}
	rn, err := newRunner("settings refresher", svc.Run, nil, a.component("settings"))
	if err != nil {
		return err
	}
	a.runnables = append(a.runnables, rn.runnable("settings"))
	return nil
}

// settingsAreas is every area: the core's (regional first, the area its package declares), then one per enabled module that
// declares a setting or a term (D57), in build order, carrying its terms. A module that
// declares neither has no area, so its screen list stays empty of it (provision gives a
// module a Reader only when it declares a setting: a terms-only module has no key to read).
func settingsAreas(a *assembly) []settings.Area {
	areas := []settings.Area{
		{Name: regional.Area, Core: true, Decls: regional.Settings},
		{Name: accountsSettings, Core: true, Decls: identity.Settings, Check: identity.CheckSettings},
		{Name: peopleSettings, Core: true, Decls: people.Settings},
		{Name: auditSettings, Core: true, Decls: auditlog.Settings},
		{Name: notifySettings, Core: true, Decls: notify.Settings},
		{Name: filesSettings, Core: true, Decls: files.Settings},
	}
	if a.plan == nil {
		return areas
	}
	for _, ms := range a.plan.Settings() {
		if len(ms.Settings) == 0 && len(ms.Terms) == 0 {
			continue
		}
		areas = append(areas, settings.Area{Name: ms.Module, Decls: ms.Settings, Terms: ms.Terms})
	}
	return areas
}
