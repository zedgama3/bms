package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/api"
	"github.com/zedgama3/bms/internal/config"
	"github.com/zedgama3/bms/internal/core/auditlog"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/demo"
	"github.com/zedgama3/bms/internal/core/departments"
	"github.com/zedgama3/bms/internal/core/events"
	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/core/regional"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/web"
)

// Constants that are not configuration keys (app.md "Settings"), each with its reason
// there. TestConstants_Pinned pins them.
const (
	buildTimeout   = 30 * time.Second // bounds Plan.Build
	startTimeout   = 30 * time.Second // bounds Set.Start, the runnables' start and an unwind after a failed start
	listenTimeout  = 5 * time.Second  // bounds api.Listen
	migrateTimeout = 30 * time.Minute // bounds one `bms migrate` run
	checkTimeout   = 30 * time.Second // bounds each probe `check` makes

	// handlerTimeout is signals.Options.HandlerTimeout (signals.md decision 5).
	handlerTimeout = 5 * time.Second
	// openAPIVersion is the fixed info.version of `bms openapi`, so the committed
	// document does not change with every commit (decision 7).
	openAPIVersion = "dev"
)

// envModulesDisabled is the variable F5 names.
const envModulesDisabled = config.Prefix + "MODULES_DISABLED"

// httpServer is the part of *api.Server the lifecycle uses. serve builds it through
// newServer, which an internal test replaces to make Serve fail (F13).
type httpServer interface {
	Listen(ctx context.Context) (net.Addr, error)
	Serve() error
	Shutdown(ctx context.Context) error
	OpenAPI() []byte
}

var newServer = func(o api.Options) (httpServer, error) {
	srv, err := api.New(o)
	if err != nil {
		return nil, err //nolint:wrapcheck // the caller wraps with the step's name
	}
	return srv, nil
}

// webFS and webVersion are the web build, replaceable in internal tests (F17, F18).
var (
	webFS      = web.FS
	webVersion = web.Version
)

// assembly is the core being built: what the commands fill in and what the parts read and
// extend. One per command run. It is not safe for concurrent use; it is built on one
// goroutine before anything serves.
type assembly struct {
	cfg      config.Config
	version  string
	clock    clock.Clock
	tel      *telemetry.Telemetry
	logger   *slog.Logger // tel.Logger().With(slog.String("component", "app"))
	dbc      db.DB        // *db.Pool for serve, migrate, check; db.None() for openapi (decision 7)
	pool     *db.Pool     // nil in openapi
	migrator *db.Migrator // nil in openapi
	disp     *signals.Dispatcher
	plan     *module.Plan // after Resolve
	set      *module.Set  // after Build
	ui       fs.FS        // the web build, or nil (decision 8)

	// What parts contribute, in part order.
	coreSignals   []signals.Name              // Env.CoreSignals (people, departments, events)
	corePoints    []module.ExtensionPointDecl // Env.CoreExtensionPoints
	coreSets      []db.Set                    // the core's migration sets, in part order
	corePerms     []authz.PermissionDecl      // api.PermissionModulesRead first, then each part's
	routeSets     []module.ModuleRoutes       // one per core area with routes; Module is the area name
	authenticator api.Authenticator           // identity (M1); nil until then
	permissions   api.PermissionChecker       // the authz.Lookup; nil before buildLookups
	names         identity.NameSource         // people (M2); nil until then (identity reads it lazily)
	deps          coreDeps                    // copied into every module's Deps by provision

	runnables []runnable // started after Set.Start, stopped before it (jobs, M4)
	commands  []command  // added by declare

	// Beyond the spec's list: the parts, what they built that a command calls, and what the
	// lifecycle has started.
	identity *identity.Service // the identity part's service (create-superuser); nil before its build
	authz    *authz.Service    // the authz part's service (migrate's seed, people's Access); nil before its build
	people   *people.Service   // the people part's service (export, delete); nil before its build

	settings *settings.Service // the settings part's service (identity, people, notify and files read their areas through it); nil before its build

	departments *departments.Service // the departments part's service; nil before its build

	departmentsLookup *departments.Lookup        // built by buildLookups; nil before it
	authzLookup       *authz.Lookup              // built by buildLookups; audit, identity, settings and api take it; nil before it
	events            *events.Service            // the events part's service; nil before its build
	notify            *notify.Service            // the notify part's service (the alert, the outbox in export); nil before its build
	regional          *regional.Service          // the regional part's service (notify's site source); nil before its build
	files             *files.Service             // the files part's service (the Start probe, bms check, bms files verify); nil before its build
	auditlog          *auditlog.Service          // the auditlog part's service (people's deletion report, the audit routes); nil before its build
	jobs              *jobs.Runner               // the jobs part's runner (lazyDeferrer, audit.purge, bms check); nil before its build
	notifiers         map[string]notify.Notifier // the Notifiers notify scoped for identity and people (Scoped fails once Run started); nil before its build
	stdout            io.Writer                  // the command's standard output (migrate's seed line)
	parts             []part
	started           []runnable
	srv               httpServer
	uiVersion         string
	demo              *demo.Service   // the demo part's service (demo seed|reset, the lazy adapters); nil before its build
	filesReader       settings.Reader // the reader scoped "files"; the demo part caps the policy mapped from it when enabled
	forDocument       bool            // the openapi command: the client services are built off and empty (usage.go)
}

// newAssembly returns the assembly of one command run over cfg. Telemetry and everything
// after it are set by the steps below.
func newAssembly(p *process, cfg config.Config) *assembly {
	return &assembly{
		cfg:       cfg,
		version:   p.version,
		clock:     p.clock,
		logger:    slog.New(slog.DiscardHandler),
		parts:     p.parts,
		stdout:    p.stdout,
		corePerms: slices.Clone(api.Permissions),
	}
}

// startTelemetry builds telemetry from the configuration, writing log lines to stdout (the
// server writes its own stdout, every other command stderr; decision 4).
func (a *assembly) startTelemetry(ctx context.Context, lookup func(string) (string, bool), stdout io.Writer) error {
	tel, err := telemetry.New(ctx, telemetry.Options{
		Level:    a.cfg.Log.Level,
		Format:   telemetry.Format(a.cfg.Log.Format),
		Instance: a.cfg.Instance.Name,
		Version:  a.version,
		Stdout:   stdout,
		Clock:    a.clock,
		Environ:  lookup,
	})
	if err != nil {
		return err //nolint:wrapcheck // telemetry.New already names itself
	}
	a.setTelemetry(tel)
	return nil
}

func (a *assembly) setTelemetry(tel *telemetry.Telemetry) {
	a.tel = tel
	a.logger = tel.Logger().With(slog.String("component", "app"))
}

// scopes returns the tracer and meter of the named telemetry scope.
func (a *assembly) scopes(name string) (telemetry.Tracer, telemetry.Meter, error) {
	tr, err := a.tel.Tracer(name)
	if err != nil {
		return nil, nil, fmt.Errorf("tracer %s: %w", name, err)
	}
	m, err := a.tel.Meter(name)
	if err != nil {
		return nil, nil, fmt.Errorf("meter %s: %w", name, err)
	}
	return tr, m, nil
}

// component returns the logger of a core component.
func (a *assembly) component(name string) *slog.Logger {
	return a.tel.Logger().With(slog.String("component", name))
}

// enabledModules derives Env.Enabled: the registry's names in registry order minus
// disabled. A disabled name that is not registered fails with errs.Invalid under
// BMS_MODULES_DISABLED, listing every unknown name and the compiled-in ones (F5).
func enabledModules(r *module.Registry, disabled []string) ([]string, error) {
	all := r.Manifests()
	registered := make(map[string]bool, len(all))
	names := make([]string, len(all))
	for i, m := range all {
		registered[m.Name] = true
		names[i] = m.Name
	}
	acc := validate.Errors{Op: "app.enabledModules"}
	off := make(map[string]bool, len(disabled))
	for _, d := range disabled {
		d = strings.TrimSpace(d)
		if !registered[d] {
			compiled := "(none)"
			if len(names) > 0 {
				compiled = strings.Join(names, ", ")
			}
			acc.Addf(envModulesDisabled, validate.CodeOneOf, "module %q is not compiled in; compiled-in modules: %s", d, compiled)
			continue
		}
		off[d] = true
	}
	if err := acc.Err(); err != nil {
		return nil, err //nolint:wrapcheck // Err already carries Op and errs.Invalid
	}
	enabled := make([]string, 0, len(names))
	for _, n := range names {
		if !off[n] {
			enabled = append(enabled, n)
		}
	}
	return enabled, nil
}

// resolve runs every part's declare and the registry's Resolve over enabled, logs each
// warning and keeps the plan (F7, F28).
func (a *assembly) resolve(ctx context.Context, reg *module.Registry, enabled []string) error {
	if err := a.declare(); err != nil {
		return err
	}
	plan, err := reg.Resolve(module.Env{
		Enabled:             enabled,
		CoreSignals:         a.coreSignals,
		CoreExtensionPoints: a.corePoints,
	})
	if err != nil {
		return err //nolint:wrapcheck // Resolve names itself and carries every problem
	}
	a.plan = plan
	for _, w := range plan.Graph().Warnings {
		a.logger.WarnContext(ctx, "module warning", slog.String("module", w.Module), slog.String("message", w.Message))
	}
	return nil
}

// openDatabase opens the pool and the migrator over the configuration (F8).
func (a *assembly) openDatabase(ctx context.Context) error {
	tr, m, err := a.scopes("db")
	if err != nil {
		return err
	}
	pool, err := db.Open(ctx, db.Options{
		URL:            a.cfg.DB.URL,
		MaxConns:       a.cfg.DB.MaxConns,
		ConnectTimeout: a.cfg.DB.ConnectTimeout,
		QueryTimeout:   a.cfg.DB.QueryTimeout,
		Tracer:         tr,
		Meter:          m,
	}, a.clock, a.component("db"))
	if err != nil {
		return err //nolint:wrapcheck // db.Open names itself and scrubs the URL
	}
	a.pool, a.dbc = pool, pool
	mg, err := db.NewMigrator(pool, a.component("db"))
	if err != nil {
		return err //nolint:wrapcheck // db.NewMigrator names itself
	}
	a.migrator = mg
	return nil
}

// newDispatcher builds the signals dispatcher (decision 9, step 6).
func (a *assembly) newDispatcher() error {
	tr, m, err := a.scopes("signals")
	if err != nil {
		return err
	}
	d, err := signals.New(signals.Options{
		Logger:         a.component("signals"),
		Tracer:         tr,
		Meter:          m,
		Clock:          a.clock,
		HandlerTimeout: handlerTimeout,
		Deferrer:       lazyDeferrer{a: a},
	})
	if err != nil {
		return err //nolint:wrapcheck // signals.New names itself
	}
	a.disp = d
	return nil
}

// provision is the module.Provision for this assembly (module-contract Proposed change
// 9; telemetry.md and signals.md Proposed change 5): Logger = tel.Logger().With("module",
// m.Name); Tracer = tel.Tracer("module." + m.Name); Clock = a.clock; DB = a.dbc; Signals
// = a.disp.Scope(m.Name, m.Publishes, m.Subscribes); plus a.deps, scoped where the field's
// spec says so. It fails with the kind of whatever refused (F29).
func (a *assembly) provision(_ context.Context, m module.Manifest) (module.Deps, error) {
	tr, err := a.tel.Tracer("module." + m.Name)
	if err != nil {
		return module.Deps{}, fmt.Errorf("module %s: tracer: %w", m.Name, err)
	}
	bus, err := a.disp.Scope(m.Name, m.Publishes, m.Subscribes)
	if err != nil {
		return module.Deps{}, fmt.Errorf("module %s: signals scope: %w", m.Name, err)
	}
	deps := module.Deps{
		Logger:  a.tel.Logger().With(slog.String("module", m.Name)),
		Tracer:  tr,
		Clock:   a.clock,
		DB:      a.dbc,
		Signals: bus,
	}
	if a.deps.audit != nil {
		if deps.Audit, err = a.deps.audit.Scoped(m.Name); err != nil {
			return module.Deps{}, fmt.Errorf("module %s: audit scope: %w", m.Name, err)
		}
	}
	if a.deps.settings != nil && len(m.Settings) > 0 {
		// A module that declares no setting has no area and no key to read: its Reader stays nil.
		if deps.Settings, err = a.deps.settings.Scoped(m.Name); err != nil {
			return module.Deps{}, fmt.Errorf("module %s: settings scope: %w", m.Name, err)
		}
	}
	if a.deps.notify != nil {
		if deps.Notify, err = a.deps.notify.Scoped(m.Name); err != nil {
			return module.Deps{}, fmt.Errorf("module %s: notify scope: %w", m.Name, err)
		}
	}
	if a.deps.jobs != nil {
		if deps.Jobs, err = a.deps.jobs.Scoped(m.Name); err != nil {
			return module.Deps{}, fmt.Errorf("module %s: jobs scope: %w", m.Name, err)
		}
	}
	if a.deps.files != nil {
		if deps.Files, err = a.deps.files.Scoped(m.Name); err != nil {
			return module.Deps{}, fmt.Errorf("module %s: files scope: %w", m.Name, err)
		}
	}
	deps.Authz = a.deps.authz
	deps.People = a.deps.people
	deps.Departments = a.deps.departments
	deps.Events = a.deps.events
	return deps, nil
}

// checkSchemas checks the core's migration sets the way Plan.Build checks a module's. When
// they are all current it returns nil and leaves the modules to Plan.Build (F9), so no set
// is checked twice on the way to a running server. When one is missing or differs, it also
// checks manifests' sets (the enabled modules with migrations), because nothing is built
// after this failure and the one error should name every set that is out of date. With
// skipSchema nothing is checked. The kind is Conflict when every problem is one, else the
// first other kind.
func (a *assembly) checkSchemas(ctx context.Context, schema module.SchemaChecker, manifests []module.Manifest) error {
	problems, kind := checkSets(ctx, schema, a.coreSets, errs.Conflict)
	if len(problems) == 0 {
		return nil
	}
	var sets []db.Set
	for _, m := range manifests {
		if m.Migrations != nil {
			sets = append(sets, db.Set{Name: m.Name, Files: m.Migrations})
		}
	}
	more, kind := checkSets(ctx, schema, sets, kind)
	problems = append(problems, more...)
	return errs.E("app.checkSchemas", kind, errors.Join(problems...))
}

// checkSets runs CheckSchema for each set and returns the problems and the kind: kind in,
// unless it is still Conflict and a problem has another kind (the first such decides).
func checkSets(ctx context.Context, schema module.SchemaChecker, sets []db.Set, kind errs.Kind) ([]error, errs.Kind) {
	var problems []error
	for _, s := range sets {
		if err := schema.CheckSchema(ctx, s.Name, s.Files); err != nil {
			problems = append(problems, err)
			if k := errs.KindOf(err); kind == errs.Conflict && k != errs.Conflict {
				kind = k
			}
		}
	}
	return problems, kind
}

// buildModules runs the parts' build, builds the modules under buildTimeout with schema
// as the checker, then runs the parts' link (decision 9, step 8; F9).
func (a *assembly) buildModules(ctx context.Context, schema module.SchemaChecker) error {
	bctx, cancel := context.WithTimeout(ctx, buildTimeout)
	defer cancel()
	// The core's sets are checked here, before the parts that use their tables are built;
	// Plan.Build checks the modules' (F9).
	if err := a.checkSchemas(bctx, schema, a.plan.Order()); err != nil {
		return err
	}
	if err := a.buildParts(bctx); err != nil {
		return err
	}
	set, err := a.plan.Build(bctx, schema, a.provision)
	if err != nil {
		return err //nolint:wrapcheck // Build names the module and the step
	}
	a.set = set
	return a.linkParts(bctx)
}

// loadUI resolves the web build and warns when it is missing or stale (decision 8;
// F17, F18).
func (a *assembly) loadUI(ctx context.Context) {
	ui, err := webFS()
	switch {
	case err == nil:
		a.ui = ui
	case errs.Is(err, errs.NotFound):
		a.logger.WarnContext(ctx, "serving without a UI: run `make web-build`")
		return
	default:
		a.logger.WarnContext(ctx, "serving without a UI: the web build is unusable",
			slog.String("error.kind", errs.KindOf(err).String()))
		return
	}
	v, err := webVersion()
	switch {
	case err != nil:
		a.logger.WarnContext(ctx, "the web build has no usable version; rebuild it with `make web-build`",
			slog.String("version", a.version), slog.String("error.kind", errs.KindOf(err).String()))
	case v != a.version:
		a.uiVersion = v
		a.logger.WarnContext(ctx, "the web build is from another version; rebuild it with `make web-build`",
			slog.String("version", a.version), slog.String("ui_version", v))
	default:
		a.uiVersion = v
	}
}

// uiLabel is the ui field of the log lines: "built" or "absent".
func (a *assembly) uiLabel() string {
	if a.ui == nil {
		return "absent"
	}
	return "built"
}

// setsFor returns the migration sets of the core, then of each manifest that has
// migrations, in the order given (migrate step 6).
func (a *assembly) setsFor(manifests []module.Manifest) []db.Set {
	sets := append([]db.Set(nil), a.coreSets...)
	for _, m := range manifests {
		if m.Migrations != nil {
			sets = append(sets, db.Set{Name: m.Name, Schema: m.Name, Files: m.Migrations})
		}
	}
	return sets
}

// addCoreRoutes converts a core area's routes with module.CoreRoutes and adds the set to the
// assembly's route sets. A bad route is a programming error and fails the build (boundaries
// B1, B2).
func (a *assembly) addCoreRoutes(area string, routes []httpx.Route, openapi []byte) error {
	set, err := module.CoreRoutes(area, routes, openapi)
	if err != nil {
		return err //nolint:wrapcheck // CoreRoutes names itself
	}
	a.routeSets = append(a.routeSets, set)
	return nil
}

// apiOptions returns the options of api.New for this assembly; the caller overrides what
// differs per command (openapi, check).
func (a *assembly) apiOptions(version string, database api.Database, schema api.SchemaStatus) (api.Options, error) {
	tr, m, err := a.scopes("api")
	if err != nil {
		return api.Options{}, err
	}
	var routes []module.ModuleRoutes
	if a.set != nil {
		routes = append(routes, a.set.Routes()...)
	}
	routes = append(routes, a.routeSets...)
	in, usage, err := a.clientServices(routes)
	if err != nil {
		return api.Options{}, err
	}
	return api.Options{
		Addr:           a.cfg.HTTP.Addr,
		RequestTimeout: a.cfg.HTTP.RequestTimeout,
		MaxBodyBytes:   a.cfg.HTTP.MaxBodyBytes,
		TrustedProxies: a.cfg.HTTP.TrustedProxies,
		AllowPlaintext: a.cfg.HTTP.AllowPlaintext,
		Logger:         a.component("api"),
		Tracer:         tr,
		Meter:          m,
		Clock:          a.clock,
		Version:        version,
		Routes:         routes,
		Database:       database,
		Intake:         in,
		Usage:          usage,
		Graph:          a.plan.Graph(),
		Schema:         schema,
		Authenticator:  a.authenticator,
		Permissions:    a.permissions,
		UI:             a.ui,
		NoIndex:        a.cfg.Demo.Enabled,
	}, nil
}

// closers are the shutdown steps in the order of decision 10, with audit's Close after
// Set.Stop (late work of a stopping module may still audit) and before db.Close. Steps for things that
// were never started do nothing, so a failed start-up runs the same list.
func (a *assembly) closers() []closer {
	return []closer{
		{name: "http", close: func(ctx context.Context) error {
			if a.srv == nil {
				return nil
			}
			return a.srv.Shutdown(ctx) //nolint:wrapcheck // shutdown names the step
		}},
		{name: "runnables", close: a.stopRunnables},
		{name: "signals", close: func(ctx context.Context) error {
			if a.disp == nil {
				return nil
			}
			return a.disp.Close(ctx) //nolint:wrapcheck // shutdown names the step
		}},
		{name: "modules", close: func(ctx context.Context) error {
			if a.set == nil {
				return nil
			}
			return a.set.Stop(ctx) //nolint:wrapcheck // shutdown names the step
		}},
		{name: "audit", close: func(ctx context.Context) error {
			if a.deps.audit == nil {
				return nil
			}
			return a.deps.audit.Close(ctx) //nolint:wrapcheck // shutdown names the step
		}},
		{name: "db", close: func(ctx context.Context) error {
			if a.pool == nil {
				return nil
			}
			return a.pool.Close(ctx) //nolint:wrapcheck // shutdown names the step
		}},
		{name: "telemetry", close: func(ctx context.Context) error {
			if a.tel == nil {
				return nil
			}
			return a.tel.Close(ctx) //nolint:wrapcheck // shutdown names the step
		}},
	}
}

// unwind stops whatever started, in order, within startTimeout, after a failed start-up.
// The failure that caused it is the one reported; a failing closer is logged by shutdown
// and not repeated.
func (a *assembly) unwind(ctx context.Context) {
	if err := shutdown(ctx, a.logger, a.clock, startTimeout, a.closers()); err != nil {
		a.logger.ErrorContext(ctx, "cleanup after a failed start-up did not finish", slog.String("error.kind", errs.KindOf(err).String()))
	}
}

// fail reports err once, to stderr and, when telemetry exists, to the log, and returns
// status. The log carries the kind and the message; neither holds a secret (config reasons
// never echo values; db scrubs its errors).
func (a *assembly) fail(ctx context.Context, p *process, err error, status int) int {
	if errs.Is(err, errs.Canceled) && ctx.Err() != nil {
		sayln(p.stderr, interruptedText)
		a.logger.ErrorContext(ctx, "interrupted", slog.String("error.kind", errs.Canceled.String()))
		return ExitFailure
	}
	p.problem(err)
	a.logger.ErrorContext(ctx, "bms failed", slog.String("error.kind", errs.KindOf(err).String()), slog.String("error", err.Error()))
	return status
}

// closeQuiet stops whatever started, silently, for the commands that do not serve. It
// returns the failures joined.
func (a *assembly) closeQuiet(ctx context.Context) error {
	return shutdown(ctx, nil, a.clock, checkTimeout, a.closers())
}
