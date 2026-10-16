# module: the module contract and registry

- **Status:** Draft (2026-10-16)
- **Package:** `internal/core/module` (Go module `github.com/zedgama3/bms`), plus the test
  helper package `internal/core/module/moduletest`. **UI:** none of its own; the admin
  "modules" screen is `web-shell.md`'s, fed by `GET /api/modules` (`api.md`), both drawing
  on the `Graph` this spec provides.
- **Depends on specs:** `lib-errs.md` (Implemented): `errs.E`, `errs.Is`, kinds `Invalid`,
  `Conflict`, `NotFound`, `Internal`, `Canceled`, `Timeout`. `lib-core.md` (Implemented):
  `clock.Clock`, `validate.Errors`, `validate.FieldError`. `model.md` (Approved):
  `model.PersonID`, `model.Sensitivity`, `ID.Validate`, `Sensitivity.Validate`. Two leaf
  types that this spec places in other core packages and defines here because no spec for
  them exists yet: `authz.Permission`, `authz.Scope`, `authz.PermissionDecl`
  (`internal/core/authz`, row 7) and `signals.Name` (`internal/core/signals`, row 10). See
  "Types" and Proposed changes 2 and 3. The `Deps` struct also names fields typed by `db.md`
  (row 5) and `signals.md` (row 10); each is added when that spec is Approved (see "Types:
  Deps").
  **Used by:** `app` (composition root: resolves, builds, starts and stops the modules; the
  `migrate` and `check` commands), `api` (mounts routes, serves the graph), `authz` (receives
  the declared permissions), `settings` (receives the declared settings), `people` (export
  and deletion across modules), `signals` (the declared names), every module (`Manifest`,
  `Register`, `Deps`, `Links`), and the `hello` reference module (09-core-features §1).

## Purpose

Describe each compiled-in module to the core well enough that `app` can wire it in without
knowing it.

## Responsibilities

- Define the **manifest**: the static declaration of a module (name, migrations,
  permissions, settings, signals published and subscribed, capabilities provided, required
  and used, extension points defined and contributed to; D19) and its validation.
- Define the **module**: what a built module gives the core at runtime (routes, capability
  implementations, contributions, export and deletion of one person's data, start and stop)
  and what the core gives it (`Deps`).
- Keep the **registry** of compiled-in modules (D5) and **resolve** it against the set the
  configuration enables: check the cooperation graph (every required capability has exactly
  one enabled provider; every subscribed signal and every contribution names something that
  exists), compute the build order, and expose the graph for the admin screen (09 §7).
- **Build** the enabled modules in that order (schema check first, D17), verify that each
  module implements what its manifest declares, link providers to consumers, and start and
  stop them in order.
- Define the **conformance test** every module runs against itself (`moduletest`).

## Not responsible for

| Concern | Owner |
|---|---|
| Applying migrations, recording schema versions, deciding whether a database is read-only | `db` (`db.md`). This spec calls `db` to check each enabled module's schema version before building it (D17) through the `SchemaChecker` interface and never touches the schema. |
| Mounting routes, middleware, sessions, CSRF, the error mapper, the OpenAPI document | `api` (`api.md`). This spec hands it each module's route list and OpenAPI fragment. |
| Deciding whether a person may do something; roles, grants, separation of duties | `authz` (`authz.md`). This spec hands it the declared permissions. |
| Delivering signals, the catalogue of payloads, after-commit semantics | `signals` (`signals.md`). This spec validates names and tells `signals` which names each module declared. |
| Storing, typing and editing settings; terminology; retention settings per module and level (D30) | `settings` (`settings.md`). This spec hands it the declared settings; `settings` adds the retention keys for every module itself. |
| Running jobs, and running them only on the writing copy | `jobs` (`jobs.md`). A module registers its jobs through `Deps.Jobs` in `Start`. |
| Orchestrating "export everything about person X" and "delete everything about person X" across modules, checking the permission for it and auditing it | `people` (`people.md`). This spec defines the `PersonData` interface every module must implement and gives `people` the list. |
| The shapes of capabilities and extension points (the Go interfaces and data types) | `internal/contracts/<name>/` (D19). This spec carries only their names and checks the wiring. |
| The module's UI: screens, navigation, its message catalogue, the build-time web registry (D31) | `web-shell.md`. The manifest does not declare "has UI"; see decision 9. |
| Forms (D32), terminology, any feature a burner sees | the module's own spec, written with `add-module`. |
| Process configuration, including which modules are enabled | `config` (`config.md`). This spec receives the enabled names in `Env`. |
| Telemetry: tracer, meter, request IDs | `telemetry.md`. Modules get a scoped logger in `Deps`; tracing is the api shell's middleware. |
| Trust between regionals, versioned contracts, upgrade promises | Q-TRUST, Q-UPGRADE. A capability or extension point name is stable; a breaking change to a contract is a new name (D19). |

## Sub-abstractions

| Name | Purpose (one line) | Notes |
|---|---|---|
| Names | Give each kind of declared identifier one format and one `Validate`. | `Capability`, `ExtensionPoint`, `ExtensionPointDecl`, `SettingDecl`; plus `authz.Permission`/`PermissionDecl` and `signals.Name`, placed in their own packages. |
| `Manifest` | Declare a module statically, with no dependencies, so `migrate`, `check` and the graph need nothing built. | A value; `Validate` reports every problem at once. |
| `Module`, `Deps`, `Factory` | State what a built module gives the core and what the core gives it. | Mandatory: `PersonData`, `Start`, `Stop`. Optional interfaces: `RouteProvider`, `APIDescriber`, `CapabilityProvider`, `Contributor`. |
| `Registry` and `Plan` | Hold the compiled-in modules; resolve the enabled set into a checked graph and a build order. | `Register` at `init`; `Resolve(env)` is pure over the registry. |
| `Links` | Let a built module reach the capabilities it declared and the contributions to the points it defines, and nothing else. | Per-module view, frozen after `Build`. Safe for concurrent use. |
| `Set` | The built, enabled modules: routes, person-data handlers, start and stop in order. | Built once per process by `app`. |
| `moduletest` | Prove a module keeps the contract. | `Conformance(t, manifest, factory, provision)`. |

## Interface

```go
// Package module is the contract between the core and the feature modules compiled into
// the binary (D5, D19): what a module declares (Manifest), what it gives the core at
// runtime (Module), what the core gives it (Deps), the registry of modules, and the
// start-up check of the whole graph. It sits at the top of the core: it imports core
// contracts, and no core package imports it.
package module

// ---- Names ----

// ident is the segment grammar every declared name uses: lower-case letters and digits in
// kebab-case, "[a-z][a-z0-9]*(-[a-z0-9]+)*", at most 64 bytes. Exported names are built
// from idents joined with ".". Module names are stricter (see Manifest.Name).

// Capability names a service that exactly one enabled module provides and others require
// or use (D19). Its Go interface lives in internal/contracts/<capability>/, which exports
// the name as an untyped string constant (const Name = "volunteer-credit") so it converts
// to this type. Format: one ident.
type Capability string

// Validate fails with errs.Invalid if c is not one ident.
func (c Capability) Validate() error

// ExtensionPoint names a place where any number of modules contribute something the
// definer shows or collects (D19): "<definer>.<ident>", for example "people.person-page".
// The definer is a module name or a core area name (people, events, departments).
type ExtensionPoint string

// Validate fails with errs.Invalid unless p is "<ident>.<ident>".
func (p ExtensionPoint) Validate() error

// Definer returns the part before the first ".". Cannot fail; meaningful once Validate
// passes.
func (p ExtensionPoint) Definer() string

// ExtensionPointDecl declares an extension point and how to recognise a valid
// contribution, so Build can reject a wrong type at start-up with both module names.
type ExtensionPointDecl struct {
	Name ExtensionPoint
	// Accepts reports whether v implements the point's contract interface, typically
	// func(v any) bool { _, ok := v.(personpage.Section); return ok }. Must not be nil.
	Accepts func(v any) bool
}

// Validate fails with errs.Invalid if Name fails Validate or Accepts is nil.
func (d ExtensionPointDecl) Validate() error

// ---- Settings ----

// SettingKind is the type of a setting's value. The zero value is invalid.
type SettingKind uint8

const (
	SettingBool     SettingKind = iota + 1 // "true" or "false"
	SettingInt                             // a base-10 integer
	SettingDuration                        // time.ParseDuration form, for example "72h"
	SettingText                            // free text, up to 4096 bytes
	SettingEnum                            // exactly one of Options
)

// String returns "bool", "int", "duration", "text", "enum" or "SettingKind(<n>)". Cannot fail.
func (k SettingKind) String() string

// SettingDecl declares one runtime setting of a module (settings.md stores and edits it
// under the key "<module>.<key>"). Its title and description are message keys, not text
// (decision 8).
type SettingDecl struct {
	Key     string      // one ident
	Kind    SettingKind // how Default and every value are parsed
	Default string      // text form; must parse as Kind and pass Validate
	Options []string    // SettingEnum only: the allowed values, each one ident, at least two
	// Validate is an optional extra rule on the parsed text ("must be between 1 and 24");
	// nil means the kind's parse is the whole rule. It must be pure and safe for
	// concurrent use.
	Validate func(value string) error
}

// Validate fails with errs.Invalid, joining one validate.FieldError per problem: Key not
// an ident; Kind invalid; Options given for a kind other than enum, or fewer than two or
// not idents or duplicated for enum; Default not parsable as Kind; Default rejected by
// Validate.
func (d SettingDecl) Validate() error

// ---- Manifest ----

// Manifest is everything the core needs to know about a module without building it. It
// is a plain value with no dependencies, so `bms migrate`, `bms check` and the admin
// graph work before any module is constructed. Every human-readable text about it lives
// in the module's message catalogue under keys derived from these names (decision 8).
type Manifest struct {
	// Name is the module's identity everywhere: Go package, PostgreSQL schema, route
	// prefix /api/<name>/, permission and signal prefix, web folder, message-key segment.
	// Format: "[a-z][a-z0-9]{1,31}" (letters and digits only, so one name fits every
	// place without translation) and not a reserved name (see Behavior).
	Name string

	// Migrations holds postgres/migrations/*.sql (go:embed), applied by `bms migrate` and
	// checked by `db` at start-up (D17). nil means the module has no schema; then it is
	// neither migrated nor checked.
	Migrations fs.FS

	// Permissions this module declares. Each name starts with "<Name>.". authz turns them
	// into what a regional can grant (09 §4).
	Permissions []authz.PermissionDecl

	// Settings this module declares. Keys are stored as "<Name>.<Key>" by settings.
	Settings []SettingDecl

	// Publishes and Subscribes are signal names (D24). A published name starts with
	// "<Name>."; a subscribed name is any valid name, declared by the core or by a module
	// compiled in.
	Publishes  []signals.Name
	Subscribes []signals.Name

	// Provides, Requires and Uses are capabilities (D19). Requires: start-up fails
	// without an enabled provider. Uses: optional; Links.Capability returns NotFound and
	// the module hides the feature.
	Provides []Capability
	Requires []Capability
	Uses     []Capability

	// Defines are extension points this module owns; their names start with "<Name>.".
	// ContributesTo are points, owned by the core or another module, this module
	// contributes to.
	Defines       []ExtensionPointDecl
	ContributesTo []ExtensionPoint
}

// Validate checks the manifest alone (not the graph) and fails with errs.Invalid joining
// one validate.FieldError per problem, with fields such as "name", "permissions[2].name",
// "settings[0].default". See failure modes F1–F9. It never reads Migrations.
func (m Manifest) Validate() error

// ---- Module ----

// Factory builds a module from its dependencies. It validates the fields of deps it uses
// (go-conventions: constructors validate), builds its services, and does no I/O beyond
// reads; it never writes to the database, so a module builds on a read-only copy. ctx
// bounds any read it does.
type Factory func(ctx context.Context, deps Deps) (Module, error)

// Module is what every module gives the core at runtime. The optional interfaces below
// add routes, an API description, capability implementations and contributions; Build
// checks that a module implements exactly what its manifest declares (F29–F32).
type Module interface {
	PersonData

	// Start begins the module's background work: it registers signal handlers and jobs
	// through Deps and launches goroutines that exit when Stop is called. It returns once
	// that is set up, without blocking, and must succeed on a read-only database (it
	// performs no writes). It fails with the kind of whatever it tried; Set.Start then
	// stops the modules already started.
	Start(ctx context.Context) error

	// Stop ends the work Start began and waits for it, within ctx. It is called once,
	// after Start returned nil, and must be safe to call when Start was never called.
	// Fails with errs.Timeout if ctx ends first, joined with any goroutine's error.
	Stop(ctx context.Context) error
}

// PersonData is how a module answers "export everything about person X" and "delete
// everything about person X" (privacy-data). Every module implements it, including one
// that holds no personal data (it returns an empty Export and a zero Deletion), so the
// obligation is visible in code and tested by moduletest. The caller is the core
// (people.md) with an actor who has already been authorized for people.data.export or
// people.data.delete and whose action people audits; a module does not re-check that
// permission, and these methods are never reachable through a module's own routes.
type PersonData interface {
	// ExportPerson returns every record this module holds about person, at every
	// sensitivity level, as one JSON object the module's spec documents (field names are
	// stable, so a person can read their own data). An empty Data means nothing is held.
	// Fails with errs.Invalid for a zero person and with the store's kind otherwise.
	ExportPerson(ctx context.Context, person model.PersonID) (Export, error)

	// DeletePerson deletes or irreversibly anonymizes every record this module holds
	// about person, including attachments, and reports what it did. Records the module's
	// spec says must be kept (a legal or safety reason) are counted under Kept with their
	// kind. It is idempotent: a person with no records yields a zero Deletion and nil.
	// Fails with errs.Invalid for a zero person; on a read-only copy, with the kind db
	// returns for a write (db.md).
	DeletePerson(ctx context.Context, person model.PersonID) (Deletion, error)
}

// Export is one module's share of a person's data export.
type Export struct {
	Data json.RawMessage // a JSON object, or empty when the module holds nothing
}

// Deletion reports what DeletePerson did.
type Deletion struct {
	Deleted    int    // records removed
	Anonymized int    // records kept with personal fields irreversibly replaced
	Kept       []Kept // records kept as they are, by the module's spec
}

// Kept is one kind of record a module keeps on deletion; the reason is documented in the
// module's spec and shown through the message key module.<name>.kept.<kind>.
type Kept struct {
	Kind  string // one ident
	Count int
}

// RouteProvider is implemented by a module with an HTTP API. The api shell mounts every
// route under /api/<name>/ with its middleware (session, CSRF, tracing) and the error
// mapper; handlers decode, call the service, encode (add-module). Permission checks
// happen in the service, never here.
type RouteProvider interface {
	// Routes returns the module's routes. Cannot fail: handlers are built already. Build
	// validates each route (F33) and keeps the list; api reads it from Set.Routes.
	Routes() []Route
}

// Route is one HTTP route relative to /api/<name>.
type Route struct {
	Method  string       // "GET", "POST", "PUT", "PATCH" or "DELETE"
	Pattern string       // net/http ServeMux path syntax, starting with "/", e.g. "/shifts/{id}"
	Access  Access       // who may reach it before any permission check
	Handler http.Handler // must not be nil
}

// Access says whether a route needs a signed-in person. The zero value is the safe one.
type Access uint8

const (
	AccessSignedIn Access = iota // a valid session is required (default)
	AccessPublic                 // reachable without a session, e.g. a published camp list
)

// Validate fails with errs.Invalid if Method is not one of the five, Pattern does not
// start with "/" or contains a space, Access is out of range, or Handler is nil.
func (r Route) Validate() error

// APIDescriber is implemented by a RouteProvider that documents its routes. The api shell
// merges the fragments into one OpenAPI document (api.md defines the fragment form).
type APIDescriber interface {
	// OpenAPI returns the module's fragment, embedded at build time. Cannot fail; Build
	// rejects an empty result (F34).
	OpenAPI() []byte
}

// CapabilityProvider is implemented by a module whose manifest lists Provides.
type CapabilityProvider interface {
	// Capabilities returns one implementation per capability in Manifest.Provides, keyed
	// by name; each value implements that contract's interface. Cannot fail; Build checks
	// the keys against the manifest (F29) and consumers assert the type (decision 5).
	Capabilities() map[Capability]any
}

// Contributor is implemented by a module whose manifest lists ContributesTo.
type Contributor interface {
	// Contributions returns one value per point in Manifest.ContributesTo. Cannot fail;
	// Build checks the keys (F30) and asks the definer's Accepts (F31).
	Contributions() map[ExtensionPoint]any
}

// ---- Deps and Links ----

// Deps is what the core gives a module. app builds one per module (Provision), scoped to
// it where that matters: the logger carries module=<name>; Signals, Settings and Jobs
// accept only what the manifest declared; Links sees only the module's own declarations.
// Fields typed by later specs are added when those specs are Approved; adding a field is
// not a breaking change for modules. A module validates the fields it uses.
type Deps struct {
	Logger *slog.Logger // scoped; never nil (Build checks)
	Clock  clock.Clock  // never nil (Build checks)
	Links  *Links       // set by Build, never by Provision

	DB      db.DB       // db.md (row 5): pool, transactions, read-only state
	Signals signals.Bus // signals.md (row 10): publish declared signals, subscribe to declared ones

	// Added by later specs, named here so modules know where to look:
	// Settings settings.Reader   settings.md (row 15)
	// Authz    authz.Authorizer  authz.md (row 7)
	// People   people.Directory  people.md (row 8)
	// Audit    audit.Recorder    audit.md (row 12)
	// Jobs     jobs.Registrar    jobs.md (row 14)
	// Notify   notify.Notifier   notify.md (row 11)
	// Files    files.Store       files.md (row 13)
}

// Links is a module's view of the modules it cooperates with. Build fills it: providers
// are built before their consumers, so Capability works from the Factory on;
// Contributors works from Start on, after every module is built. Safe for concurrent use
// once Build returns; the zero value fails every call with errs.Internal.
type Links struct{ /* unexported: owner manifest, providers, contributions, frozen */ }

// Capability returns the implementation of name, which the module asserts to the
// contract's interface. Fails with errs.Invalid if name is in neither Requires nor Uses of
// the owning manifest, and errs.NotFound if it is in Uses and no enabled module provides
// it (the module then hides the feature). A required capability never fails here:
// Resolve guaranteed its provider.
func (l *Links) Capability(name Capability) (any, error)

// Contributors returns every enabled module's contribution to point, in build order; each
// value passed the point's Accepts. Fails with errs.Invalid if point is not in the owning
// manifest's Defines, and errs.Internal if called before Build finished (that is, from a
// Factory).
func (l *Links) Contributors(point ExtensionPoint) ([]Contribution, error)

// Contribution is one module's value for an extension point.
type Contribution struct {
	Module string
	Value  any
}

// ---- Registry ----

// Registry holds the modules compiled into the binary. Default() is the process-wide one
// that Register adds to; NewRegistry gives tests their own. Safe for concurrent use.
type Registry struct{ /* unexported: mu, entries, problems */ }

// NewRegistry returns an empty registry. Cannot fail.
func NewRegistry() *Registry

// Default returns the process-wide registry. Cannot fail.
func Default() *Registry

// Register adds a module to Default(). It is called from a module package's init:
//   func init() { module.Register(Manifest, New) }
// and app imports the package for that side effect (D5). It never panics: a problem
// (F11–F13) is recorded and reported by Default().Resolve (F14), so a bad manifest stops
// start-up with a message instead of a stack trace.
func Register(m Manifest, f Factory)

// Add validates m and adds (m, f). Fails with errs.Invalid if f is nil or m fails
// Validate, and errs.Conflict if a module with m.Name was already added.
func (r *Registry) Add(m Manifest, f Factory) error

// Manifests returns every registered manifest, sorted by name, enabled or not. Cannot
// fail. For `bms check` and the admin graph.
func (r *Registry) Manifests() []Manifest

// Resolve checks the registry against env and returns the plan for the enabled modules:
// the build order and the graph. It is pure over the registry's contents and may be
// called more than once. Fails with errs.Invalid or errs.Conflict joining every problem
// found (F14–F23), so an operator sees them all at once.
func (r *Registry) Resolve(env Env) (*Plan, error)

// Env is what the core brings to the graph.
type Env struct {
	// Enabled names the modules to run, from config (app derives it: every registered
	// module minus the configured exclusions; decision 7).
	Enabled []string
	// CoreSignals are the signals the core publishes (people.person-created, ...), so a
	// module may subscribe to them.
	CoreSignals []signals.Name
	// CoreExtensionPoints are the points the core defines (people.person-page,
	// events.event-created, ...), so modules may contribute to them.
	CoreExtensionPoints []ExtensionPointDecl
}

// Validate fails with errs.Invalid if a name in Enabled is empty or repeated, a core
// signal name is invalid or not prefixed by a core area, or a core point fails Validate.
func (e Env) Validate() error

// Plan is a resolved, checked set of enabled modules. Immutable.
type Plan struct{ /* unexported */ }

// Order returns the enabled manifests in build order: a provider before every module that
// requires or uses it, ties broken by name. Cannot fail.
func (p *Plan) Order() []Manifest

// Permissions returns every enabled module's permission declarations, for authz. Cannot fail.
func (p *Plan) Permissions() []authz.PermissionDecl

// Settings returns every enabled module's setting declarations, keyed by module, for
// settings. Cannot fail.
func (p *Plan) Settings() []ModuleSettings

// ModuleSettings pairs a module name with its declared settings.
type ModuleSettings struct {
	Module   string
	Settings []SettingDecl
}

// Graph returns the whole registry as data for `GET /api/modules` and `bms check`:
// every compiled-in module, enabled or not, its declarations, the edges between them
// and the warnings Resolve produced. Cannot fail.
func (p *Plan) Graph() Graph

// Graph is the admin view of the module graph (09 §7). It is serializable; it carries no
// handlers, functions or file systems.
type Graph struct {
	Modules  []Node
	Edges    []Edge
	Warnings []Warning
}

// Node describes one compiled-in module.
type Node struct {
	Name          string
	Enabled       bool
	HasSchema     bool // Migrations != nil; db adds the versions (api.md)
	Permissions   []authz.PermissionDecl
	Settings      []SettingDecl // Validate funcs are not serialized
	Publishes     []signals.Name
	Subscribes    []signals.Name
	Provides      []Capability
	Requires      []Capability
	Uses          []Capability
	Defines       []ExtensionPoint
	ContributesTo []ExtensionPoint
}

// Edge is one dependency between two modules (or a module and the core, named by its
// area, e.g. "people").
type Edge struct {
	From string   // the module that requires, uses, contributes or subscribes
	To   string   // the provider, definer or publisher
	Kind EdgeKind
	Via  string   // the capability, extension point or signal name
}

// EdgeKind is the kind of cooperation an edge records.
type EdgeKind uint8

const (
	EdgeRequires EdgeKind = iota + 1
	EdgeUses
	EdgeContributes
	EdgeSubscribes
)

// String returns "requires", "uses", "contributes", "subscribes" or "EdgeKind(<n>)". Cannot fail.
func (k EdgeKind) String() string

// Warning is something Resolve allows but an operator should know (decision 6): a
// subscription whose only publishers are disabled, a contribution to a point whose
// definer is disabled, a used capability with no enabled provider.
type Warning struct {
	Module  string
	Message string // English, for logs and the admin screen; no personal data
}

// ---- Build ----

// SchemaChecker is what Build needs from db (D17): compare the schema version recorded
// for module with the one migrations is built for. db implements it (db.md).
type SchemaChecker interface {
	// CheckSchema fails with errs.Conflict if the recorded version is missing or differs,
	// with a message naming the module and both versions and saying to run `bms migrate`;
	// with errs.Unavailable or errs.Timeout if the database cannot be reached in time;
	// and with errs.Invalid if migrations is malformed.
	CheckSchema(ctx context.Context, module string, migrations fs.FS) error
}

// Provision builds the scoped dependencies for one module (app). It sets every field of
// Deps except Links.
type Provision func(ctx context.Context, m Manifest) (Deps, error)

// Build constructs the enabled modules in Order. For each module it checks the schema
// (when Migrations != nil), calls provision, fills Links, calls the factory, and verifies
// the module implements what its manifest declares. Schema errors for every module are
// collected before anything is built, so an operator sees all of them. Fails with the
// kind of the first failing step (F24–F36), wrapped with the module name; a built module
// is discarded (Stop is not called, since Start was not). Does not start anything.
func (p *Plan) Build(ctx context.Context, schema SchemaChecker, provision Provision) (*Set, error)

// Set is the built, enabled modules. Built once per process; Start and Stop are each
// called once by app, not concurrently with each other. The accessors are safe for
// concurrent use.
type Set struct{ /* unexported */ }

// Modules returns the built modules in build order. Cannot fail.
func (s *Set) Modules() []Built

// Built pairs a manifest with its module.
type Built struct {
	Manifest Manifest
	Module   Module
}

// Routes returns the validated routes of every RouteProvider, in build order, with each
// module's OpenAPI fragment (nil when it has none), for api. Cannot fail.
func (s *Set) Routes() []ModuleRoutes

// ModuleRoutes is one module's routes and API description.
type ModuleRoutes struct {
	Module  string
	Routes  []Route
	OpenAPI []byte
}

// PersonData returns every module's PersonData with its name, in build order, for people
// (export and deletion run over all of them). Cannot fail.
func (s *Set) PersonData() []ModulePersonData

// ModulePersonData pairs a module name with its PersonData.
type ModulePersonData struct {
	Module string
	PersonData
}

// Start calls each module's Start in build order. If one fails, the modules already
// started are stopped in reverse order with ctx, and the error is returned wrapped with
// the failing module's name, joined with any Stop errors (F37). Fails with errs.Conflict
// if Start already succeeded (F38).
func (s *Set) Start(ctx context.Context) error

// Stop calls each started module's Stop in reverse build order, continuing past
// failures, and returns them joined (F39). A Set that was never started, or already
// stopped, returns nil.
func (s *Set) Stop(ctx context.Context) error
```

```go
// Package moduletest proves a module keeps the module contract. Every module's test suite
// calls Conformance; the hello module is the reference.
package moduletest

// Conformance runs the contract checks against one module (see Test plan,
// "Conformance"). provision supplies the module's dependencies as fakes (db, signals and
// later fields); Conformance supplies Logger, Clock and Links. It fails t on the first
// broken rule with the rule's name.
func Conformance(t *testing.T, m module.Manifest, f module.Factory, provision module.Provision)
```

### Who needs each member

| Member | Consumer(s) | Pre-condition | Post-condition |
|---|---|---|---|
| `Capability`, `ExtensionPoint`, `ExtensionPointDecl` and their `Validate` | every module's manifest; `contracts/<name>` (exports the constant); `Manifest.Validate`; `Resolve` | — | one format each |
| `ExtensionPoint.Definer` | `Resolve` (who must exist), `Graph` edges | valid | prefix |
| `SettingKind`, `SettingDecl`, `Validate` | every module's manifest; `settings` (stores and edits); `Manifest.Validate` | — | `Default` parses and passes |
| `Manifest`, `Validate` | every module (`var Manifest = module.Manifest{...}`); `Registry.Add`; `app` (`migrate`, `check`); `db` (reads `Migrations` through `Plan.Order`) | — | every problem reported at once |
| `Factory` | every module (`func New(ctx, deps) (module.Module, error)`); `Register`; `Build` | — | a module or an error, never both nil |
| `Module` (`Start`, `Stop`) | `Set.Start`/`Stop` for `app` | built | started; stopped and waited |
| `PersonData`, `Export`, `Deletion`, `Kept` | every module (`privacy.go`); `people` (orchestrates); `moduletest` | actor authorized by `people` | all records, all levels; deletion idempotent |
| `RouteProvider`, `Route`, `Access`, `Route.Validate` | modules with an API; `Build` (validates); `api` (mounts, from `Set.Routes`) | handlers built | relative, validated routes |
| `APIDescriber` | modules with an API; `api` (merges fragments) | embedded | non-empty bytes |
| `CapabilityProvider` | providing modules; `Build` (checks keys); consumers through `Links` | built | one value per declared name |
| `Contributor` | contributing modules; `Build` (checks keys, `Accepts`); definers through `Links.Contributors` | built | accepted values only |
| `Deps` | every `Factory`; `Provision` in `app`; `Build` (sets `Links`, checks `Logger`, `Clock`) | — | scoped services |
| `Links.Capability` | consuming modules, in their `Factory` | provider built (order) | the impl, `NotFound` for an absent optional one |
| `Links.Contributors` | defining modules and the core's definers (`people` for the person page, `departments-events` for new-event copying), at request time | `Build` finished | accepted contributions, build order |
| `Registry`, `NewRegistry`, `Default`, `Register`, `Add`, `Manifests` | every module (`init`); `app`; tests | — | the compiled-in set |
| `Resolve`, `Env`, `Plan.Order` | `app` (`serve`, `migrate`, `check`); `db` (migrations in order) | registered | checked order |
| `Plan.Permissions` | `authz` through `app` | resolved | declared permissions |
| `Plan.Settings` | `settings` through `app` | resolved | declared settings |
| `Plan.Graph`, `Graph`, `Node`, `Edge`, `Warning` | `api` (`GET /api/modules`), `app` (`check`, logs warnings) | resolved | serializable |
| `SchemaChecker` | `Build`; implemented by `db` | — | D17 check |
| `Provision` | `app` | — | scoped `Deps` |
| `Plan.Build`, `Set` and accessors | `app`; `api` (`Routes`); `people` (`PersonData`) | resolved; database reachable | every module verified |
| `moduletest.Conformance` | every module's tests; `hello` | — | contract kept |

## Types

Defines: `Capability`, `ExtensionPoint`, `ExtensionPointDecl`, `SettingKind`, `SettingDecl`,
`Manifest`, `Factory`, `Module`, `PersonData`, `Export`, `Deletion`, `Kept`, `RouteProvider`,
`Route`, `Access`, `APIDescriber`, `CapabilityProvider`, `Contributor`, `Deps`, `Links`,
`Contribution`, `Registry`, `Env`, `Plan`, `ModuleSettings`, `Graph`, `Node`, `Edge`,
`EdgeKind`, `Warning`, `SchemaChecker`, `Provision`, `Set`, `Built`, `ModuleRoutes`,
`ModulePersonData`. Uses `model.PersonID`, `model.Sensitivity`, `clock.Clock`,
`validate.Errors`, `validate.FieldError`, `fs.FS`, `http.Handler`, `json.RawMessage`,
`slog.Logger`.

### Leaf types placed in `core/authz` and `core/signals`

`module` is the top of the core graph (it holds every core contract in `Deps`), so no core
package may import it. `authz` must read the declared permissions and `signals` the declared
names without importing `module`; therefore the name types live in those packages. They are
defined here because `authz.md` and `signals.md` are not yet written; those specs adopt them
unchanged (Proposed changes 2 and 3). Until then, each package holds exactly these types.

```go
package authz // internal/core/authz

// Permission names one thing a person may be allowed to do: "<module>.<ident>(.<ident>)*",
// for example "volunteers.shifts.manage". The first segment is the declaring module's
// name or a core area (people, events, departments, accounts, access, settings, audit,
// core). A permission is declared in code and granted by a regional (09 §4).
type Permission string

// Validate fails with errs.Invalid unless p has two or more ident segments.
func (p Permission) Validate() error

// Module returns the first segment. Cannot fail; meaningful once Validate passes.
func (p Permission) Module() string

// Scope says how a grant of the permission may be limited (09 §4).
type Scope uint8

const (
	ScopeSite       Scope = iota + 1 // granted site-wide only, e.g. events.manage
	ScopeDepartment                  // granted site-wide or for one department, e.g. volunteers.shifts.manage
)

// String returns "site", "department" or "Scope(<n>)". Cannot fail.
func (s Scope) String() string

// PermissionDecl is one declared permission. Its title and description are message keys
// derived from Name (module-contract decision 8), not text.
type PermissionDecl struct {
	Name    Permission
	Scope   Scope
	// Reaches is the highest sensitivity of any field the permission lets a person read
	// or change (model.md, Proposed change 3). authz shows it to access admins and uses
	// it in separation-of-duties rules; a permission that reaches sensitive data stands
	// out in the role editor.
	Reaches model.Sensitivity
}

// Validate fails with errs.Invalid joining a FieldError per problem: Name invalid, Scope
// zero or out of range, Reaches failing model.Sensitivity.Validate.
func (d PermissionDecl) Validate() error
```

```go
package signals // internal/core/signals

// Name identifies a signal (D24): "<publisher>.<ident>", for example
// "volunteers.shift-completed" or "people.person-created". The publisher is the module's
// name or a core area.
type Name string

// Validate fails with errs.Invalid unless n is "<ident>.<ident>".
func (n Name) Validate() error

// Publisher returns the first segment. Cannot fail; meaningful once Validate passes.
func (n Name) Publisher() string
```

### `Deps` fields by spec

| Field | Typed by | Scoped how | Available from |
|---|---|---|---|
| `Logger` | `log/slog` | `module=<name>` attribute; never logs personal data (go-conventions) | M0 |
| `Clock` | `lib-core` | shared | M0 |
| `Links` | this spec | the module's own declarations | M0 |
| `DB` | `db.md` | the module's schema is its own responsibility; `db` reports read-only | M0 |
| `Signals` | `signals.md` | publishing a name not in `Publishes`, or subscribing to one not in `Subscribes`, fails with `errs.Invalid` | M0 |
| `Settings` | `settings.md` | keys prefixed `<name>.`; reads only the module's declared keys and the core's retention keys for it | M2 |
| `Authz` | `authz.md` | shared; the module passes its own permission names | M2 |
| `People`, `Audit` | `people.md`, `audit.md` | shared; audit entries carry the module name | M2 / M1 |
| `Jobs` | `jobs.md` | job names prefixed `<name>.`; jobs run only on the writing copy | M4 |
| `Notify` | `notify.md` | templates prefixed `<name>.` | M4 |
| `Files` | `files.md` | shared | Later |

## Data

None. `module` owns no tables and no files. The table of schema versions per module is
`db`'s (D17); the settings values are `settings`'; grants are `authz`'s. Nothing here holds
personal data: `Export` and `Deletion` pass through `people`, which audits them, and are
never logged by this package (the `Logger` rule in go-conventions); `moduletest`'s
`no_personal_data_in_logs` checks the module side.

## Permissions

`module` performs no operation on behalf of a person: `Register`, `Resolve`, `Build`, `Start`
and `Stop` run at process start-up and shutdown with no actor. The two operations other
specs expose on this spec's data:

| Permission | Allows | Scoped by | Default roles | Declared by |
|---|---|---|---|---|
| `core.modules.read` | read the module graph and schema versions (`GET /api/modules`, 09 §7) | site | every site-admin starter role | `api.md` / `authz.md` (Proposed change 6) |
| `people.data.export`, `people.data.delete` | run `PersonData` across modules for one person | site | account admin (09 §11) | `people.md` (Proposed change 8) |

Every module's own routes name their permissions in the module's spec.

## Signals

None published, none subscribed by this package. It validates the names modules declare,
passes `Env.CoreSignals` through to the check, and tells `signals` (through `app`) which
names each module declared so the scoped `Deps.Signals` can enforce them.

## Settings

None of its own. Which modules are enabled is **process configuration** (`config.md`), not a
runtime setting, because the schema an instance may touch must be fixed before `bms migrate`
runs (D17) and must not change while a replica serves reads (Q-OFFLINE). Proposed knob
(Proposed change 10): `modules.disabled`, a list of names, default empty, so every
compiled-in module is on unless named (decision 7); a name not compiled in fails config
validation. Retention settings per module and level (D30) are added by `settings` for every
module, with the default "keep forever"; a module needs to declare nothing for them.

Regional differences inside a module are that module's settings; this contract gives them
one declaration (`SettingDecl`) with a default, a kind and validation, so `settings` can
show and check every module's settings the same way.

## UI

None of its own. What this spec provides for screens elsewhere:

- **Admin "modules" screen** (`web-shell.md`, 09 §7): `GET /api/modules` (`api.md`) serves
  `Plan.Graph()` plus, per module with a schema, the built-for and applied versions from
  `db`. Visible with `core.modules.read`. Empty state: no modules compiled in (the graph has
  only core nodes). Forbidden: 403 from the api; the shell hides the menu entry.
- **Module screens** (D31): the web shell's build-time registry imports each module's
  folder; it shows a module's navigation and screens only if the module's name is enabled
  in `GET /api/modules`. A disabled module's screens are not mounted, so turning a module
  off hides its UI without a rebuild (add-module §3).

## Behavior

### Decisions settled here, with reasons

1. **The manifest is a static value and the module is built separately.** `bms migrate`,
   `bms check` and the graph need every module's name, migrations and declarations before
   any dependency exists, and the graph check must run before anything is built. So
   `Register(Manifest, Factory)` takes both: the value and the constructor. (go-conventions
   wrote `module.Register(volunteers.New)`; Proposed change 11 updates it.)
2. **`Register` records problems instead of panicking.** It runs in `init`, where no caller
   can handle an error, and go-conventions reserves `panic` for impossible states. A bad
   manifest is not impossible; it is a contributor's mistake, and the operator should see
   "module volunteers: permissions[1].name: must start with volunteers." at start-up, not a
   stack trace. `Default().Resolve` reports every recorded problem together (F14).
3. **`module` is the top of the core graph.** `Deps` holds every core contract a module may
   use, so `module` imports them and nothing in `core/` may import `module`. That is why
   the permission and signal name types live in `authz` and `signals` (Types), and why
   `authz` and `settings` receive declarations through `app` rather than reading the
   registry.
4. **Capabilities resolve at construction; extension points resolve per request.** A
   capability is a dependency a module holds as a field, so its provider is built first
   (`Plan.Order` is topological over `Requires` and `Uses`; a cycle is an error, F19) and
   the consumer asserts the type once in its `Factory`, failing start-up with a clear
   message if the provider is the wrong type. An extension point is a question ("who has a
   section for this page?") whose answer must follow the enabled set, so `Links.Contributors`
   is called at request time against a table frozen after `Build`; that also means
   contributors and definers may depend on each other without a cycle problem.
5. **Types are checked at start-up, by the side that knows the interface.** `module` cannot
   import `contracts/*` (it would then know every contract), so it carries `any`. A
   consumer asserts a capability in its `Factory`; a definer supplies `Accepts` in its
   `ExtensionPointDecl` and `Build` runs it over every contribution (F31), naming both
   modules on a mismatch. No wrong type reaches a request.
6. **A dangling subscription or contribution is a warning, not an error, when its other
   side is compiled in but disabled; it is an error when nothing compiled in declares
   it.** D19 asks the registry to check that every subscribed signal has a publisher; D19
   also says modules should prefer optional cooperation and hide what is missing. Failing
   start-up would force every subscriber to require its publisher, which is the coupling
   D19 avoids; silently accepting an unknown name would hide typos. So: unknown name →
   `Invalid` (F20, F21); known but disabled → `Warning`, logged at warn by `app` and shown
   in the graph. Confirmed by the owner (2026-10-16).
7. **Every compiled-in module is enabled unless configuration names it.** D5 says a fork
   adds a module with one import line; an opt-in list would make that two edits and a
   silent no-op when forgotten. Enablement is opt-out via `modules.disabled`. Confirmed by
   the owner (2026-10-16).
8. **The manifest carries no human-readable text; every title and description is a message
   key derived from the name, held in the module's web catalogue.** D28 says every string
   is translation-ready from day one; an English `Description` in Go would be the one
   string that is not. The keys are fixed by convention, so there is nothing to declare:
   `module.<name>.title` and `.description`; `module.<name>.permission.<rest>.title` and
   `.description` (`<rest>` is the permission name after `<name>.`);
   `module.<name>.setting.<key>.title`, `.description` and `.option.<opt>`;
   `module.<name>.signal.<rest>.title`; `module.<name>.kept.<kind>`. `web-shell.md` owns the
   catalogue files and checks that `en` has every key the module's declarations imply
   (Proposed change 7). `Warning.Message` and error messages are English, like logs: they
   are for operators. Confirmed by the owner (2026-10-16).
9. **The manifest does not say whether a module has UI.** The core would do nothing with
   the flag; the web shell's build-time registry (D31) is the source of truth for screens,
   and it shows a module only when both registries have it (UI section). The two cannot
   disagree silently: a Go module without a web folder simply has no screens; a web folder
   without an enabled Go module is not shown.
10. **Module names are letters and digits only.** One name is the Go package, the
    PostgreSQL schema (unquoted), the URL segment, the permission prefix, the web folder and
    the message-key segment; hyphens and underscores are legal in some of those and not
    others, and a mapping between spellings is a bug waiting to happen. So the module the
    plan calls `after-reports` is `afterreports` in code, schema, URL and keys; the plan's
    prose may keep the hyphen. Confirmed by the owner (2026-10-16).
11. **`PersonData` is mandatory and part of `Module`, not an extension point.** privacy-data
    says every module answers export and delete; an optional contribution could be
    forgotten. Making it part of the interface means a module does not compile without it,
    and `moduletest` proves the empty case for a module without personal data.
12. **The schema check is done by `Build`, once for all modules.** D17 says each module
    checks its version when it initializes; doing it in the contract, before the factory
    runs, satisfies that for every module with one implementation and one test, and lets an
    operator see every out-of-date module in one message rather than one per restart.
13. **Routes are a list, not one handler.** The api shell must know which routes are public
    (D15: "modules declare their own public and private routes in one place") and must
    build the OpenAPI document; one opaque `http.Handler` hides both. The default `Access`
    is signed-in, so a forgotten field is the safe one.
14. **Start never writes.** The MVP guarantees that every instance can be a read-only copy
    (09 ground rules, 04 "Read-only mode"). A module that seeded rows or wrote a heartbeat
    in `Start` would fail to start on a replica. Writes happen in requests and in jobs,
    which `jobs` runs only on the writing copy; a write on a read-only copy fails with the
    kind `db` returns, which the api maps to "this copy is read-only". Nothing in this
    contract is special for read-only mode beyond this rule, and `moduletest` proves it with
    a fake `DB` that rejects writes.
15. **Contract calls take batches and providers check permissions for the person asking.**
    Restated from D19 as an obligation on every `Capabilities()` and `Contributions()`
    value: a method takes a list of IDs, not one; the provider applies its own permission
    checks for the actor in `ctx`, so nothing leaks through a contract; and payloads carry
    IDs, never personal data, unless the contract's spec says so and names the level.

### Reserved module names

`Manifest.Validate` rejects: the core areas (`core`, `people`, `accounts`, `access`,
`identity`, `authz`, `departments`, `events`, `signals`, `notify`, `audit`, `files`, `jobs`,
`settings`, `telemetry`, `db`, `module`, `modules`), the shell packages (`api`, `app`,
`config`, `web`, `admin`, `client`), and PostgreSQL's (`public`, `information`, anything
starting with `pg`). The list is a constant in the package and a test pins it.

### Resolve, step by step

1. `env.Validate()`; collect recorded `Register` problems (F14).
2. Every name in `Enabled` is registered (F15), none repeated (F16).
3. Capabilities: for each enabled module's `Requires`, exactly one enabled provider (F17,
   F18); for each capability, at most one enabled provider even if nobody uses it (F18);
   `Uses` with no provider → warning.
4. Order: topological sort over `Requires` and `Uses` edges among enabled modules, ties by
   name; a cycle → F19 naming the modules on it.
5. Signals: each subscribed name's publisher is a core area in `CoreSignals` or a registered
   module that publishes it (F20); if that module is disabled → warning.
6. Extension points: each `ContributesTo` names a point in `CoreExtensionPoints` or in a
   registered module's `Defines` (F21); if that module is disabled → warning. Two definers
   of one point cannot happen (names are prefixed by definer; F9).
7. Produce `Plan` with the order, the graph (every registered module, enabled or not) and
   the warnings. All problems are joined into one error with `validate.Errors`, op
   `module.Registry.Resolve`.

### Build, step by step

1. For every module in order with `Migrations != nil`: `schema.CheckSchema`; collect every
   error (F26) and stop if any.
2. For each module in order: `provision(ctx, m)` (F24); check `Logger` and `Clock` (F25); set
   `Links` to the module's view with providers built so far; call the factory (F27, F28).
3. Verify: `Capabilities()` keys equal `Provides` and no value is nil (F29); `Contributions()`
   keys equal `ContributesTo` (F30); `Routes()` each validate and no two share method and
   pattern (F33); `OpenAPI()` non-empty (F34); a module implementing `CapabilityProvider` or
   `Contributor` with declarations empty is F29/F30 too.
4. After all are built: for every contribution, the definer's `Accepts` (F31); freeze every
   `Links`.
5. Return `Set`. `ctx` done at any step → `Canceled` or `Timeout` (F35).

### Concurrency and lifecycle

`Registry` is guarded by a mutex; `Register` and `Add` may be called from any goroutine
(`init` is sequential in practice, tests may be parallel). `Plan`, `Links` (after `Build`)
and `Set`'s accessors are immutable and safe for concurrent use. `Build`, `Start` and `Stop`
are called once each by `app`, in that order, never concurrently; `Set` guards against a
second `Start` (F38). `Set` owns no goroutines: each module's `Start` launches its own and
`Stop` waits for them, within the `ctx` `app` passes (app.md sets the deadlines; proposed 30 s
to start, 15 s to stop). `Stop` continues past a failing module so the rest still stop.

### I/O

None in this package: the only I/O is what `SchemaChecker`, `Provision`, factories, `Start`
and `Stop` do, through the `ctx` they receive. No timeouts are added here; callers derive
them.

### Imports

Standard library (`context`, `encoding/json`, `errors`, `fmt`, `io/fs`, `log/slog`,
`net/http`, `regexp`, `sort`, `sync`), `internal/lib/errs`, `internal/lib/validate`,
`internal/lib/clock`, `internal/model`, `internal/core/authz`, `internal/core/signals`,
`internal/core/db`. Never `internal/contracts`, a module, `api`, `app` or `config`
(depguard rule `core`). `moduletest` adds `testing` and `internal/lib/testkit`.

## Failure modes

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F1 | `Manifest.Validate` | `Name` empty, not `[a-z][a-z0-9]{1,31}`, or reserved | Invalid, field `name` | fix the module | `TestManifestValidate_BadOrReservedName_ReturnsInvalid` |
| F2 | `Manifest.Validate` | a permission name fails `authz.Permission.Validate` or does not start with `<Name>.` | Invalid, `permissions[i].name` | fix the module | `TestManifestValidate_PermissionNotPrefixed_ReturnsInvalid` |
| F3 | `Manifest.Validate` | a permission's `Reaches` fails `model.Sensitivity.Validate` (model.md Proposed change 3) | Invalid, `permissions[i].reaches` | fix the module | `TestManifestValidate_PermissionReachesUnset_ReturnsInvalid` |
| F4 | `Manifest.Validate` | a permission's `Scope` is zero or out of range | Invalid, `permissions[i].scope` | fix the module | `TestManifestValidate_PermissionScopeInvalid_ReturnsInvalid` |
| F5 | `Manifest.Validate` | duplicate permission, setting key, signal, capability or point within the manifest | Invalid, the second's field | fix the module | `TestManifestValidate_Duplicates_ReturnInvalid` |
| F6 | `Manifest.Validate`, `SettingDecl.Validate` | setting: key not an ident; kind invalid; options wrong for the kind; default fails the kind's parse or `Validate` | Invalid, `settings[i].<field>` | fix the module | `TestSettingDeclValidate_Table` |
| F7 | `Manifest.Validate` | a published signal fails `signals.Name.Validate` or its publisher is not `Name`; a subscribed signal fails `Validate` | Invalid, `publishes[i]` / `subscribes[i]` | fix the module | `TestManifestValidate_SignalNames_ReturnInvalid` |
| F8 | `Manifest.Validate` | a capability fails `Validate`; a name in both `Requires` and `Uses`; a name in `Provides` and in `Requires` or `Uses` | Invalid, `requires[i]` etc. | fix the module | `TestManifestValidate_CapabilityConflicts_ReturnInvalid` |
| F9 | `Manifest.Validate` | a defined point's definer is not `Name`, or its `Accepts` is nil; a `ContributesTo` fails `Validate` | Invalid, `defines[i]` / `contributesTo[i]` | fix the module | `TestManifestValidate_ExtensionPoints_ReturnInvalid` |
| F10 | `Manifest.Validate` | every problem above at once | Invalid joining all of them (`validate.Fields` lists each) | fix them all in one go | `TestManifestValidate_ReportsAllProblems` |
| F11 | `Registry.Add` | `f == nil` | Invalid | fix the module | `TestRegistryAdd_NilFactory_ReturnsInvalid` |
| F12 | `Registry.Add` | `m.Validate()` fails | Invalid (wraps F1–F10) | fix the module | `TestRegistryAdd_InvalidManifest_ReturnsInvalid` |
| F13 | `Registry.Add` | a module named `m.Name` is already registered | Conflict | remove one import | `TestRegistryAdd_DuplicateName_ReturnsConflict` |
| F14 | `Register` then `Default().Resolve` | `Add` would have failed F11–F13 | the recorded kind, reported by `Resolve` | stop start-up, show the message | `TestRegister_RecordsProblem_ResolveReportsIt` |
| F15 | `Registry.Resolve` | a name in `Enabled` is not registered | Invalid, naming it and the registered names | fix config | `TestResolve_UnknownEnabledName_ReturnsInvalid` |
| F16 | `Registry.Resolve`, `Env.Validate` | `Enabled` repeats a name, or is empty-string; a core signal or point is invalid | Invalid | fix config / app | `TestResolve_EnvInvalid_ReturnsInvalid` |
| F17 | `Registry.Resolve` | a required capability has no enabled provider | Invalid, naming the capability, the requirer and any disabled provider | enable the provider or disable the requirer | `TestResolve_RequiredCapabilityMissing_ReturnsInvalid` |
| F18 | `Registry.Resolve` | two enabled modules provide one capability | Conflict, naming both | disable one | `TestResolve_TwoProviders_ReturnsConflict` |
| F19 | `Registry.Resolve` | a cycle in `Requires`/`Uses` among enabled modules | Invalid, naming the cycle | redesign (send to architect) | `TestResolve_CapabilityCycle_ReturnsInvalid` |
| F20 | `Registry.Resolve` | a subscribed signal that neither the core nor any registered module publishes | Invalid | fix the name | `TestResolve_SubscribeUnknownSignal_ReturnsInvalid` |
| F21 | `Registry.Resolve` | a contribution to a point neither the core nor any registered module defines | Invalid | fix the name | `TestResolve_ContributeUnknownPoint_ReturnsInvalid` |
| F22 | `Registry.Resolve` | several of F15–F21 at once | one error joining all | fix them all | `TestResolve_ReportsAllProblems` |
| F23 | `Registry.Resolve` | publisher or definer registered but disabled; used capability with no provider | none: `Warning` in the plan | `app` logs at warn | `TestResolve_DisabledOtherSide_Warns` |
| F24 | `Plan.Build` | `provision` fails | its kind, wrapped with the module name | stop start-up | `TestBuild_ProvisionFails_WrapsWithModule` |
| F25 | `Plan.Build` | `Deps.Logger` or `Deps.Clock` nil after `provision` | Invalid | fix app | `TestBuild_DepsMissingLoggerOrClock_ReturnsInvalid` |
| F26 | `Plan.Build` | `CheckSchema` fails for one or more modules | the checker's kind (Conflict for a version mismatch, Unavailable, Timeout), all joined, nothing built | run `bms migrate` / fix the database | `TestBuild_SchemaMismatch_ReportsEveryModuleAndBuildsNothing` |
| F27 | `Plan.Build` | a factory returns an error | its kind, wrapped with the module name | stop start-up | `TestBuild_FactoryFails_WrapsWithModule` |
| F28 | `Plan.Build` | a factory returns `(nil, nil)` | Internal | fix the module | `TestBuild_FactoryReturnsNilModule_ReturnsInternal` |
| F29 | `Plan.Build` | `Provides` non-empty but no `CapabilityProvider`; keys differ from `Provides`; a value nil; `CapabilityProvider` with non-empty map but empty `Provides` | Invalid, naming the module and capability | fix the module | `TestBuild_CapabilitiesMismatchManifest_ReturnsInvalid` |
| F30 | `Plan.Build` | the same for `ContributesTo` and `Contributor` | Invalid | fix the module | `TestBuild_ContributionsMismatchManifest_ReturnsInvalid` |
| F31 | `Plan.Build` | a contribution fails the definer's `Accepts` | Invalid, naming contributor, definer and point | fix the contributor | `TestBuild_ContributionRejectedByDefiner_ReturnsInvalid` |
| F32 | consumer `Factory` (obligation) | a capability value does not implement the contract interface | Invalid from the consumer, naming the capability | stop start-up | `TestBuild_ConsumerRejectsProviderType_FailsStartup` (with a fake consumer) |
| F33 | `Plan.Build`, `Route.Validate` | a route with a bad method, pattern not starting with `/`, nil handler, out-of-range `Access`, or two routes with the same method and pattern | Invalid, naming module and route | fix the module | `TestBuild_InvalidOrDuplicateRoute_ReturnsInvalid` |
| F34 | `Plan.Build` | `OpenAPI()` returns empty | Invalid | fix the module | `TestBuild_EmptyOpenAPI_ReturnsInvalid` |
| F35 | `Plan.Build` | `ctx` done | Canceled / Timeout | stop start-up | `TestBuild_ContextCanceled_ReturnsCanceled` |
| F36 | `Plan.Build` | `schema` or `provision` nil | Invalid | fix app | `TestBuild_NilSchemaOrProvision_ReturnsInvalid` |
| F37 | `Set.Start` | a module's `Start` fails | its kind, wrapped with the module name; earlier modules stopped in reverse; their `Stop` errors joined | stop start-up | `TestSetStart_ModuleFails_StopsEarlierInReverse` |
| F38 | `Set.Start` | already started | Conflict | fix app | `TestSetStart_Twice_ReturnsConflict` |
| F39 | `Set.Stop` | one or more modules' `Stop` fail | joined, all modules still stopped; `Timeout` if `ctx` ends | log and exit | `TestSetStop_ContinuesPastFailuresAndJoins` |
| F40 | `Links.Capability` | name not in the owner's `Requires` or `Uses` | Invalid | fix the module (declare it) | `TestLinksCapability_Undeclared_ReturnsInvalid` |
| F41 | `Links.Capability` | in `Uses`, no enabled provider | NotFound | hide the feature | `TestLinksCapability_UsedButAbsent_ReturnsNotFound` |
| F42 | `Links.Contributors` | point not in the owner's `Defines` | Invalid | fix the module | `TestLinksContributors_Undeclared_ReturnsInvalid` |
| F43 | `Links.Contributors`, `Links.Capability` | called before `Build` froze the links (`Contributors` from a factory), or on a zero `Links` | Internal | move the call to `Start` or request time | `TestLinks_BeforeFreezeOrZero_ReturnsInternal` |
| F44 | `PersonData.ExportPerson`, `DeletePerson` (obligation) | zero `PersonID` | Invalid | 400 (people) | `moduletest`: `Conformance/export_zero_id`, `delete_zero_id` |
| F45 | `PersonData.DeletePerson` (obligation) | database read-only | the kind `db` returns for a write | "this copy is read-only" | `moduletest`: `Conformance/delete_read_only` |
| F46 | `Module.Start` (obligation) | database read-only | none: `Start` succeeds | — | `moduletest`: `Conformance/start_read_only` |
| F47 | `Module.Stop` (obligation) | `ctx` already done | Timeout or Canceled within the deadline, never a hang | log | `moduletest`: `Conformance/stop_honors_context` |

Members that cannot fail, and why: `Definer`, `Publisher`, `Permission.Module` (string
split); the `String` methods (name or `Kind(<n>)`); `NewRegistry`, `Default` (allocation);
`Manifests`, `Order`, `Permissions`, `Settings`, `Graph`, `Modules`, `Routes`, `PersonData`
(copies of validated data); `Routes()`, `OpenAPI()`, `Capabilities()`, `Contributions()` on a
module (return values built already; `Build` validates them). Each has a success-path test.

## Lock-in check

No external technology. The registry is plain Go (the same shape as `database/sql`'s driver
registry); routes use `net/http`; migrations are an `fs.FS`; the graph is plain data. Nothing
depends on PostgreSQL, pgx or any encoder.

## Test plan

Unit tests only for this package, no infrastructure, `go test -race`, table-driven, package
`module_test`, with hand-written fake modules, a fake `SchemaChecker` and a fake `Provision`
in the test files (no mocking frameworks, D14). Invented module names (`alpha`, `beta`,
`gamma`); no personal data anywhere.

- Names: `TestIdentGrammar_Table` (accepts `a1`, `shift-completed`; rejects `Shift`, `a_b`,
  `-a`, `a-`, 65 bytes); `TestCapability_ExtensionPoint_Permission_SignalName_Validate` (one
  table per type); `TestReservedNames_Pinned` (the list is stable).
- Manifest: `TestManifestValidate_MinimalAndFull_Pass` (a name-only manifest; a manifest
  using every field); F1–F10.
- Registry: `TestRegistryAdd_ThenManifests_SortedByName`; `TestRegister_AddsToDefault`
  (uses a fresh name and checks `Default().Manifests()`); `TestRegistry_ConcurrentAdd` (under
  `-race`); F11–F14.
- Resolve: `TestResolve_OrderIsTopologicalAndStable` (providers first, ties by name, same
  order across runs); `TestResolve_DisabledModuleAppearsInGraphNotOrder`;
  `TestResolve_EdgesAndKinds` (one edge per requires/uses/contributes/subscribes, core
  edges named by area); `TestResolve_CoreSignalsAndPointsSatisfyChecks`; F15–F23.
- Build: `TestBuild_CallsSchemaThenProvisionThenFactoryInOrder` (a recording fake);
  `TestBuild_SkipsSchemaCheckWhenNoMigrations`; `TestBuild_LinksCapabilityWorksInFactory`
  (consumer gets the provider's value); `TestBuild_LinksContributorsWorksAfterBuild` (and
  F43 before); `TestBuild_ModuleWithoutOptionalInterfaces_Passes` (a `Module` that only
  implements `PersonData`, `Start`, `Stop`); `TestBuild_EmptyPlan_ReturnsEmptySet`;
  F24–F36.
- Set: `TestSetStartStop_OrderAndReverse` (recording fake, `Stop` reverse of `Start`);
  `TestSetStop_WithoutStart_IsNil`; `TestSetStop_Twice_IsNil`;
  `TestSetRoutesAndPersonData_InBuildOrder`; F37–F39.
- Links: F40–F43, plus `TestLinks_SafeForConcurrentUse` (under `-race`).
- Graph: `TestGraph_IsJSONSerializable` (`encoding/json` round trip; `Settings` without
  `Validate`, `Defines` as names only).
- Lint: passes depguard rules `core` and `storage-seam` (no driver import), `errcheck`,
  `errorlint`, `wrapcheck`, `sloglint`.

**Conformance** (`moduletest.Conformance`, run by every module and by the `hello` module as
the reference; each rule is a named subtest): `manifest_valid` (`Validate` passes);
`registers_under_its_name` (a fresh registry `Add` then `Resolve` with only this module
enabled and its required capabilities faked succeeds); `implements_declared`
(`Build` passes F29–F34 against the manifest); `start_read_only` (F46, with a fake `DB`
that fails every write with the read-only kind); `stop_honors_context` (F47);
`stop_without_start_is_nil`; `export_zero_id`, `delete_zero_id` (F44);
`export_unknown_person_is_empty` and `delete_unknown_person_is_zero_and_idempotent` (with
an empty fake store); `delete_read_only` (F45); `export_is_json_object` (`Data` empty or
parses as an object); `routes_under_prefix` (patterns start with `/`; no absolute `/api/`);
`no_personal_data_in_logs` (a capturing `slog.Handler`; the test runs `Start`, one export
and one delete of an invented person and asserts no log record contains the fake e-mail or
legal name the test's fake store holds). Integration of a real module end to end (routes
mounted, migrations applied, signals delivered) is `app.md`'s and the `hello` module's
integration test, behind the `integration` tag.

### The `hello` reference module (09 §1)

Specified separately (`module-hello.md`, Proposed change 12). To "exercise every part of
the contract" it must: declare one permission (`hello.greetings.read`, `ScopeSite`,
`Reaches` internal), one setting (`greeting`, text, default `"hello"`), publish
`hello.greeted`, subscribe to `hello.greeted` and to one core signal once the core publishes
one, provide capability `greeter` (`contracts/greeter`), define point `hello.banner`,
contribute to its own point, ship one migration creating `hello.greeting`, serve `GET
/greeting` (signed in) and `GET /public-greeting` (public) with an OpenAPI fragment,
implement `PersonData` over the greetings a person saved, start one goroutine, and pass
`Conformance`.

## Open questions

None. The four questions in the first draft were answered by the owner on 2026-10-16, each
with the proposed answer:

1. **Subscribing to a signal whose publisher is compiled in but disabled.** Answered: allowed,
   with a warning in the log and the graph, not a start-up error; an unknown name is still
   `Invalid`. Recorded in decision 6 and failure modes F20 and F21.
2. **Enabling modules: opt-out or opt-in?** Answered: opt-out. Every compiled-in module is
   enabled unless named in `modules.disabled`. Recorded in decision 7 and Proposed change 10
   (`config.md`).
3. **No English text in Go manifests.** Answered: no English text. Titles and descriptions
   are message keys fixed by convention, with the text in the module's web catalogue.
   Recorded in decision 8 and Proposed changes 7 and 11.
4. **Module names are letters and digits only.** Answered: yes. `after-reports` is
   `afterreports` in code, schema, URLs and keys; the plan's prose may keep the hyphen.
   Recorded in decision 10 and Proposed changes 11 and 14.

## Proposed changes elsewhere

1. **`plan/07-abstractions.md`, `module` entry:** replace "Owns" with: the `Manifest`
   (static declaration), the `Module` interface and its optional interfaces, `Deps` (what the
   core gives a module), the registry, `Resolve` (graph check, build order, graph for the
   admin screen), `Build` (schema check per D17, verification against the manifest,
   linking), `Set` (start and stop in order), and the mandatory `PersonData` (export and
   deletion of one person's data). Add to the layer rules: "`core/module` is the top of the
   core graph: it imports core contracts; no core package imports it." Add to the `authz`
   entry: owns the `Permission`, `Scope` and `PermissionDecl` types; to the `signals` entry:
   owns the `Name` type. Note that module names are letters and digits only (`afterreports`).
2. **`plan/design/authz.md` (row 7):** adopt `authz.Permission`, `authz.Scope` and
   `authz.PermissionDecl` as defined in this spec's "Types"; receive `Plan.Permissions()`
   from `app` at start-up; show `Reaches` in the role editor and allow separation-of-duties
   rules to refer to it ("may never hold a permission reaching sensitive"); declare
   `core.modules.read` (or let `api.md` do it).
3. **`plan/design/signals.md` (row 10):** adopt `signals.Name` as defined here; the bus
   handed to a module through `Deps.Signals` is scoped by `app` to the module's `Publishes`
   and `Subscribes` and fails with `errs.Invalid` outside them; the core's published names
   are listed for `Env.CoreSignals`; subscriptions are registered in `Module.Start`.
4. **`plan/design/db.md` (row 5):** implement `module.SchemaChecker` (message names the
   module and both versions, says to run `bms migrate`; `Conflict` on mismatch or missing
   row); define the migration file layout for `Manifest.Migrations`
   (`postgres/migrations/NNNN_name.sql`, highest number is the version); `bms migrate`
   applies the core first, then `Plan.Order()`; name the error kind a write gets on a
   read-only copy (proposed `Unavailable` with a sentinel `db.ErrReadOnly`, so the api can
   say "this copy is read-only" and `retry` would not hammer it); expose read-only state for
   `/readyz`. The `db` integration test proves pgx scans `model.ID[E]` (model.md Proposed
   change 4) and that `CheckSchema` works on an empty database.
5. **`plan/design/settings.md` (row 15):** consume `Plan.Settings()` (`SettingDecl`: kinds,
   defaults, options, `Validate`); store values under `<module>.<key>`; add the retention
   settings per module and sensitivity level automatically (D30, default keep forever);
   give modules a scoped `Deps.Settings`; show titles and descriptions through the message
   keys of decision 8. Consider department-scoped settings later; this spec declares
   site-wide ones only.
6. **`plan/design/api.md` (row 16):** mount `Set.Routes()` under `/api/<name>/` with the
   session middleware applied per `Route.Access`; define the OpenAPI fragment form for
   `APIDescriber` and merge the fragments; serve `GET /api/modules` from `Plan.Graph()` plus
   `db`'s versions, behind `core.modules.read`; map `db`'s read-only kind to the
   "this copy is read-only" response.
7. **`plan/design/web-shell.md` (row 17):** each module's folder ships
   `messages/<lang>.json` with the keys of decision 8; a build-time check (or a test) that
   `en` has every key implied by the module's declarations (the build can read the Go
   manifest through a generated JSON, or the check runs in Go against the catalogue files);
   show a module's navigation and screens only when `GET /api/modules` lists it as enabled;
   the admin "modules" screen renders the graph with its warnings.
8. **`plan/design/people.md` (row 8):** orchestrate export and deletion over
   `Set.PersonData()` behind `people.data.export` and `people.data.delete`, in one audited
   operation; assemble `{ "<module>": <Export.Data>, ... }`; report `Deletion` per module;
   declare the person-page extension point (`people.person-page`, with its `Accepts`) in
   `Env.CoreExtensionPoints` and the signals the core publishes (`people.person-created`,
   `people.person-deleted`) in `Env.CoreSignals`; call `Links.Contributors` at request time.
   `departments-events.md` (row 9) likewise declares `events.event-created` as the extension
   point for copying into a new event (09 §5).
9. **`plan/design/app.md` (row 18):** `serve`: `Default().Resolve(env)` → log warnings →
   `Build` (with `db` as the checker and a `Provision` that builds scoped `Deps`) → hand
   `Plan.Permissions()` to `authz`, `Plan.Settings()` to `settings`, `Set.Routes()` to `api`,
   `Set.PersonData()` to `people` → `Set.Start` → serve → `Set.Stop` with deadlines (proposed
   30 s and 15 s). `check`: `Resolve` plus `CheckSchema` per enabled module, no build.
   `migrate`: `Resolve` then `db` over `Plan.Order()`. `export`/`delete` by command (09
   §11): through `people`.
10. **`plan/design/config.md` (row 3):** `modules.disabled` (list of module names, default
    empty, each must be registered; `BMS_MODULES_DISABLED` as a comma-separated list);
    `app` derives `Env.Enabled`. Opt-out enablement confirmed by the owner (2026-10-16;
    decision 7).
11. **`.claude/skills/go-conventions/SKILL.md` and `.claude/skills/add-module/SKILL.md`:**
    the registration line is `module.Register(Manifest, New)` in `init`, with
    `var Manifest = module.Manifest{...}` and `func New(ctx context.Context, deps module.Deps)
    (module.Module, error)` in `module.go`; `privacy.go` implements `module.PersonData`;
    `Start` registers signal handlers and jobs and never writes; module names are letters
    and digits only; titles and descriptions are catalogue keys (decision 8); every module's
    tests call `moduletest.Conformance`. `add-module` §1 adds "the Reaches level of each
    permission" to the spec checklist.
12. **`plan/design/README.md`:** add row `4b` `module-hello.md`, "the reference module
    (M0)", Not started; it depends on this spec, `db`, `signals` and `api`.
13. **`plan/design/jobs.md` (row 14) and `notify.md` (row 11):** `Deps.Jobs` and
    `Deps.Notify` are scoped to the module (job and template names prefixed `<name>.`); jobs
    are registered in `Module.Start` and run only on the writing copy.
14. **`plan/02-scope.md`, `plan/09-core-features.md`:** note that `after-reports` is
    `afterreports` in code, schema and URLs (confirmed by the owner, 2026-10-16; decision
    10); 09 §4's "a
    flag for whether they reach sensitive data" becomes "the highest sensitivity level they
    reach" (model.md Proposed change 3).
