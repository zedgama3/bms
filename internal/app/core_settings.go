package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/zedgama3/bms/internal/core/audit/auditsettings"
	"github.com/zedgama3/bms/internal/core/files/filesettings"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// settingsPart is core part 3: the typed, validated, audited values a regional configures
// while bms runs (settings.md; app.md "How later core services plug in", row 3). It is
// built after audit, whose recorder it takes, and before authz, so its Authz is a lazy
// adapter (app.md amendment 2026-10-19).
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
	auditSettings    = auditsettings.Area
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
// settings routes and registers the refresher as a runnable. The PolicySource adapters of
// identity and people resolve the service when called, because identity is built before
// this part.
func buildSettings(_ context.Context, a *assembly) error {
	const op = "app.settings.build"
	if a.deps.audit == nil {
		return errs.E(op, errs.Internal, errors.New("settings is built after audit"))
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
		Authz:      lazyAuthorizer{a: a},
		Actor:      authzActor,
		Files:      lazyFilesStore{a: a},
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
	r := &refresher{svc: svc, logger: a.component("settings")}
	a.runnables = append(a.runnables, runnable{name: "settings", start: r.start, stop: r.stop})
	return nil
}

// settingsAreas is every area but "regional": the core's, then one per enabled module that
// declares a setting or a term (D57), in build order, carrying its terms. A module that
// declares neither has no area, so its screen list stays empty of it (provision gives a
// module a Reader only when it declares a setting: a terms-only module has no key to read).
func settingsAreas(a *assembly) []settings.Area {
	areas := []settings.Area{
		{Name: accountsSettings, Core: true, Decls: identity.Settings, Check: identity.CheckSettings},
		{Name: peopleSettings, Core: true, Decls: people.Settings},
		{Name: auditSettings, Core: true, Decls: auditsettings.Settings},
		{Name: notifySettings, Core: true, Decls: notify.Settings},
		{Name: filesSettings, Core: true, Decls: filesettings.Settings},
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

// refresher runs settings.Service.Run in a goroutine of its own: start launches it, stop
// cancels it and waits for it, within the ctx it is given.
type refresher struct {
	svc    *settings.Service
	logger *slog.Logger

	cancel context.CancelFunc
	done   chan struct{}
}

// start launches Run. The goroutine exits when stop ends it; a failure, if any (Run fails
// only when started twice), is logged there, once.
func (r *refresher) start(ctx context.Context) error {
	// Run outlives start's own deadline: it stops on stop.
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	r.cancel = cancel
	r.done = make(chan struct{})
	go func() {
		defer close(r.done)
		if err := r.svc.Run(runCtx); err != nil {
			r.logger.ErrorContext(runCtx, "the settings refresher stopped",
				slog.String("error.kind", errs.KindOf(err).String()), slog.Any("error", err))
		}
	}()
	return nil
}

// stop ends Run and waits for it. It fails errs.Timeout or errs.Canceled when ctx ends
// first; the goroutine then exits on its own when Run notices its canceled context.
func (r *refresher) stop(ctx context.Context) error {
	const op = "app.settings.refresher.stop"
	if r.cancel == nil {
		return nil
	}
	r.cancel()
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return errs.E(op, errs.KindOf(ctx.Err()), fmt.Errorf("the settings refresher did not stop: %w", ctx.Err()))
	}
}

// settingsPolicy is identity's PolicySource over the "accounts" area's scoped Reader: it
// maps the accounts.* keys onto Policy, one read each. Identity validates what it returns.
type settingsPolicy struct{ r settings.Reader }

// Policy reads the eleven keys. It fails with the Reader's kinds.
func (p settingsPolicy) Policy(ctx context.Context) (identity.Policy, error) {
	const op = "app.settingsPolicy.Policy"
	var pol identity.Policy
	mode, err := p.r.Text(ctx, "signup")
	if err != nil {
		return identity.Policy{}, fmt.Errorf("%s: accounts.signup: %w", op, err)
	}
	pol.SignupMode = identity.SignupMode(mode)
	for _, f := range []struct {
		key string
		dst *time.Duration
	}{
		{"session_lifetime", &pol.SessionLifetime},
		{"superuser_session_lifetime", &pol.SuperuserSessionLifetime},
		{"login_failure_window", &pol.LoginFailureWindow},
		{"password_reset_lifetime", &pol.PasswordResetLifetime},
		{"password_reset_cooldown", &pol.PasswordResetCooldown},
	} {
		if *f.dst, err = p.r.Duration(ctx, f.key); err != nil {
			return identity.Policy{}, fmt.Errorf("%s: accounts.%s: %w", op, f.key, err)
		}
	}
	for _, f := range []struct {
		key string
		dst *int
	}{
		{"password_min_length", &pol.PasswordMinLength},
		{"login_failure_limit", &pol.LoginFailureLimit},
		{"client_failure_limit", &pol.ClientFailureLimit},
		{"password_reset_client_limit", &pol.PasswordResetClientLimit},
		{"password_reset_address_limit", &pol.PasswordResetAddressLimit},
	} {
		n, err := p.r.Int(ctx, f.key)
		if err != nil {
			return identity.Policy{}, fmt.Errorf("%s: accounts.%s: %w", op, f.key, err)
		}
		*f.dst = int(n)
	}
	return pol, nil
}

// peoplePolicy is people's PolicySource over the "people" area's scoped Reader.
type peoplePolicy struct{ r settings.Reader }

// Policy reads the three field modes. It fails with the Reader's kinds, and errs.Invalid for
// a mode people does not know.
func (p peoplePolicy) Policy(ctx context.Context) (people.Policy, error) {
	const op = "app.peoplePolicy.Policy"
	var pol people.Policy
	for _, f := range []struct {
		key string
		dst *people.FieldMode
	}{
		{"legal_name", &pol.LegalName},
		{"phone", &pol.Phone},
		{"emergency_contacts", &pol.EmergencyContacts},
	} {
		text, err := p.r.Text(ctx, f.key)
		if err != nil {
			return people.Policy{}, fmt.Errorf("%s: people.%s: %w", op, f.key, err)
		}
		if *f.dst, err = people.ParseFieldMode(text); err != nil {
			return people.Policy{}, fmt.Errorf("%s: people.%s: %w", op, f.key, err)
		}
	}
	return pol, nil
}
