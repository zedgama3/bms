package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/zedgama3/bms/internal/contracts/demoseed"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/demo"
	demopg "github.com/zedgama3/bms/internal/core/demo/postgres"
	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/core/objects"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
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
		a.departments == nil || a.events == nil || a.filesPolicy == nil {
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
		Actor:              authzActor,
		Files:              a.filesPolicy,
		Accounts:           a.identity,
		Access:             demoAccess{a: a},
		Addresses:          a.identity,
		Profiles:           demoProfiles{p: a.people},
		Departments:        a.departments,
		Events:             a.events,
		Settings:           a.settings,
		Schema:             demoSchema{a: a},
		Objects:            demoObjects{a: a},
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
		a.routeSets = append(a.routeSets, module.ModuleRoutes{
			Module:  demoArea,
			Routes:  demoRoutes(svc.Routes()),
			OpenAPI: svc.OpenAPI(),
		})
	}
	return nil
}

// demoSchemas is the schema of every migration set, deduplicated, in set order: "core" and
// each enabled module with migrations.
func demoSchemas(a *assembly) []string {
	var (
		out       []string
		manifests []module.Manifest
	)
	if a.plan != nil { // a test builds the parts without resolving the modules
		manifests = a.plan.Order()
	}
	for _, s := range a.setsFor(manifests) {
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

// demoRoutes converts demo's routes (demo cannot import core/module) to module routes.
func demoRoutes(in []demo.Route) []module.Route {
	out := make([]module.Route, len(in))
	for i, r := range in {
		access := module.AccessSignedIn
		if r.Public {
			access = module.AccessPublic
		}
		out[i] = module.Route{Method: r.Method, Pattern: r.Pattern, Access: access, Handler: r.Handler}
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

// ---- the adapters demo mode wires, all inert unless cfg.Demo.Enabled ------------------------

// demoGuard is identity's Guard in demo mode and nil otherwise. It is lazy because demo is
// built after identity; until then, and if the service is missing, it fails closed.
func demoGuard(a *assembly) identity.Guard {
	if !a.cfg.Demo.Enabled {
		return nil
	}
	return lazyGuard{a: a}
}

type lazyGuard struct{ a *assembly }

// Protected asks the demo service; it fails errs.Unavailable before demo is built, so a
// protected account is never opened by a gap in the wiring.
func (g lazyGuard) Protected(ctx context.Context, acc model.AccountID) (bool, error) {
	if g.a.demo == nil {
		return false, errs.E("app.lazyGuard.Protected", errs.Unavailable, errors.New("the demo service is not built yet"))
	}
	return g.a.demo.Protected(ctx, acc) //nolint:wrapcheck // the service's own error
}

// demoChannelFactory is notify's ChannelFactory in demo mode (every SMTPConfig yields the
// capture channel) and nil otherwise, which leaves notify its SMTP channel.
func demoChannelFactory(a *assembly) notify.ChannelFactory {
	if !a.cfg.Demo.Enabled {
		return nil
	}
	return func(notify.SMTPConfig) (notify.Channel, error) {
		if a.demo == nil {
			return nil, errs.E("app.demoChannelFactory", errs.Unavailable, errors.New("the demo service is not built yet"))
		}
		return a.demo.Channel(), nil
	}
}

// lazyFilesPolicy is files' PolicySource: the settings-backed one, capped by the demo
// service when demo mode is on. It is lazy because files is built before demo.
type lazyFilesPolicy struct{ a *assembly }

func (l lazyFilesPolicy) Policy(ctx context.Context) (files.Policy, error) {
	if l.a.cfg.Demo.Enabled {
		if l.a.demo == nil {
			// Fail closed, like lazyGuard: an upload must not slip past the demo cap through a
			// gap in the wiring.
			return files.Policy{}, errs.E("app.lazyFilesPolicy.Policy", errs.Unavailable, errors.New("the demo service is not built yet"))
		}
		return l.a.demo.Policy(ctx) //nolint:wrapcheck // the service's own error
	}
	return l.a.filesPolicy.Policy(ctx) //nolint:wrapcheck // the settings-backed source's own error
}

// demoAccess is the seed's slice of authz.
type demoAccess struct{ a *assembly }

func (d demoAccess) EnsureGrant(ctx context.Context, in authz.GrantInput) (authz.Grant, bool, error) {
	return d.a.authz.EnsureGrant(ctx, in) //nolint:wrapcheck // the service's own error
}

func (d demoAccess) Grant(ctx context.Context, in authz.GrantInput) (authz.Grant, error) {
	return d.a.authz.Grant(ctx, in) //nolint:wrapcheck // the service's own error
}

// StarterRoleIDs is authz's actor-less, read-only lookup, which the seed uses before any
// demo account holds a role.
func (d demoAccess) StarterRoleIDs(ctx context.Context, keys []string) (map[string]authz.RoleID, error) {
	return d.a.authz.StarterRoleIDs(ctx, keys) //nolint:wrapcheck // the service's own error
}

// demoProfiles is the seed's slice of people: the services answer a Profile, the seed needs
// the Person.
type demoProfiles struct{ p *people.Service }

func (d demoProfiles) UpdateMe(ctx context.Context, in people.ProfileInput) (people.Person, error) {
	pr, err := d.p.UpdateMe(ctx, in)
	return pr.Person, err //nolint:wrapcheck // the service's own error
}

func (d demoProfiles) Create(ctx context.Context, in people.ProfileInput) (people.Person, error) {
	pr, err := d.p.Create(ctx, in)
	return pr.Person, err //nolint:wrapcheck // the service's own error
}

// demoSchema is the seed's migrate step: the migrator over every set, then authz's
// starter-role seed, the same code path `bms migrate` runs.
type demoSchema struct{ a *assembly }

func (d demoSchema) Migrate(ctx context.Context) error {
	a := d.a
	if a.migrator == nil || a.authz == nil {
		return errs.E("app.demoSchema.Migrate", errs.Internal, errors.New("no migrator: demo reset runs against a real database"))
	}
	if _, err := a.migrator.Migrate(ctx, a.setsFor(a.plan.Order())); err != nil {
		return err //nolint:wrapcheck // the reset names the step
	}
	if _, err := a.authz.Seed(ctx, declaredPermissions(a)); err != nil {
		return err //nolint:wrapcheck // the reset names the step
	}
	return nil
}

// demoObjects clears every bucket the plan names (settings' and each enabled module's) and
// the files temp directory.
type demoObjects struct{ a *assembly }

func (d demoObjects) Clear(ctx context.Context) (int, error) {
	const op = "app.demoObjects.Clear"
	store, err := newObjectStore(d.a.cfg)
	if err != nil {
		return 0, err
	}
	buckets := []string{filesOwner}
	for _, m := range d.a.plan.Order() {
		if !slices.Contains(buckets, m.Name) {
			buckets = append(buckets, m.Name)
		}
	}
	deleted := 0
	for _, name := range buckets {
		b := objects.Bucket(name)
		if err := b.Validate(); err != nil {
			continue
		}
		var keys []objects.Key
		err := store.List(ctx, b, "", func(e objects.Entry) error {
			keys = append(keys, e.Key)
			return nil
		})
		if err != nil && !errs.Is(err, errs.NotFound) {
			return deleted, errs.E(op, errs.Unavailable, fmt.Errorf("list bucket %s: %w", name, err))
		}
		for _, k := range keys {
			if err := store.Delete(ctx, b, k); err != nil {
				return deleted, errs.E(op, errs.Unavailable, fmt.Errorf("delete in bucket %s: %w", name, err))
			}
			deleted++
		}
	}
	dir, err := filesDir(d.a.cfg)
	if err != nil {
		return deleted, err
	}
	entries, err := os.ReadDir(filepath.Join(dir, "tmp"))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return deleted, nil
	case err != nil:
		return deleted, errs.E(op, errs.Unavailable, fmt.Errorf("read the files temp directory: %w", err))
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(dir, "tmp", e.Name())); err != nil {
			return deleted, errs.E(op, errs.Unavailable, fmt.Errorf("remove a temp file: %w", err))
		}
		deleted++
	}
	return deleted, nil
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
	a.operator = true
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
	// is read through a store on the real pool, as the e-mail step does.
	store, err := demopg.NewStore(a.pool)
	if err != nil {
		r.printf("demo: on (state unknown: %s)\n", errs.KindOf(err))
		return
	}
	cctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	st, err := store.State(cctx)
	switch {
	case errs.Is(err, errs.NotFound): // no marker row: not seeded (Service.State maps it the same way)
		r.printf("demo: on (not seeded)\n")
	case err != nil:
		r.printf("demo: on (state unknown: %s)\n", errs.KindOf(err))
	case st.SeededAt.IsZero():
		r.printf("demo: on (seeding)\n")
	default:
		r.printf("demo: on (seeded %s)\n", st.SeededAt.UTC().Format(time.RFC3339))
	}
}
