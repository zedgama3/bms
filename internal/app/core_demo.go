package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/zedgama3/bms/internal/contracts/demoseed"
	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/demo"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// demoPart is core part 11: the public demo's seed, reset, guard, mail capture and upload
// cap (demo.md, D53; app.md row 11). It is built after files. With cfg.Demo.Enabled false it
// declares its tables and its command and wires nothing else: a production install never
// sees it.
var demoPart = part{
	name:    "demo",
	declare: declareDemo,
	build:   buildDemo,
	link:    linkDemo,
}

// demoArea is the route set, audit scope and telemetry scope of demo: /api/demo.
const demoArea = "demo"

// declareDemo adds the demo migration set, the demo.seed extension point and the `demo`
// command. No I/O. The part owns no permission.
func declareDemo(a *assembly) error {
	if err := a.addCoreSet(demo.Set); err != nil {
		return err
	}
	a.corePoints = append(a.corePoints, module.ExtensionPointDecl{
		Name: demoseed.Point,
		Accepts: func(v any) bool {
			_, ok := v.(demoseed.Provider)
			return ok
		},
	})
	a.commands = append(a.commands, command{
		name:    "demo",
		summary: "seed the fictional demo regional, or reset it (demo seed|reset)",
		usage:   "seed|reset",
		run:     runDemo,
	})
	return nil
}

// buildDemo constructs the service (no I/O: it succeeds against db.None() and on a read-only
// copy) over the real services, and mounts its route set. It is built in every process.
func buildDemo(_ context.Context, a *assembly) error {
	const op = "app.demo.build"
	if a.deps.audit == nil || a.authz == nil || a.settings == nil || a.identity == nil || a.people == nil ||
		a.departments == nil || a.events == nil || a.files == nil {
		return errs.E(op, errs.Internal, errors.New("demo is built after audit, identity, settings, authz, people, departments, events and files"))
	}
	rec, err := a.deps.audit.Scoped(demoArea)
	if err != nil {
		return fmt.Errorf("%s: audit scope: %w", op, err)
	}
	tr, m, err := a.scopes(demoArea)
	if err != nil {
		return err
	}
	svc, err := demo.New(demo.Options{
		Enabled:            a.cfg.Demo.Enabled,
		PublicURL:          a.cfg.Demo.PublicURL,
		Instance:           a.cfg.Instance.Name,
		DB:                 a.dbc,
		Clock:              a.clock,
		Logger:             a.component(demoArea),
		Tracer:             tr,
		Meter:              m,
		Audit:              rec,
		Actor:              identity.Actor,
		Accounts:           a.identity,
		Access:             a.authz,
		Addresses:          a.identity,
		Profiles:           a.people,
		Departments:        a.departments,
		Events:             a.events,
		Settings:           a.settings,
		Schema:             demoSchema{migrator: a.migrator, sets: demoSets(a), seeder: a.authz, declared: declaredPermissions(a)},
		Objects:            a.files,
		Schemas:            demoSchemas(a),
		DataDir:            a.cfg.Data.Dir,
		ContributorTimeout: demo.DefaultContributorTimeout,
	})
	if err != nil {
		return err //nolint:wrapcheck // demo.New names itself
	}
	a.demo = svc
	if a.cfg.Demo.Enabled {
		// The route set exists only in demo mode: on any other install /api/demo is a 404, the
		// web reads that as "not a demo", and `bms openapi` (demo off) does not document it.
		if err := a.addCoreRoutes(demoArea, svc.Routes(), svc.OpenAPI()); err != nil {
			return err
		}
	}
	return nil
}

// demoSets is every migration set the plan runs: the core's and each enabled module's, in
// set order.
func demoSets(a *assembly) []db.Set {
	var manifests []module.Manifest
	if a.plan != nil { // a test builds the parts without resolving the modules
		manifests = a.plan.Order()
	}
	return a.setsFor(manifests)
}

// demoSchemas is the schema of every migration set, deduplicated, in set order: "core" and
// each enabled module with migrations.
func demoSchemas(a *assembly) []string {
	var out []string
	for _, s := range demoSets(a) {
		name := s.Schema
		if name == "" {
			name = s.Name
		}
		if !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	if len(out) == 0 {
		out = []string{"core"} // a test builds the parts without declaring any set
	}
	return out
}

// linkDemo gives the service every module's contribution to demo.seed, in build order. It
// runs after module Build and before api.New.
func linkDemo(_ context.Context, a *assembly) error {
	const op = "app.demo.link"
	if a.demo == nil || a.set == nil {
		return errs.E(op, errs.Internal, errors.New("demo is linked after it and the modules are built"))
	}
	contribs, err := a.set.Contributors(demoseed.Point)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	var out []demo.Contributor
	for _, c := range contribs {
		prov, ok := c.Value.(demoseed.Provider)
		if !ok {
			return errs.E(op, errs.Internal, fmt.Errorf("module %q contributed a %T to %s, which is not a demoseed.Provider", c.Module, c.Value, demoseed.Point))
		}
		out = append(out, demo.Contributor{Module: c.Module, Provider: prov})
	}
	return a.demo.Link(out) //nolint:wrapcheck // Link names itself
}

// ---- what demo mode wires into the parts built before it ------------------------------------

// demoGuard is identity's Guard in demo mode and nil otherwise (build order row 13). It reads
// core.demo_account alone, so identity needs nothing built after it.
func demoGuard(a *assembly) (identity.Guard, error) {
	if !a.cfg.Demo.Enabled {
		return nil, nil
	}
	tr, _, err := a.scopes(demoArea)
	if err != nil {
		return nil, err
	}
	g, err := demo.NewGuard(demo.GuardOptions{DB: a.dbc, Tracer: tr})
	if err != nil {
		return nil, err //nolint:wrapcheck // NewGuard names itself
	}
	return g, nil
}

// demoCapture is notify's Channel in demo mode and nil otherwise (build order row 14), which
// leaves notify its SMTP channel. It writes core.demo_mail alone.
func demoCapture(a *assembly) (notify.Channel, error) {
	if !a.cfg.Demo.Enabled {
		return nil, nil
	}
	tr, m, err := a.scopes(demoArea)
	if err != nil {
		return nil, err
	}
	c, err := demo.NewCapture(demo.CaptureOptions{DB: a.dbc, Clock: a.clock, Logger: a.component(demoArea), Tracer: tr, Meter: m})
	if err != nil {
		return nil, err //nolint:wrapcheck // NewCapture names itself
	}
	return c, nil
}

// demoSchema is the seed's migrate step: the migrator over every set, then authz's
// starter-role seed, the same code path `bms migrate` runs. It holds its inputs, not the
// assembly.
type demoSchema struct {
	migrator *db.Migrator // nil in openapi: a reset needs a real database
	sets     []db.Set     // the plan's sets, in order
	seeder   *authz.Service
	declared []authz.PermissionDecl
}

func (d demoSchema) Migrate(ctx context.Context) error {
	if d.migrator == nil || d.seeder == nil {
		return errs.E("app.demoSchema.Migrate", errs.Internal, errors.New("no migrator: demo reset runs against a real database"))
	}
	if _, err := d.migrator.Migrate(ctx, d.sets); err != nil {
		return err //nolint:wrapcheck // the reset names the step
	}
	if _, err := d.seeder.Seed(ctx, d.declared); err != nil {
		return err //nolint:wrapcheck // the reset names the step
	}
	return nil
}

// ---- the command ------------------------------------------------------------------------------

// demoTimeout bounds one `bms demo` run: the seed's own budget and a reset's drop and
// migration.
const demoTimeout = 2 * time.Hour

// runDemo is `bms demo seed|reset` (demo.md). It loads configuration like serve, requires
// the session key, builds the core as `bms export` does (modules built and started, nothing
// served) and runs Seed or Reset as the operator. Exit 2: demo mode off, the database
// already seeded or not empty, a reset of a database that is not a demo, a bad argument;
// exit 3: the migrator found the schema behind or ahead; else 1.
func runDemo(ctx context.Context, p *process) int {
	if len(p.args) != 2 || (p.args[1] != "seed" && p.args[1] != "reset") {
		sayln(p.stderr, "usage: bms demo seed|reset")
		return ExitUsage
	}
	cfg, err := p.loadConfig(ctx)
	if err != nil {
		return p.earlyFail(ctx, err, ExitUsage)
	}
	p.warnDebugToStderr(cfg)
	if err := requireSessionKey(cfg); err != nil {
		p.problem(err)
		return ExitUsage
	}
	if !cfg.Demo.Enabled {
		// Before the database is opened: a non-demo installation is never touched.
		sayln(p.stderr, "bms demo: demo mode is off; set BMS_DEMO_ENABLED=true to use this command")
		return ExitUsage
	}
	a := newAssembly(p, cfg)
	ctx = audit.WithOperator(ctx) // the command acts with the operator's hands
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
	if err := a.newDispatcher(); err != nil {
		return bail(err, ExitFailure)
	}
	if err := a.buildModules(ctx, a.migrator); err != nil {
		return bail(err, statusOf(err, ExitSchema))
	}
	if a.demo == nil {
		return bail(errs.E("app.demo", errs.Internal, errors.New("the demo part built no service")), ExitFailure)
	}
	sctx, cancel := context.WithTimeout(ctx, startTimeout)
	err = a.set.Start(sctx)
	cancel()
	if err != nil {
		return bail(err, ExitFailure)
	}
	rctx, cancel := context.WithTimeout(ctx, demoTimeout)
	if p.args[1] == "seed" {
		_, err = a.demo.Seed(rctx, p.stdout)
	} else {
		_, err = a.demo.Reset(rctx, p.stdout)
	}
	cancel()
	if err != nil {
		return bail(err, demoStatus(err))
	}
	if err := a.closeQuiet(ctx); err != nil {
		p.problem(err)
		return ExitFailure
	}
	return ExitOK
}

// demoStatus is the exit status of a failed Seed or Reset: 2 for Unsupported (demo is off)
// and for the demo conflicts seeded, not_empty and not_demo; for the reset's migrate step,
// the migrate command's own mapping (3 when the migrator found the schema behind, ahead or
// busy); else 1. A Conflict a service reported while seeding, with a field (a demo address
// that belongs to the superuser) or without one (a module's), is a failure of the seed: 1.
func demoStatus(err error) int {
	switch {
	case errors.Is(err, demo.ErrMigrate):
		return migrateStatus(err)
	case errs.Is(err, errs.Unsupported):
		return ExitUsage
	case errs.Is(err, errs.Conflict):
		for _, fe := range validate.Fields(err) {
			if fe.Field == demo.ConflictField {
				return ExitUsage
			}
		}
		return ExitFailure
	default:
		return ExitFailure
	}
}

// logDemo is serve's "demo mode on" warning, after "bms starting".
func (a *assembly) logDemo(ctx context.Context) {
	if a.cfg.Demo.Enabled {
		a.logger.WarnContext(ctx, "demo mode on")
	}
}

// demo prints bms check's `demo:` line: off, or on with the marker's state. A failed read
// is printed and is not a check failure.
func (r *checkReport) demo(ctx context.Context, a *assembly) {
	if !a.cfg.Demo.Enabled {
		r.printf("demo: off\n")
		return
	}
	if a.pool == nil {
		r.printf("demo: on (state unknown: no database)\n")
		return
	}
	// The services check built sit on a database that refuses every statement, so the marker
	// is read on the real pool, as the e-mail step does.
	cctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	st, err := demo.StateOf(cctx, a.pool)
	switch {
	case errs.Is(err, errs.NotFound): // no marker row: not seeded
		r.printf("demo: on (not seeded)\n")
	case err != nil:
		r.printf("demo: on (state unknown: %s)\n", errs.KindOf(err))
	case st.SeededAt.IsZero():
		r.printf("demo: on (seeding)\n")
	default:
		r.printf("demo: on (seeded %s)\n", st.SeededAt.UTC().Format(time.RFC3339))
	}
}
