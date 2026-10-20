# module: the module contract and registry

- **Status:** Implemented (2026-10-17); Approved (2026-10-16)
- **Amended 2026-10-17 (owner):** `moduletest.Conformance` takes options (`WithCapability`, `WithSeed`/`WithoutPersonData`) and gains five rules; `Set.Start` rolls back under `StartRollbackTimeout`; `SettingDecl.Check` replaces `Validate`; `module.IsCoreArea` exported; wording fixes (Graph edges, Resolve kind, Build step 3).
- **Amended 2026-10-18 (owner approval of audit/identity):** `Deps.Audit audit.Recorder` is available from M1 and scoped per module (actions are prefixed `<name>.`, another prefix fails `Invalid`); `moduletest.Conformance` fills a nil `Deps.Audit` with `audittest.NewFake(manifest.Name)` and the leak rule scans the recorded entries' `Reason` and `Fields` (audit.md Proposed change 2); the actor is `identity.PersonFrom(ctx)`, taken as a seam, and no `Deps` field is added (identity.md Proposed change 5).
- **Amended 2026-10-18 (owner approval of authz/people):** `Deps.Authz authz.Authorizer` and `Deps.People people.Directory` are available from M2 (unscoped; `Provision` sets them); `authz.PermissionDecl` gains `StarterRoles []string` (each a valid ident, unique; `Validate` reports `starter_roles[j]`); `moduletest.Conformance` fills a nil `Deps.Authz` with `authztest.NewFake()` (denies everything) and a nil `Deps.People` with `peopletest.NewFake()`; new rules for module authors: a list, search, count or export use case filters by `ExtentOf`, a single-record use case uses `AllowIn` (department records) or `Allow`, and a foreign key to `core.person(id)` is allowed with `ON DELETE RESTRICT` because `people` calls the module's `DeletePerson` first; `Env.CoreExtensionPoints` carries `people.person-page` and `Env.CoreSignals` the three `people.*` names (authz.md Proposed change 2, people.md Proposed change 2).
- **Amended 2026-10-18 (M1 build, accepted deviations):** implementation deviations recorded in Behavior, "Accepted implementation deviations (M1 build, 2026-10-18)".
- **Amended 2026-10-18 (D37; departments-events Proposed change 3, audit Proposed change 16):** `Deps.Departments departments.Directory` (`NamesOf`, `Active`) and `Deps.Events events.Directory` (`InfoOf`) are available from M3 (unscoped; `Provision` sets them); `moduletest.Conformance` fills a nil one with `departmentstest.NewFake()` / `eventstest.NewFake()`; `Env.CoreExtensionPoints` carries `events.event-setup` (the point is `events.event-setup`, not `events.event-created`) and `Env.CoreSignals` the seven `departments.*` and `events.*` names; module authors may declare a foreign key to `core.department(id)` or `core.event(id)` with `ON DELETE RESTRICT`, never read those tables directly, and keep an event-setup contribution idempotent per (event, module); `audittest.Fake` fails `Internal` on a `Record` inside a `db.Read` transaction, so a module whose read path records inside its `Transact(Read)` fails its own unit tests and the conformance run (record after the transaction, before returning).
- **Amended 2026-10-18 (M2 build, accepted deviations):** implementation deviations recorded in Behavior, "Accepted implementation deviations (M2 build, 2026-10-18)": `Deps.Authz` and `Deps.People` in `module.go`; Conformance fills them with fakes.
- **Amended 2026-10-19 (D39, owner approval of settings and notify):** `SettingKind` and `SettingDecl` move to `core/settings` and are aliased here (`SettingBool` ... `SettingEnum`, plus the new `SettingSecret`); the setting key grammar is `[a-z][a-z0-9]*(_[a-z0-9]+)*` (underscores, at most 64 bytes); `Deps.Settings settings.Reader` (M4, scoped per module, bare keys) and `Deps.Notify notify.Notifier` (M4, scoped per module) are available; `Manifest` gains `Templates fs.FS`; `regional` joins the reserved names; there are no automatic retention keys (the module that runs the job declares them); `moduletest.Conformance` supplies `settingstest.NewFake` and `notifytest.NewFake` and gains the rule `templates_register`. Proposed changes 5 and 13 (notify) applied.
- **Package:** `internal/core/module` (Go module `github.com/zedgama3/bms`), plus the test
  helper package `internal/core/module/moduletest`. **UI:** none of its own; the admin
  "modules" screen is `web-shell.md`'s, fed by `GET /api/admin/modules` (`api.md`), both
  drawing on the `Graph` this spec provides.
- **Depends on specs:** `lib-errs.md` (Implemented): `errs.E`, `errs.Is`, kinds `Invalid`,
  `Conflict`, `NotFound`, `Unavailable`, `Internal`, `Canceled`, `Timeout`. `lib-core.md` (Implemented):
  `clock.Clock`, `validate.Errors`, `validate.FieldError`. `model.md` (Approved):
  `model.PersonID`, `model.Sensitivity`, `ID.Validate`, `Sensitivity.Validate`. `db.md`
  (Approved): `db.DB` (the type of `Deps.DB`), `db.ErrReadOnly` (the sentinel a write gets
  on a read-only copy), `*db.Migrator` (the `SchemaChecker` that `app` passes to `Build`),
  `dbtest.Fake` (what `moduletest.Conformance` hands to a factory). `telemetry.md`
  (Approved, 2026-10-16; its Proposed change 1 amended this spec the same day):
  `telemetry.Tracer` (the type of `Deps.Tracer`), `telemetrytest.Capture` (what
  `moduletest.Conformance` uses for `Logger` and `Tracer`). `signals.md` (Approved,
  2026-10-16; its Proposed change 1 amended this spec the same day): `signals.Bus` (the
  type of `Deps.Signals`), `signals.Name` (adopted unchanged from this spec's "Leaf
  types"), `signalstest.NewFake` (what `moduletest.Conformance` hands to a factory as
  `Deps.Signals`). Leaf types that this spec places in other core packages and defines
  here because no spec for them exists yet: `authz.Permission`, `authz.Scope`,
  `authz.PermissionDecl` (`internal/core/authz`, row 7); `signals.Name`
  (`internal/core/signals`, row 10) was defined the same way and is now `signals.md`'s. See
  "Types" and Proposed changes 2 and 3.
  **Used by:** `app` (composition root: resolves, builds, starts and stops the modules; the
  `migrate` and `check` commands), `api` (mounts routes, serves the graph), `authz` (receives
  the declared permissions), `settings` (receives the declared settings), `people` (export
  and deletion across modules), `signals` (the declared names), every module (`Manifest`,
  `Register`, `Deps`, `Links`), and the `hello` reference module (09-core-features §1).

## Summary

Purpose: describe each compiled-in module to the core (Manifest) so `app` can resolve, build,
start and stop it without knowing it. Top of the core graph (no core package imports it);
owns no tables, settings or signals.

Exported names (package `module`):
- `Capability` (one ident; Go interface in `contracts/<name>`), `ExtensionPoint`
  (`<definer>.<ident>`, `.Definer()`), `ExtensionPointDecl{Name, Accepts func(any) bool}`;
  each has `Validate() error`; idents `[a-z][a-z0-9]*(-[a-z0-9]+)*`, at most 64 bytes.
- `SettingKind = settings.Kind` (`SettingBool|Int|Duration|Text|Enum|Secret`, zero invalid; the `Secret` kind is sealed at rest, D39), `SettingDecl = settings.Decl{Key, Kind,
  Default, Options, Check func(string) error `json:"-"`}` (aliases; the types are `core/settings`'s because it cannot import `module`; setting keys are `[a-z][a-z0-9]*(_[a-z0-9]+)*`, at most 64 bytes, not the hyphenated ident).
- `Manifest{Name, Migrations fs.FS, Templates fs.FS (D39; files `<ident>.<lang>.tmpl`, notify names them `<name>.<ident>`), Permissions []authz.PermissionDecl, Settings, Publishes,
  Subscribes []signals.Name, Provides/Requires/Uses []Capability, Defines
  []ExtensionPointDecl, ContributesTo []ExtensionPoint}`; `Validate()` joins every problem.
- `Factory func(ctx, Deps) (Module, error)`; `Module` = `PersonData` + `Start(ctx) error` +
  `Stop(ctx) error`. Optional: `RouteProvider.Routes() []Route`, `APIDescriber.OpenAPI() []byte`
  (mandatory for a RouteProvider), `CapabilityProvider.Capabilities() map[Capability]any`,
  `Contributor.Contributions() map[ExtensionPoint]any`. `PersonData{ExportPerson(ctx,
  PersonID) (Export, error); DeletePerson(ctx, PersonID) (Deletion, error)}`; `Export{Data
  json.RawMessage}`, `Deletion{Deleted, Anonymized int, Kept []Kept}`, `Kept{Kind, Count}`.
- `Route{Method, Pattern, Access, Handler}` (relative to `/api/<name>`; `Validate`),
  `Access` (`AccessSignedIn` zero default, `AccessPublic`). `Deps{Logger, Tracer
  telemetry.Tracer, Clock, Links *Links, DB db.DB, Signals signals.Bus}`; later fields
  Settings, Authz, People, Departments, Events, Audit, Jobs, Notify, Files (Audit M1, scoped per module: actions are prefixed `<name>.`; Departments and Events M3, unscoped, D37; Settings (`settings.Reader`, bare keys) and Notify (`notify.Notifier`, templates prefixed `<name>.`) M4, scoped per module, D39; the rest M2 to Later). The actor is `identity.PersonFrom(ctx)` (false inside a signal delivery and for a restricted session), taken as a seam (`actorFunc`) so tests use `identity.WithSession`; no `Deps` field.
- `Set.Contributors(point) ([]Contribution, error)` for core definers (`Invalid` unless point is in `Env.CoreExtensionPoints`); `Factory` needs no database read (`openapi`/`check` build against `db.None()`); `Build`/`Start` run under 30 s, `Stop` shares `http.shutdown_timeout`; decision 9: every module ships `messages/en.json`.
- `Links.Capability(name) (any, error)`, `Links.Contributors(point) ([]Contribution, error)`, `Contribution{Module, Value}`; frozen after `Build`, safe for concurrent use.
- `Registry`: `NewRegistry`, `Default`, package `Register(Manifest, Factory)` (in `init`),
  `Add`, `Manifests`, `Resolve(Env) (*Plan, error)`; `Env{Enabled, CoreSignals, CoreExtensionPoints}`.
- `Plan`: `Order()`, `Permissions()`, `Settings() []ModuleSettings`, `Graph() Graph`,
  `Build(ctx, SchemaChecker, Provision) (*Set, error)`. `Graph{Modules []Node, Edges []Edge,
  Warnings []Warning}`; `EdgeKind` (`EdgeRequires|Uses|Contributes|Subscribes`).
  `SchemaChecker.CheckSchema(ctx, module, fs.FS) error` (`*db.Migrator` satisfies it);
  `Provision func(ctx, Manifest) (Deps, error)` (sets all but `Links`).
- `Set`: `Modules() []Built`, `Routes() []ModuleRoutes` (also the shape of core route sets),
  `PersonData() []ModulePersonData`, `Start(ctx)`, `Stop(ctx)` (reverse order, continues).
- Leaf types placed elsewhere, defined here: `authz.Permission` (`<module>.<ident>(.<ident>)*`,
  `.Module()`), `authz.Scope` (`ScopeSite|ScopeDepartment`), `authz.PermissionDecl{Name, Scope,
  Reaches model.Sensitivity, StarterRoles []string}` (M2); `signals.Name` (now signals.md's).
- `moduletest.Conformance(t, manifest, factory, provision, opts ...Option)`: named subtests
  (`manifest_valid`, `start_read_only`, `delete_read_only`, `no_personal_data_in_logs`, ...).
  Options (owner-approved 2026-10-17): `WithCapability(name, impl)` (mandatory per `Requires`);
  exactly one of `WithSeed(Seed)` / `WithoutPersonData()`; `Seed func(ctx, Module, Subject)
  error`; `Subject{Person, LegalName, Email, Phone, Note}` (markers; the unknown-person ID is
  unexported and distinct); rules (D39: `templates_register`; a nil `Deps.Settings` becomes `settingstest.NewFake(manifest.Name, manifest.Settings)` and a nil `Deps.Notify` `notifytest.NewFake(manifest.Name)`, and the leak rule also scans `Fake.Sent()` vars and links for the seed markers) `rejects_wrong_capability_type`, `used_capability_absent_
  builds_and_starts`, `seeded_person_exports_data`, `seeded_person_delete_counts_and_is_
  idempotent`; the leak rule also asserts no `[email]` and scans the `audittest.Fake`'s recorded entries (`Reason`, `Fields`) for the seed markers and `[email]`; a nil `Deps.Audit` becomes `audittest.NewFake(manifest.Name)`; `module.StartRollbackTimeout` (10 s).
- `module.IsCoreArea(name) bool` (the core areas reserved by `Manifest.Validate`; app.md F28 uses it).

Key decisions: 1 static Manifest plus separate Factory; 2 `Register` records problems, no
panic; 3 top of core graph, name types in authz/signals; 4 capabilities resolve at
construction (providers first, cycle is an error), extension points per request; 5 types
checked at start-up by the side that knows the interface (`any` plus `Accepts`); 6 dangling
subscription/contribution to a disabled module is a Warning, to an unknown name an error;
7 all compiled-in modules enabled unless `modules.disabled` names them; 8 no English text in
manifests, titles are keys `module.<name>.*`; 9 no "has UI" flag; 10 names
`[a-z][a-z0-9]{1,31}` (`afterreports`), reserved list pinned; 11 `PersonData` mandatory;
12 schema check once in `Build`, all errors collected (D17); 13 routes are a list with
`Access`, default signed-in; 14 `Start` never writes; 15 contract calls take batches,
providers check permissions, payloads carry IDs only; 16–21 (amendment approved 2026-10-17): seed via
`Seed`, unscrubbable markers plus `[email]`, seed-or-none mandatory, capability fakes by
option, `Set.Start` rollback under `StartRollbackTimeout`, `IsCoreArea` exported.

Errs kinds callers handle: `Invalid` (manifest/route/graph problems, undeclared Links use,
zero PersonID), `Conflict` (duplicate name, two providers, `Start` twice, schema mismatch),
`NotFound` (`Links.Capability` for an absent `Uses` capability: hide the feature),
`Internal` (nil module, `Links` misuse before freeze), `Unavailable` wrapping
`db.ErrReadOnly` (`DeletePerson` on a read-only copy), `Timeout`/`Canceled`. No sentinels.

Owns: permission `core.modules.read` (declared by `api`; graph at `GET /api/admin/modules`;
public `GET /api/modules` lists enabled names only); `people.data.export`/`.delete` are
`people`'s. No signals, no settings; config key `modules.disabled` (config.md). Route prefix
`/api/<name>/`; message-key convention in decision 8; web catalogues are web-shell's.
Expects of dependencies: lib-errs (kinds, `errs.E`/`Is`); lib-core (`clock.Clock`,
`validate.Errors`/`FieldError`); model (`PersonID`, `Sensitivity.Validate`); db (`db.DB`,
`db.ErrReadOnly`, `*db.Migrator` as `SchemaChecker`, `dbtest.Fake`); telemetry (`Tracer`,
`telemetrytest`, `Scrub`'s `[email]` token); signals (`Bus`, `Name`, `signalstest.NewFake`).
Of consumers: `app` supplies `Env`, `Provision`, the checker and the order Build, Start, Stop;
`api` mounts `Set.Routes`; `authz`/`settings` take `Plan.Permissions`/`Plan.Settings`; `people` runs `Set.PersonData`.
Proposed changes: 1, 3, 4, 6, 7, 9–12, 14 applied; 2 and 8 applied 2026-10-18; 5 and 13 (notify) applied 2026-10-19 (D39), 13 (jobs) pending (target unwritten);
15–17 (hello, skills; none for telemetry or app) applied 2026-10-17.

**M2 build deviations (2026-10-18).** `Deps.Authz` and `Deps.People` are declared in `module.go`; `moduletest.Conformance` fills them with `authztest` and `peopletest` fakes.

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
| Mounting routes, middleware, sessions, cross-origin protection, the error mapper, the OpenAPI document | `api` (`api.md`). This spec hands it each module's route list and OpenAPI fragment. |
| Deciding whether a person may do something; roles, grants, separation of duties | `authz` (`authz.md`). This spec hands it the declared permissions. |
| Delivering signals, the catalogue of payloads, after-commit semantics | `signals` (`signals.md`). This spec validates names and tells `signals` which names each module declared. |
| Storing, typing and editing settings; terminology; retention settings per module and level (D30) | `settings` (`settings.md`). This spec hands it the declared settings; `settings` adds the retention keys for every module itself. |
| Running jobs, and running them only on the writing copy | `jobs` (`jobs.md`). A module registers its jobs through `Deps.Jobs` in `Start`. |
| Orchestrating "export everything about person X" and "delete everything about person X" across modules, checking the permission for it and auditing it | `people` (`people.md`). This spec defines the `PersonData` interface every module must implement and gives `people` the list. |
| The shapes of capabilities and extension points (the Go interfaces and data types) | `internal/contracts/<name>/` (D19). This spec carries only their names and checks the wiring. |
| The module's UI: screens, navigation, its message catalogue, the build-time web registry (D31) | `web-shell.md`. The manifest does not declare "has UI"; see decision 9. |
| Forms (D32), terminology, any feature a burner sees | the module's own spec, written with `add-module`. |
| Process configuration, including which modules are enabled | `config` (`config.md`). This spec receives the enabled names in `Env`. |
| Telemetry: the logger, tracer and meter, trace IDs, the error reference | `telemetry.md`. Modules get a scoped logger and a scoped tracer in `Deps`; the request span is the api shell's; a module opens sub-spans through `Deps.Tracer`. |
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
| `moduletest` | Prove a module keeps the contract. | `Conformance(t, manifest, factory, provision, opts ...Option)`. |

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

// The declaration types live in core/settings (settings.md: settings cannot import
// module, so they move there as authz.Permission and signals.Name did) and are aliased
// here, so manifests are unchanged (D39).
type SettingKind = settings.Kind

const (
	SettingBool     = settings.Bool     // "true" or "false"
	SettingInt      = settings.Int      // a base-10 integer
	SettingDuration = settings.Duration // time.ParseDuration form, for example "72h"
	SettingText     = settings.Text     // free text, up to 4096 bytes
	SettingEnum     = settings.Enum     // exactly one of Options
	SettingSecret   = settings.Secret   // free text, sealed at rest, never returned; Default must be ""
)

// SettingDecl declares one runtime setting of a module (settings.md stores and edits it
// under the key "<module>.<key>"). Its title and description are message keys, not text
// (decision 8): module.<name>.setting.<key>.title|description|option.<opt>. The key grammar
// is [a-z][a-z0-9]*(_[a-z0-9]+)*, at most 64 bytes (underscores, because ICU arguments
// allow no hyphen); Check is optional, pure and safe for concurrent use. Validate is
// settings.Decl.Validate: errs.Invalid joining one validate.FieldError per problem (key,
// kind, options, default, a Secret with a Default or Options).
type SettingDecl = settings.Decl

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

	// Templates holds the module's notification templates (go:embed), files named
	// <ident>.<lang>.tmpl with Subject:/Vars: headers; notify names them "<Name>.<ident>"
	// and checks them at Register (D39). nil means the module sends nothing.
	Templates fs.FS

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
// (go-conventions: constructors validate), builds its services, and needs no database
// read and never writes: `bms openapi` and `bms check` build every module against a DB
// that refuses every statement (app.md decision 7), so a read at construction fails the
// build step. A module builds on a read-only copy. ctx carries app's 30 s Build deadline.
type Factory func(ctx context.Context, deps Deps) (Module, error)

// Module is what every module gives the core at runtime. The optional interfaces below
// add routes, an API description, capability implementations and contributions; Build
// checks that a module implements exactly what its manifest declares (F29–F31 and F33–F34).
type Module interface {
	PersonData

	// Start begins the module's background work: it registers signal handlers and jobs
	// through Deps and launches goroutines that exit when Stop is called. It subscribes at
	// most once per declared signal name and never publishes (a signal describes a write;
	// signals.md). It returns once that is set up, without blocking, and must succeed on a
	// read-only database (it performs no writes). It fails with the kind of whatever it
	// tried; Set.Start then stops the modules already started.
	Start(ctx context.Context) error

	// Stop ends the work Start began and waits for it, within ctx. It is called once,
	// after Start returned nil, and must be safe to call when Start was never called.
	// Fails with errs.Timeout or errs.Canceled when ctx ends first (F47), joined with any
	// goroutine's error.
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
	// Fails with errs.Invalid for a zero person; on a read-only copy, with
	// errs.Unavailable wrapping db.ErrReadOnly, the kind db returns for a write (db.md).
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
// route under /api/<name>/ with its pipeline (authentication per Access, cross-origin
// protection for unsafe methods, the request span) and the error mapper; handlers use
// httpx.ReadJSON, call the service, httpx.WriteJSON, and httpx.Fail on error
// (add-module). Permission checks happen in the service, never here. A RouteProvider
// must also implement APIDescriber: api requires the fragment (api.md decision 11; F34).
type RouteProvider interface {
	// Routes returns the module's routes. Cannot fail: handlers are built already. Build
	// validates each route (F33) and keeps the list; api reads it from Set.Routes.
	Routes() []Route
}

// Route is one HTTP route relative to /api/<name>. api mounts it as
// "<Method> /api/<name><Pattern>" (api.md decision 3): a Pattern ending in "/" is mounted
// exactly ("{$}" appended), so "/" answers only /api/<name>/; a module that wants a
// subtree says "{path...}". The mounted pattern must fit telemetry's token grammar
// ("[A-Za-z0-9/_][A-Za-z0-9._:/{}-]*"; Validate enforces the character set, so a module
// learns in its own tests) and the whole span name "<Method> /api/<name><Pattern>" must
// be at most 128 bytes (api F48). A GET or HEAD route never changes state: the
// cross-origin check does not cover it and browsers prefetch it.
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
// start with "/" or contains a byte outside "[A-Za-z0-9._:/{}$-]" (so no space, "%", "+",
// "," or ";"; "$" only for ServeMux's "{$}"), Access is out of range, or Handler is nil.
func (r Route) Validate() error

// APIDescriber is implemented by a RouteProvider that documents its routes; every
// RouteProvider must implement it. The api shell
// merges the fragments into one OpenAPI document (api.md decision 11 defines the fragment
// form: one JSON object with paths relative to /api/<name>, components.schemas whose names
// start with the module's name, and tags; "$ref"s point at the module's own schemas or
// the base ones, never another module's).
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
	Logger *slog.Logger     // scoped (module=<name>); never nil (Build checks)
	Tracer telemetry.Tracer // telemetry.md (row 15b): scope module.<name>; never nil (Build checks)
	Clock  clock.Clock      // never nil (Build checks)
	Links  *Links           // set by Build, never by Provision

	DB      db.DB       // db.md (row 5): pool, transactions, read-only state
	Signals signals.Bus // signals.md (row 10): Provision sets disp.Scope(m.Name, m.Publishes, m.Subscribes)

	// Added by later specs, named here so modules know where to look:
	// Settings settings.Reader   settings.md (row 15): M4; scoped per module (bare keys); Provision sets a.deps.Settings.Scoped(m.Name);
	//                            a module that declares no settings gets a nil Deps.Settings (no area, no key to read) (implementation, 2026-10-19)
	// Authz    authz.Authorizer  authz.md (row 7): M2; unscoped; Provision sets it
	// People   people.Directory  people.md (row 8): M2; unscoped; Provision sets it
	// Departments departments.Directory  departments-events.md (row 9): M3; unscoped; Provision sets it (D37)
	// Events   events.Directory  departments-events.md (row 9): M3; unscoped; Provision sets it (D37)
	// Audit    audit.Recorder    audit.md (row 12): M1; scoped per module, actions prefixed "<name>."
	// Jobs     jobs.Registrar    jobs.md (row 14)
	// Notify   notify.Notifier   notify.md (row 11): M4; scoped per module (templates prefixed "<name>.")
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
	// events.event-setup, ...), so modules may contribute to them.
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

// Graph returns the whole registry as data for `GET /api/admin/modules` and `bms check`:
// every compiled-in module, enabled or not, its declarations, the edges between enabled
// modules (and from an enabled module to a core area) and the warnings Resolve produced. An
// enabled module's dangling edge to a disabled module is a Warning, not an Edge. Cannot fail.
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
	Settings      []SettingDecl // Check funcs are not serialized
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
// for module with the one migrations is built for. *db.Migrator implements it
// structurally (db may not import module); app asserts it and passes it to Build (db.md).
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

// Contributors returns every enabled module's contribution to point, in build order, for
// core definers that are not modules and hold no Links (people for the person page,
// departments-events for new-event copying; app.md "How later core services plug in").
// Fails with errs.Invalid for any point not in Env.CoreExtensionPoints (F43b). Safe for
// concurrent use.
func (s *Set) Contributors(point ExtensionPoint) ([]Contribution, error)

// Start calls each module's Start in build order. If one fails, the modules already
// started are stopped in reverse order under StartRollbackTimeout (a fresh deadline on
// context.WithoutCancel(ctx), decision 20), and the error is returned wrapped with the
// failing module's name, joined with any Stop errors (F37). Fails with errs.Conflict
// if Start already succeeded (F38).
func (s *Set) Start(ctx context.Context) error

// StartRollbackTimeout bounds the rollback Stop calls when Start fails (decision 20).
const StartRollbackTimeout = 10 * time.Second

// IsCoreArea reports whether name is a core area (decision 21).
func IsCoreArea(name string) bool

// Stop calls each started module's Stop in reverse build order, continuing past
// failures, and returns them joined (F39). A Set that was never started, or already
// stopped, returns nil.
func (s *Set) Stop(ctx context.Context) error
```

```go
// Package moduletest proves a module keeps the module contract. Every module's test suite
// calls Conformance; the hello module is the reference.
package moduletest

// Conformance runs the contract checks against one module. provision supplies the
// module's dependencies as fakes, as app's Provision does (db is a dbtest.Fake, signals a
// signalstest.Fake scoped to the manifest, and the later fields); it is called once per
// rule, so each rule gets fresh fakes; nil is allowed. Conformance replaces Logger and
// Tracer with telemetrytest.New(t)'s (any policy violation fails the rule), fills a nil
// Clock, DB, Signals or Audit with a fake (audittest.NewFake(m.Name)), and sets Links through Plan.Build.
//
// opts supply what the rules cannot learn from the manifest: the value behind each
// capability the module requires or uses (WithCapability), and how to put a known
// person's records into the module (WithSeed) or the statement that it holds none
// (WithoutPersonData). Exactly one of WithSeed and WithoutPersonData is required, and
// WithCapability is required for every name in m.Requires. Conformance fails t before the
// first rule when the options disagree with the manifest (F48–F52), naming the option and
// the capability; otherwise it runs the rules as named subtests and stops at the first
// that fails, so the failing subtest's name is the broken rule.
func Conformance(t *testing.T, m module.Manifest, f module.Factory, provision module.Provision, opts ...Option)

// Option configures Conformance. Only the With* functions make one; the zero Option is
// invalid and fails Conformance before the first rule (F52).
type Option struct{ apply func(*options) error }

// WithCapability makes the stub provider for capability name return impl from
// Capabilities(), so the module's Factory can assert it to the contract interface
// (decision 5, F32). name must be in m.Requires or m.Uses (F49), impl non-nil (F50), each
// name given once (F51). It is mandatory for every name in m.Requires (F48). For a name
// in m.Uses it is optional: given, the rules run with the capability present; not given,
// the capability is absent and Links.Capability returns errs.NotFound (F41), which the
// module must survive. The rule used_capability_absent_builds_and_starts runs the absent
// path regardless.
func WithCapability(name module.Capability, impl any) Option

// WithSeed registers how a rule puts records about a known person into the module. The
// rules that need data (seeded_person_exports_data,
// seeded_person_delete_counts_and_is_idempotent, no_personal_data_in_logs) call seed
// once each, on a fresh module, after Plan.Build and before Start.
func WithSeed(seed Seed) Option

// WithoutPersonData states that the module holds nothing about any person (its
// ExportPerson is always empty and its DeletePerson always zero). The seeded rules then
// pass trivially and no_personal_data_in_logs runs without a seed. A module whose spec
// lists any table or field about a person must not use it; the reviewer checks this
// against the spec's Data section.
func WithoutPersonData() Option

// Seed writes records about subject into the module built as mod, putting each of
// subject's marker values into every field of the matching kind the records have (a
// legal-name field gets LegalName, an e-mail field Email, a phone field Phone, a free-text
// field such as a note, an address or a narrative gets Note), so a record that reaches a
// log or span is caught. It touches no other person. An error fails the rule.
//
// Conformance cannot reach a module's store, and the production Factory over Deps.DB
// cannot be seeded because dbtest.Fake holds no rows. So the factory passed to Conformance
// builds the module over the test's in-memory fake store (the one the module's unit tests
// already use), and Seed writes to that store (shared with the factory closure, or reached
// by asserting mod to the module's concrete type).
type Seed func(ctx context.Context, mod module.Module, subject Subject) error

// Subject is the known person the seeded rules act on. The values are fixed, invented and
// reach a module only through Seed; moduletest exports no person ID or marker constant,
// and the ID the unknown-person rules use is unexported and differs from Person, so a
// seeded fake cannot also be asked to be empty.
type Subject struct {
	Person    model.PersonID // never zero; never the unknown-person ID
	LegalName string         // "Marker Quillfeather": a name has no shape telemetry can scrub, so it is found verbatim
	Email     string         // "marker.quillfeather@example.invalid": a leak shows as "[email]" after Scrub, or as a Violation under a denied key
	Phone     string         // "+15550100773": digits only, found verbatim
	Note      string         // "conformance-marker-note-0773": free text (an address, a narrative), found verbatim
}
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
| `Deps` | every `Factory`; `Provision` in `app`; `Build` (sets `Links`, checks `Logger`, `Tracer`, `Clock`) | — | scoped services |
| `Links.Capability` | consuming modules, in their `Factory` | provider built (order) | the impl, `NotFound` for an absent optional one |
| `Links.Contributors` | defining modules and the core's definers (`people` for the person page, `departments-events` for new-event copying), at request time | `Build` finished | accepted contributions, build order |
| `Registry`, `NewRegistry`, `Default`, `Register`, `Add`, `Manifests` | every module (`init`); `app`; tests | — | the compiled-in set |
| `Resolve`, `Env`, `Plan.Order` | `app` (`serve`, `migrate`, `check`); `db` (migrations in order) | registered | checked order |
| `Plan.Permissions` | `authz` through `app` | resolved | declared permissions |
| `Plan.Settings` | `settings` through `app` | resolved | declared settings |
| `Plan.Graph`, `Graph`, `Node`, `Edge`, `Warning` | `api` (`GET /api/admin/modules`; enabled names only on the public `GET /api/modules`), `app` (`check`, logs warnings) | resolved | serializable |
| `SchemaChecker` | `Build`; implemented by `db` | — | D17 check |
| `Provision` | `app` | — | scoped `Deps` |
| `Plan.Build`, `Set` and accessors | `app`; `api` (`Routes`); `people` (`PersonData`) | resolved; database reachable | every module verified |
| `moduletest.Conformance` | every module's tests; `hello` | — | contract kept |

## Types

Defines: `Capability`, `ExtensionPoint`, `ExtensionPointDecl`, `SettingKind` and `SettingDecl` (aliases of `settings.Kind` and `settings.Decl`, D39),
`Manifest`, `Factory`, `Module`, `PersonData`, `Export`, `Deletion`, `Kept`, `RouteProvider`,
`Route`, `Access`, `APIDescriber`, `CapabilityProvider`, `Contributor`, `Deps`, `Links`,
`Contribution`, `Registry`, `Env`, `Plan`, `ModuleSettings`, `Graph`, `Node`, `Edge`,
`EdgeKind`, `Warning`, `SchemaChecker`, `Provision`, `Set`, `Built`, `ModuleRoutes`,
`ModulePersonData`. Uses `model.PersonID`, `model.Sensitivity`, `clock.Clock`,
`validate.Errors`, `validate.FieldError`, `fs.FS`, `http.Handler`, `json.RawMessage`,
`slog.Logger`, `telemetry.Tracer`.

### Leaf types placed in `core/authz` and `core/signals`

`module` is the top of the core graph (it holds every core contract in `Deps`), so no core
package may import it. `authz` must read the declared permissions and `signals` the declared
names without importing `module`; therefore the name types live in those packages. They are
defined here because `authz.md` and `signals.md` were not yet written; those specs adopt them
unchanged (Proposed changes 2 and 3; `signals.md` did so on 2026-10-16). Until then, each
package holds exactly these types.

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
	// StarterRoles are the keys of the starter roles this permission is seeded into once
	// by `bms migrate` (authz.md decision 12). Each is a valid ident, unique.
	StarterRoles []string
}

// Validate fails with errs.Invalid joining a FieldError per problem: Name invalid, Scope
// zero or out of range, Reaches failing model.Sensitivity.Validate, starter_roles[j]
// not a valid ident or repeated.
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
| `Logger` | `log/slog` (built by `telemetry.md`) | `module=<name>` attribute; never logs personal data (go-conventions) | M0 |
| `Tracer` | `telemetry.md` | scope `module.<name>`; sub-spans inside the api's request span; typed attributes only (IDs and tokens, never personal data) | M0 |
| `Clock` | `lib-core` | shared | M0 |
| `Links` | this spec | the module's own declarations | M0 |
| `DB` | `db.md` | the module's schema is its own responsibility; `db` reports read-only | M0 |
| `Signals` | `signals.md` | publishing a name not in `Publishes`, or subscribing to one not in `Subscribes`, fails with `errs.Invalid` | M0 |
| `Settings` | `settings.md` | scoped to the module's area: bare keys, only the declared ones; `Bool`, `Int`, `Duration`, `Text` (text and enum), `Secret` (sealed); a read is a map lookup, so a module never caches a setting; `Conformance` supplies `settingstest.NewFake(manifest.Name, manifest.Settings)` when nil (D39) | M4 |
| `Authz` | `authz.md` | shared (unscoped); the module passes its own permission names; a list, search, count or export use case filters by `ExtentOf`, a single-record use case uses `AllowIn` (department records) or `Allow`; `Conformance` supplies `authztest.NewFake()` when `Provision` leaves it nil (it denies everything, so a module that forgets its check fails its tests) | M2 |
| `Audit` | `audit.md` | scoped per module with `Scoped(<name>)`: actions are prefixed `<name>.`, another prefix fails `Invalid`; `Conformance` supplies `audittest.NewFake(manifest.Name)` when `Provision` leaves it nil and its leak rule scans the recorded entries' `Reason` and `Fields` for the seed markers and `[email]` | M1 |
| `People` | `people.md` | shared (unscoped); `NamesOf`, `ConsentOf`; `Conformance` supplies `peopletest.NewFake()` when nil; a foreign key to `core.person(id)` is allowed and uses `ON DELETE RESTRICT` (`people` calls the module's `DeletePerson` first) | M2 |
| `Departments` | `departments-events.md` | shared (unscoped); `NamesOf`, `Active` (check `Active` before writing a department's record); `Conformance` supplies `departmentstest.NewFake()` when nil; a foreign key to `core.department(id)` is allowed with `ON DELETE RESTRICT`, the table is never read directly (D37) | M3 |
| `Events` | `departments-events.md` | shared (unscoped); `InfoOf`; `Conformance` supplies `eventstest.NewFake()` when nil; a foreign key to `core.event(id)` is allowed with `ON DELETE RESTRICT`; an `events.event-setup` contribution is idempotent per (event, module) (D37) | M3 |
| `Jobs` | `jobs.md` | job names prefixed `<name>.`; jobs run only on the writing copy | M4 |
| `Notify` | `notify.md` | scoped per module (templates prefixed `<name>.`, shipped in `Manifest.Templates`); `Conformance` supplies `notifytest.NewFake(manifest.Name)` when nil (D39) | M4 |
| `Files` | `files.md` | shared | Later |

The actor is not a `Deps` field. A module that needs the signed-in person calls `identity.PersonFrom(ctx)` (false when there is no session, for a restricted session and inside `signals.InDelivery`); it imports `core/identity` for that and takes it as a seam (`actorFunc`, as `hello` does) so tests use `identity.WithSession` (identity.md Proposed change 5).

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
| `core.modules.read` | read the module graph and schema versions (`GET /api/admin/modules`, 09 §7); the enabled names alone are public on `GET /api/modules` (api.md decision 27) | site | every site-admin starter role | `api.md` (`api.PermissionModulesRead`, declared where it is checked), registered with `authz` by `app` (api.md Proposed change 10) |
| `people.data.export`, `people.data.delete` | run `PersonData` across modules for one person | site | account admin (09 §11) | `people.md` (Proposed change 8, applied 2026-10-18) |

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
validation. There are no automatic retention settings per module and level (D39, settings decision 11): the module whose spec has the job that enforces a retention declares its own `<name>.retention*` keys (default "keep forever", D30).

Regional differences inside a module are that module's settings; this contract gives them
one declaration (`SettingDecl`, an alias of `settings.Decl`) with a default, a kind and validation, so `settings` can
show and check every module's settings the same way.

## UI

None of its own. What this spec provides for screens elsewhere:

- **Admin "modules" screen** (`web-shell.md`, 09 §7): `GET /api/admin/modules` (`api.md`)
  serves `Plan.Graph()` plus, per module with a schema, the built-for and applied versions
  from `db`. Visible with `core.modules.read`. Empty state: no modules compiled in (the
  graph has only core nodes). Forbidden: 403 from the api; the shell hides the menu entry.
- **Module screens** (D31): the web shell's build-time registry imports each module's
  folder; it shows a module's navigation and screens only if the module's name is enabled
  in the public `GET /api/modules` (names and enablement only; api.md decision 27). A
  disabled module's screens are not mounted, so turning a module off hides its UI without
  a rebuild (add-module §3).

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
   without an enabled Go module is not shown. The web registry also lists the core areas
   (`kind: "core"`), which are always enabled because they are not modules (web-shell.md
   decision 3, 2026-10-17). Every module ships
   `web/src/modules/<name>/messages/en.json` even without screens, because the admin
   "modules" screen shows its title; `app`'s catalogue test requires it (app.md decision 16).
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

16. **The module seeds itself through `Seed`; `Conformance` never reaches into a store.**
    The contract knows nothing about any module's storage, and `dbtest.Fake` holds no rows,
    so there is no generic way to put a row in front of a module. The one value every
    module shares is the built `Module`, so that is what `Seed` receives, with the markers.
    The unknown-person ID stays unexported and distinct from `Subject.Person`, so the two
    families of rules cannot be satisfied by one fake record. Exporting `Person()` and the
    marker constants (as the first implementation did) is withdrawn.
17. **Markers that scrubbing cannot hide, and the scrub token as evidence; no telemetry
    change.** `Scrub` is a production safety net, not a detector: making it report every
    e-mail it rewrites would count a `Violation` (and bump `policy_violations`) on
    legitimate production traffic, or need a test-only mode in telemetry. A name, a phone
    and a free-text note have no shape `Scrub` recognises and are found verbatim; an e-mail
    leaks as `[email]`, which telemetry.md already specifies as `Scrub`'s substitution, so
    asserting its absence costs nothing and proves the e-mail path too.
18. **Exactly one of `WithSeed` and `WithoutPersonData` is mandatory.** `Conformance` cannot
    tell a module that holds nothing about people from one whose author forgot to seed, and
    a silent pass is the failure mode this amendment exists to remove. A skipped subtest
    is visible only under `-v`; an explicit claim is visible in the test file and in review.
    The cost is one option in the rare module without person data.
19. **Capability fakes come through options; required ones are mandatory, used ones
    optional, and both the wrong-type and the absent paths are proven.** Putting the fake in
    the option, not in `provision`, keeps `Provision`'s shape (it sets `Deps`, and `Links`
    is `Build`'s); keeps `Conformance` free of `internal/contracts` (the module's test
    imports the contract and hands over a value, `Conformance` carries `any` like `Links`);
    and lets the rules swap the value for `struct{}{}` to prove F32 for every module rather
    than only for the fake consumer in `module`'s own tests.
20. **`Set.Start` rolls back under a fresh deadline.** The `Set.Start` doc says the modules
    already started are stopped in reverse order under `context.WithoutCancel(ctx)` with a
    fresh deadline of `StartRollbackTimeout = 10 * time.Second`, an exported constant of
    `module`: the one timeout this package owns, because it is the one call whose budget the
    caller cannot derive (the caller's budget is for starting, and the failure may have spent
    it; with a done `ctx` every rollback `Stop` would return `Timeout` at once, F47, and the
    started goroutines would never be waited on; `Set.Stop` on a set whose `Start` failed
    returns nil, so `app` cannot repair this afterwards). `app` needs no change: its F11 and
    `startTimeout` stand.
21. **`module.IsCoreArea(name string) bool` is exported** so `app`'s part-name check (app.md
    F28) uses the same list as `Manifest.Validate`'s reserved core areas.

### Reserved module names

`Manifest.Validate` rejects: the core areas (`core`, `people`, `accounts`, `access`,
`identity`, `authz`, `departments`, `events`, `signals`, `notify`, `audit`, `files`, `jobs`,
`settings`, `telemetry`, `db`, `module`, `modules`), `regional` (the settings area of the regional's own values, D39; reserved so no module can declare `regional.*` keys; `IsCoreArea` is unchanged), the shell packages (`api`, `app`,
`config`, `web`, `admin`, `client`), and PostgreSQL's (`public`, `information`, anything
starting with `pg`). The list is a constant in the package and a test pins it. `func IsCoreArea(name string) bool`
reports whether `name` is one of the core areas in the first group (decision 21); it cannot
fail and is pure.
`ModuleRoutes` is also the shape `app` uses for the core's route sets, with `Module` set to
the core area name (`accounts`, `people`, ...), which is reserved here and so can never
collide with a module; `api` additionally refuses a set named `core`, `modules` or `admin`
(api.md F10), since those are its own route set's name and the first segments of its
absolute routes.

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
   `module.Registry.Resolve`; its kind is `Conflict` only when every problem is a
   `Conflict` (F18 alone), otherwise `Invalid`.

### Build, step by step

1. For every module in order with `Migrations != nil`: `schema.CheckSchema`; collect every
   error (F26) and stop if any.
2. For each module in order: `provision(ctx, m)` (F24); check `Logger`, `Tracer` and `Clock` (F25); set
   `Links` to the module's view with providers built so far; call the factory (F27, F28).
3. Verify: `Capabilities()` keys equal `Provides` and no value is nil (F29); `Contributions()`
   keys equal `ContributesTo` (F30); `Routes()` each validate and no two share method and
   pattern (F33); a `RouteProvider` implements `APIDescriber` and `OpenAPI()` is non-empty
   (F34). Implementing `CapabilityProvider` or `Contributor` and returning an empty map is
   allowed; only a non-empty map with an empty declaration is F29/F30.
4. After all are built: for every contribution, the definer's `Accepts` (F31); freeze every
   `Links`.
5. Return `Set`. `ctx` done at any step → `Canceled` or `Timeout` (F35).

### Concurrency and lifecycle

`Registry` is guarded by a mutex; `Register` and `Add` may be called from any goroutine
(`init` is sequential in practice, tests may be parallel). `Plan`, `Links` (after `Build`)
and `Set`'s accessors are immutable and safe for concurrent use. `Build`, `Start` and `Stop`
are called once each by `app`, in that order, never concurrently; `Set` guards against a
second `Start` (F38). `Set` owns no goroutines: each module's `Start` launches its own and
`Stop` waits for them, within the `ctx` `app` passes (app.md sets the deadlines: `Build` and `Set.Start` each
under 30 s; `Set.Stop` shares `http.shutdown_timeout` with the other closers). `Stop` continues past a failing module so the rest still stop.

### I/O

None in this package: the only I/O is what `SchemaChecker`, `Provision`, factories, `Start`
and `Stop` do, through the `ctx` they receive. No timeouts are added here except `StartRollbackTimeout`;
callers derive the rest.

### Imports

Standard library (`context`, `encoding/json`, `errors`, `fmt`, `io/fs`, `log/slog`,
`net/http`, `regexp`, `sort`, `sync`), `internal/lib/errs`, `internal/lib/validate`,
`internal/lib/clock`, `internal/model`, `internal/core/authz`, `internal/core/signals`,
`internal/core/db`, `internal/core/telemetry` (the `Tracer` type only). Never
`internal/contracts`, a module, `api`, `app` or `config` (depguard rule `core`); never
`go.opentelemetry.io` (depguard rule `otel-seam`). `moduletest` adds `testing`,
`internal/lib/testkit` and `internal/core/telemetry/telemetrytest`.

### Accepted implementation deviations (M1 build, 2026-10-18)

- `Deps.Audit` is declared in `module.go`.
- Conformance's leak rule scans the audit entries' `Reason` and `Fields` (recorded by the fake) for the seed markers.

**M2 build (2026-10-18):**

- `Deps.Authz` and `Deps.People` are declared in `module.go`.
- `moduletest.Conformance` fills them with `authztest` and `peopletest` fakes.

## Failure modes

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F1 | `Manifest.Validate` | `Name` empty, not `[a-z][a-z0-9]{1,31}`, or reserved | Invalid, field `name` | fix the module | `TestManifestValidate_BadOrReservedName_ReturnsInvalid` |
| F2 | `Manifest.Validate` | a permission name fails `authz.Permission.Validate` or does not start with `<Name>.` | Invalid, `permissions[i].name` | fix the module | `TestManifestValidate_PermissionNotPrefixed_ReturnsInvalid` |
| F3 | `Manifest.Validate` | a permission's `Reaches` fails `model.Sensitivity.Validate` (model.md Proposed change 3) | Invalid, `permissions[i].reaches` | fix the module | `TestManifestValidate_PermissionReachesUnset_ReturnsInvalid` |
| F4 | `Manifest.Validate` | a permission's `Scope` is zero or out of range | Invalid, `permissions[i].scope` | fix the module | `TestManifestValidate_PermissionScopeInvalid_ReturnsInvalid` |
| F5 | `Manifest.Validate` | duplicate permission, setting key, signal, capability or point within the manifest | Invalid, the second's field | fix the module | `TestManifestValidate_Duplicates_ReturnInvalid` |
| F6 | `Manifest.Validate`, `SettingDecl.Validate` | setting: key not `[a-z][a-z0-9]*(_[a-z0-9]+)*` or over 64 bytes; kind invalid; options wrong for the kind; default fails the kind's parse or `Check`; a `Secret` with a `Default` or `Options` | Invalid, `settings[i].<field>` | fix the module | `TestSettingDeclValidate_Table` |
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
| F18 | `Registry.Resolve` | two enabled modules provide one capability | Conflict, naming both (the error is `Conflict` only when every collected problem is a Conflict; any mix is Invalid, F22) | disable one | `TestResolve_TwoProviders_ReturnsConflict` |
| F19 | `Registry.Resolve` | a cycle in `Requires`/`Uses` among enabled modules | Invalid, naming the cycle | redesign (send to architect) | `TestResolve_CapabilityCycle_ReturnsInvalid` |
| F20 | `Registry.Resolve` | a subscribed signal that neither the core nor any registered module publishes | Invalid | fix the name | `TestResolve_SubscribeUnknownSignal_ReturnsInvalid` |
| F21 | `Registry.Resolve` | a contribution to a point neither the core nor any registered module defines | Invalid | fix the name | `TestResolve_ContributeUnknownPoint_ReturnsInvalid` |
| F22 | `Registry.Resolve` | several of F15–F21 at once | Invalid joining all (also when F18 is mixed with other problems) | fix them all | `TestResolve_ReportsAllProblems` |
| F23 | `Registry.Resolve` | publisher or definer registered but disabled; used capability with no provider | none: `Warning` in the plan | `app` logs at warn | `TestResolve_DisabledOtherSide_Warns` |
| F24 | `Plan.Build` | `provision` fails | its kind, wrapped with the module name | stop start-up | `TestBuild_ProvisionFails_WrapsWithModule` |
| F25 | `Plan.Build` | `Deps.Logger`, `Deps.Tracer` or `Deps.Clock` nil after `provision` | Invalid | fix app | `TestBuild_DepsMissingLoggerTracerOrClock_ReturnsInvalid` |
| F26 | `Plan.Build` | `CheckSchema` fails for one or more modules | the checker's kind (Conflict for a version mismatch, Unavailable, Timeout), all joined, nothing built | run `bms migrate` / fix the database | `TestBuild_SchemaMismatch_ReportsEveryModuleAndBuildsNothing` |
| F27 | `Plan.Build` | a factory returns an error | its kind, wrapped with the module name | stop start-up | `TestBuild_FactoryFails_WrapsWithModule` |
| F28 | `Plan.Build` | a factory returns `(nil, nil)` | Internal | fix the module | `TestBuild_FactoryReturnsNilModule_ReturnsInternal` |
| F29 | `Plan.Build` | `Provides` non-empty but no `CapabilityProvider`; keys differ from `Provides`; a value nil; `CapabilityProvider` with non-empty map but empty `Provides` | Invalid, naming the module and capability | fix the module | `TestBuild_CapabilitiesMismatchManifest_ReturnsInvalid` |
| F30 | `Plan.Build` | the same for `ContributesTo` and `Contributor` | Invalid | fix the module | `TestBuild_ContributionsMismatchManifest_ReturnsInvalid` |
| F31 | `Plan.Build` | a contribution fails the definer's `Accepts` | Invalid, naming contributor, definer and point | fix the contributor | `TestBuild_ContributionRejectedByDefiner_ReturnsInvalid` |
| F32 | consumer `Factory` (obligation) | a capability value does not implement the contract interface | Invalid from the consumer, naming the capability | stop start-up | `TestBuild_ConsumerRejectsProviderType_FailsStartup` (with a fake consumer) |
| F33 | `Plan.Build`, `Route.Validate` | a route with a bad method, pattern not starting with `/` or containing a byte outside `[A-Za-z0-9._:/{}$-]` (a space, `%`, `+`, `,`, `;`), nil handler, out-of-range `Access`, or two routes with the same method and pattern | Invalid, naming module and route | fix the module | `TestBuild_InvalidOrDuplicateRoute_ReturnsInvalid` (its table includes `/caf%C3%A9` and `/a+b`; `/x/{$}` and `/files/{path...}` pass) |
| F34 | `Plan.Build` | a `RouteProvider` without a non-empty `OpenAPI()` (no `APIDescriber`, or an empty result) | Invalid | add the fragment | `TestBuild_EmptyOpenAPI_ReturnsInvalid`, `TestBuild_RouteProviderWithoutDescriber_ReturnsInvalid` |
| F35 | `Plan.Build` | `ctx` done | Canceled / Timeout | stop start-up | `TestBuild_ContextCanceled_ReturnsCanceled` |
| F36 | `Plan.Build` | `schema` or `provision` nil | Invalid | fix app | `TestBuild_NilSchemaOrProvision_ReturnsInvalid` |
| F37 | `Set.Start` | a module's `Start` fails | its kind, wrapped with the module name; earlier modules stopped in reverse under `StartRollbackTimeout` via `context.WithoutCancel(ctx)`; their `Stop` errors joined | stop start-up | `TestSetStart_ModuleFails_StopsEarlierInReverse`, `TestSetStart_ModuleFailsOnExpiredCtx_RollbackStillWaits` (the first module's recording `Stop` sees an undone ctx and completes; the second's `Start` fails with the caller's ctx already done) |
| F38 | `Set.Start` | already started | Conflict | fix app | `TestSetStart_Twice_ReturnsConflict` |
| F39 | `Set.Stop` | one or more modules' `Stop` fail | joined, all modules still stopped; `Timeout` if `ctx` ends | log and exit | `TestSetStop_ContinuesPastFailuresAndJoins` |
| F40 | `Links.Capability` | name not in the owner's `Requires` or `Uses` | Invalid | fix the module (declare it) | `TestLinksCapability_Undeclared_ReturnsInvalid` |
| F41 | `Links.Capability` | in `Uses`, no enabled provider | NotFound | hide the feature | `TestLinksCapability_UsedButAbsent_ReturnsNotFound` |
| F42 | `Links.Contributors` | point not in the owner's `Defines` | Invalid | fix the module | `TestLinksContributors_Undeclared_ReturnsInvalid` |
| F43b | `Set.Contributors` | point not in `Env.CoreExtensionPoints` | Invalid | fix the core definer | `TestSetContributors_NotCorePoint_ReturnsInvalid` |
| F43 | `Links.Contributors`, `Links.Capability` | called before `Build` froze the links (`Contributors` from a factory), or on a zero `Links` | Internal | move the call to `Start` or request time | `TestLinks_BeforeFreezeOrZero_ReturnsInternal` |
| F44 | `PersonData.ExportPerson`, `DeletePerson` (obligation) | zero `PersonID` | Invalid | 400 (people) | `moduletest`: `Conformance/export_zero_id`, `delete_zero_id` |
| F45 | `PersonData.DeletePerson` (obligation) | database read-only | Unavailable wrapping `db.ErrReadOnly` (the kind `db` returns for a write) | "this copy is read-only" | `moduletest`: `Conformance/delete_read_only` |
| F46 | `Module.Start` (obligation) | database read-only | none: `Start` succeeds | — | `moduletest`: `Conformance/start_read_only` |
| F47 | `Module.Stop` (obligation) | `ctx` already done | Timeout or Canceled within the deadline, never a hang | log | `moduletest`: `Conformance/stop_honors_context` |
| F48 | `Conformance` | a name in `Requires` has no `WithCapability` | `t.Fatal` naming the module and the capability | add `WithCapability(name, fake)` | `TestConformance_RequiredCapabilityWithoutFake_FailsEarly` |
| F49 | `Conformance` | `WithCapability` names a capability not in `Requires` or `Uses` | `t.Fatal` naming it | fix the test (or declare it) | `TestConformance_UndeclaredCapabilityOption_FailsEarly` |
| F50 | `Conformance` | `WithCapability` with a nil `impl` | `t.Fatal` naming the capability | pass a fake | `TestConformance_NilCapabilityImpl_FailsEarly` |
| F51 | `Conformance` | the same capability given twice | `t.Fatal` naming it | remove one | `TestConformance_DuplicateCapabilityOption_FailsEarly` |
| F52 | `Conformance` | neither or both of `WithSeed` and `WithoutPersonData`; a `WithSeed(nil)`; a zero `Option` | `t.Fatal` saying which | choose one | `TestConformance_SeedOrWithoutPersonDataRequired_FailsEarly` (table: neither, both, nil seed, zero option) |
| F53 | `Conformance/rejects_wrong_capability_type` (obligation on the consumer, F32) | `Build` succeeds, or fails with a kind other than Invalid or without naming the capability, when the stub returns `struct{}{}` | the rule fails | assert the type in the Factory with `errs.Invalid` naming the capability | mutant `acceptsAnyCapabilityType` in `TestConformance_BrokenModule_FailsTheNamedRule` |
| F54 | `Conformance/used_capability_absent_builds_and_starts` (obligation, D19) | `Build` or `Start` fails without a provider for a `Uses` name | the rule fails | treat `NotFound` from `Links.Capability` as "hide the feature" | mutant `needsUsedCapability` |
| F55 | `Conformance/seeded_person_exports_data` (obligation) | export of the seeded person is empty or not an object | the rule fails | wire the factory to the fake store the seed writes to; make `ExportPerson` cover every table | mutant `seedIgnored` |
| F56 | `Conformance/seeded_person_delete_counts_and_is_idempotent` (obligation) | zero `Deletion` after a seed; a `Kept` with empty `Kind` or `Count < 1`; data still exported after a delete with no `Kept`; a second delete not zero | the rule fails | make `DeletePerson` cover every table and report counts | mutants `deleteKeepsSeeded`, `deleteReportsZero`, `deleteNotIdempotent` |
| F57 | `Conformance/no_personal_data_in_logs` (obligation, privacy-data) | a marker, or `[email]`, in any captured message, attribute, span name or stdout; or any `Violation` at cleanup | the rule fails | log IDs, never fields | mutants `logsSeededName`, `logsSeededEmailUnderBenignKey` (caught as `[email]`), `logsSeededEmailUnderDeniedKey` (caught as a `Violation`), `logsSeededNote`, `spansSeededPhone` |
| F58 | `Conformance/templates_register` (obligation, D39) | a `Manifest.Templates` file does not parse, has a bad header, is not named `<ident>.<lang>.tmpl`, or an ident lacks `en` | the rule fails naming the file | fix the template (the same checks as `notify.Register`) | `TestConformance_BadTemplate_FailsRule` |
| F59 | `Conformance/no_personal_data_in_logs` (obligation, D39) | a seed marker or `[email]` in a `notifytest.Fake.Sent()` var or link | the rule fails | pass only the recipient's own data, never a marker-bearing value (notify decision 10) | `TestConformance_NotifyVarLeak_FailsRule` |

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
  `TestResolve_EdgesAndKinds` (one edge per requires/uses/contributes/subscribes between enabled modules, core
  edges named by area; an edge to a disabled module is a Warning, not an Edge); `TestResolve_CoreSignalsAndPointsSatisfyChecks`; F15–F23.
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
  `Check`, `Defines` as names only).
- Lint: passes depguard rules `core` and `storage-seam` (no driver import), `errcheck`,
  `errorlint`, `wrapcheck`, `sloglint`.

**Conformance** (`moduletest.Conformance`, run by every module and by the `hello` module as the reference; each rule is a named subtest, run in order; it stops at the first failure). F48-F52 are precondition failures before the first rule. Rules, in order:
`manifest_valid`, `registers_under_its_name` (a fresh registry `Add` then `Resolve` with only this module enabled and its required capabilities faked succeeds), `implements_declared` (`Build` passes F29-F34 against the manifest, with the option's `impl` behind each capability), `rejects_wrong_capability_type` (for each name in `Requires` and `Uses` in turn the stub returns `struct{}{}` for that name and the configured value for the rest; `Plan.Build` must fail with `errs.Invalid` naming the capability: F32 proven per module; trivial without either), `used_capability_absent_builds_and_starts` (no provider for any `Uses` name; `Build`, `Start`, `Stop` succeed; trivial without `Uses`), `start_read_only` (F46, with `dbtest.NewFake()` after `SetReadOnly(true)` and `signalstest.NewFake(m.Name, m.Publishes, m.Subscribes)`, so `Start` also proves it subscribes only to declared names), `subscribes_every_declared_name` (after `Start`, `Fake.Handlers()` equals `Subscribes`), `stop_honors_context` (F47), `stop_without_start_is_nil`, `export_zero_id`, `delete_zero_id` (F44), `export_unknown_person_is_empty` and `delete_unknown_person_is_zero_and_idempotent` (plain `dbtest.NewFake()`, unexported unknown ID differing from `Subject.Person`), `delete_read_only` (F45), `export_is_json_object`, `seeded_person_exports_data` (with `WithSeed`: `Seed`, then `ExportPerson(Subject.Person)` is a JSON object with at least one non-empty value, so the leak rule is not vacuous), `seeded_person_delete_counts_and_is_idempotent` (with `WithSeed`: `Seed`, then `DeletePerson` returns `Deleted + Anonymized + ΣKept.Count ≥ 1`, every `Kept` has a non-empty `Kind` and `Count ≥ 1`; a second `ExportPerson` is empty unless `Kept` was non-empty; a second `DeletePerson` is a zero `Deletion`), `routes_under_prefix` (patterns start with `/`; no absolute `/api/`), and `no_personal_data_in_logs` (`Logger` and `Tracer` from `telemetrytest.New(t)`; `Build`, `Seed` if given, `Start`, `ExportPerson` and `DeletePerson` of `Subject.Person`, `Stop`; then `!Contains` for `LegalName`, `Phone`, `Note` and `Email`, and `!Contains("[email]")`, the token telemetry.md's `Scrub` substitutes for e-mail-shaped text; the capture fails the test at cleanup on any `Violation`, which covers a denied key such as `email` or `legal_name`). `WithoutPersonData` makes the seeded rules pass trivially and the leak rule run without a seed.

`moduletest`'s own tests keep the child-process pattern (`TestConformanceHelper` with a mutant name in the environment) and add: `TestConformance_RequiredCapabilityFaked_Passes` (a module that `Requires` `alphacap` and asserts it, with `WithCapability("alphacap", fakeAlphaCap{})`); `TestConformance_UsedCapabilityPresentAndAbsent_Passes`; `TestConformance_RunsEveryNamedRule`; the F48-F57 tests; `TestConformance_SeedRunsPerRuleOnFreshModule` (a counting seed sees a different module value each time, three times); `TestConformance_WellBehavedModule_Passes` (with `WithSeed` over an in-memory fake that stores all five `Subject` fields and exports them); `TestIsCoreArea_Table`; `TestSetStart_*` as in F37. The `hello` module calls `Conformance(t, Manifest, factory, provision, moduletest.WithSeed(seed))`, where `factory` builds the module over its in-memory fake store and `seed` saves one greeting for `subject.Person`; the production `New` cannot be seeded because `dbtest.Fake` holds no rows. Integration of a real module end to end (routes
mounted, migrations applied, signals delivered) is `app.md`'s and the `hello` module's
integration test, behind the `integration` tag.

### The `hello` reference module (09 §1)

Specified separately (`module-hello.md`; its Proposed change 1 applied this paragraph
2026-10-17). To "exercise every part of the contract" it declares two permissions
(`hello.greetings.read` and `hello.greetings.write`, both `ScopeSite`, `Reaches` internal),
one setting (`greeting`, text, default `"hello"`), publishes `hello.greeted` (payload
`{person}`, inside the transaction that saves a greeting) and subscribes to it, provides
capability `greeter` (`contracts/greeter`: `Greeter{Greeting(ctx) (string, error)}`) and
defines point `hello.banner` (shape `greeter.Banner{Key}`), contributes to its own point,
ships one migration creating `hello.greeting`, one notification template `hello.greeted` (D39, sent from `Greet` so the reference covers the path; `module-hello.md` carries the detail), serves `GET /public-greeting` (public),
`GET /greeting` (signed in) and `POST /greetings` (signed in) with an OpenAPI fragment,
implements `PersonData` over the greetings a person saved, starts one goroutine (the
counter), opens one span per use case in its service through `Deps.Tracer` (children of the
api's request span), and passes `Conformance`. Its subscription to a core signal is
deferred until `people.person-deleted` exists.

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

1. **`plan/07-abstractions.md`, `module` entry:** (applied, verified 2026-10-17) replace "Owns" with: the `Manifest`
   (static declaration), the `Module` interface and its optional interfaces, `Deps` (what the
   core gives a module), the registry, `Resolve` (graph check, build order, graph for the
   admin screen), `Build` (schema check per D17, verification against the manifest,
   linking), `Set` (start and stop in order), and the mandatory `PersonData` (export and
   deletion of one person's data). Add to the layer rules: "`core/module` is the top of the
   core graph: it imports core contracts; no core package imports it." Add to the `authz`
   entry: owns the `Permission`, `Scope` and `PermissionDecl` types; to the `signals` entry:
   owns the `Name` type. Note that module names are letters and digits only (`afterreports`).
2. **`plan/design/authz.md` (row 7):** (applied 2026-10-18, by authz.md: `Reaches` shown in the editor, rules may refer to it, `core.modules.read` is api's) adopt `authz.Permission`, `authz.Scope` and
   `authz.PermissionDecl` as defined in this spec's "Types"; receive `Plan.Permissions()`
   from `app` at start-up; show `Reaches` in the role editor and allow separation-of-duties
   rules to refer to it ("may never hold a permission reaching sensitive"); declare
   `core.modules.read` (or let `api.md` do it).
3. **`plan/design/signals.md` (row 10)** (applied 2026-10-16, by signals.md's Proposed
   change 1: names adopted unchanged, scoping as asked, subscriptions in `Start`): adopt
   `signals.Name` as defined here; the bus
   handed to a module through `Deps.Signals` is scoped by `app` to the module's `Publishes`
   and `Subscribes` and fails with `errs.Invalid` outside them; the core's published names
   are listed for `Env.CoreSignals`; subscriptions are registered in `Module.Start`.
4. **`plan/design/db.md` (row 5):** (applied, verified 2026-10-17: db.md carries `SchemaChecker`, `ErrReadOnly` and the read-only state) implement `module.SchemaChecker` (message names the
   module and both versions, says to run `bms migrate`; `Conflict` on mismatch or missing
   row); define the migration file layout for `Manifest.Migrations`
   (`postgres/migrations/NNNN_name.sql`, highest number is the version); `bms migrate`
   applies the core first, then `Plan.Order()`; name the error kind a write gets on a
   read-only copy (proposed `Unavailable` with a sentinel `db.ErrReadOnly`, so the api can
   say "this copy is read-only" and `retry` would not hammer it); expose read-only state for
   `/readyz`. The `db` integration test proves pgx scans `model.ID[E]` (model.md Proposed
   change 4) and that `CheckSchema` works on an empty database.
5. **`plan/design/settings.md` (row 15):** (applied 2026-10-19, D39, with one deviation: no automatic retention keys, the module that runs the job declares them, settings decision 11; the types are aliased from `core/settings`) consume `Plan.Settings()` (`SettingDecl`: kinds,
   defaults, options, `Validate`); store values under `<module>.<key>`; add the retention
   settings per module and sensitivity level automatically (D30, default keep forever);
   give modules a scoped `Deps.Settings`; show titles and descriptions through the message
   keys of decision 8. Consider department-scoped settings later; this spec declares
   site-wide ones only.
6. **`plan/design/api.md` (row 16)** (applied 2026-10-17, by api.md's Proposed change 3:
   mounting as `<METHOD> /api/<name><pattern>` with trailing slashes exact; the fragment
   form in its decision 11; `GET /api/modules` split into a public list of enabled names
   and `GET /api/admin/modules` behind `core.modules.read` for the graph and versions (its
   decision 27); `db.ErrReadOnly` → `503 read_only`; this spec amended: `RouteProvider`
   must implement `APIDescriber` (F34), `Route.Validate` enforces the pattern character
   set (F33), the `Route` doc notes exact trailing slashes, `{path...}`, the token grammar
   and that `GET`/`HEAD` never change state, `ModuleRoutes` is also the core sets' shape,
   and `core.modules.read` is declared by `api`): mount `Set.Routes()` under `/api/<name>/` with the
   session middleware applied per `Route.Access`; define the OpenAPI fragment form for
   `APIDescriber` and merge the fragments; serve `GET /api/modules` from `Plan.Graph()` plus
   `db`'s versions, behind `core.modules.read`; map `db`'s read-only kind to the
   "this copy is read-only" response.
7. **`plan/design/web-shell.md` (row 17)** (applied 2026-10-17: each set ships `web/src/modules/<name>/messages/<lang>.json`, flat JSON with ICU messages, `en` required; the manifest-derived keys are checked by a Go test in `internal/app`, `TestMessageCatalogues_EnglishCoversManifests`, skipped when `web/` is absent; screens mount only when `GET /api/modules` lists the module; the admin "modules" screen renders the graph as tables with the warnings; the web registry lists core areas too, `kind: "core"`, always enabled): each module's folder ships
   `messages/<lang>.json` with the keys of decision 8; a build-time check (or a test) that
   `en` has every key implied by the module's declarations (the build can read the Go
   manifest through a generated JSON, or the check runs in Go against the catalogue files);
   show a module's navigation and screens only when `GET /api/modules` lists it as enabled;
   the admin "modules" screen renders the graph with its warnings.
8. **`plan/design/people.md` (row 8):** (applied 2026-10-18, by people.md) orchestrate export and deletion over
   `Set.PersonData()` behind `people.data.export` and `people.data.delete`, in one audited
   operation; assemble `{ "<module>": <Export.Data>, ... }`; report `Deletion` per module;
   declare the person-page extension point (`people.person-page`, with its `Accepts`) in
   `Env.CoreExtensionPoints` and the signals the core publishes (`people.person-created`,
   `people.person-deleted`) in `Env.CoreSignals`; call `Links.Contributors` at request time.
   `departments-events.md` (row 9) likewise declares `events.event-setup` as the extension
   point for copying into a new event (09 §5) and the seven `departments.*` and `events.*`
   signals in `Env.CoreSignals` (applied 2026-10-18, D37; see the Amended line and the Deps table).
9. **`plan/design/app.md` (row 18):** (applied 2026-10-17, by app.md "`serve`, step by step"; `Build` and `Set.Start` run under 30 s, `Set.Stop` shares `http.shutdown_timeout`, replacing the proposed 30 s and 15 s) `serve`: `Default().Resolve(env)` → log warnings →
   `Build` (with `db` as the checker and a `Provision` that builds scoped `Deps`) → hand
   `Plan.Permissions()` to `authz`, `Plan.Settings()` to `settings`, `Set.Routes()` to `api`,
   `Set.PersonData()` to `people` → `Set.Start` → serve → `Set.Stop` with deadlines (proposed
   30 s and 15 s). `check`: `Resolve` plus `CheckSchema` per enabled module, no build.
   `migrate`: `Resolve` then `db` over `Plan.Order()`. `export`/`delete` by command (09
   §11): through `people`.
10. **`plan/design/config.md` (row 3):** (applied, verified 2026-10-17) `modules.disabled` (list of module names, default
    empty, each must be registered; `BMS_MODULES_DISABLED` as a comma-separated list);
    `app` derives `Env.Enabled`. Opt-out enablement confirmed by the owner (2026-10-16;
    decision 7).
11. **`.claude/skills/go-conventions/SKILL.md` and `.claude/skills/add-module/SKILL.md`:** (applied, verified 2026-10-17)
    the registration line is `module.Register(Manifest, New)` in `init`, with
    `var Manifest = module.Manifest{...}` and `func New(ctx context.Context, deps module.Deps)
    (module.Module, error)` in `module.go`; `privacy.go` implements `module.PersonData`;
    `Start` registers signal handlers and jobs and never writes; module names are letters
    and digits only; titles and descriptions are catalogue keys (decision 8); every module's
    tests call `moduletest.Conformance`. `add-module` §1 adds "the Reaches level of each
    permission" to the spec checklist.
12. **`plan/design/README.md`:** (applied, verified 2026-10-17) add row `4b` `module-hello.md`, "the reference module
    (M0)", Not started; it depends on this spec, `db`, `signals` and `api`.
13. **`plan/design/jobs.md` (row 14) and `notify.md` (row 11):** (notify applied 2026-10-19, D39: `Deps.Notify` scoped by name, `Manifest.Templates`, `notifytest.NewFake` in `Conformance`, the rule `templates_register`; jobs pending: target not written) `Deps.Jobs` and
    `Deps.Notify` are scoped to the module (job and template names prefixed `<name>.`); jobs
    are registered in `Module.Start` and run only on the writing copy.
14. **`plan/02-scope.md`, `plan/09-core-features.md`:** (applied, verified 2026-10-17) note that `after-reports` is
    `afterreports` in code, schema and URLs (confirmed by the owner, 2026-10-16; decision
    10); 09 §4's "a
    flag for whether they reach sensitive data" becomes "the highest sensitivity level they
    reach" (model.md Proposed change 3).
15. **`plan/design/module-hello.md`, Test plan "Conformance":** (applied 2026-10-17) call `Conformance` with a
    factory over the in-memory fake store and `moduletest.WithSeed(seed)`; the seed saves
    one greeting for `subject.Person`; note that the production `New` cannot be seeded.
16. **`.claude/skills/go-conventions/SKILL.md` and `.claude/skills/add-module/SKILL.md`:** (applied 2026-10-17)
    "every module's tests call `moduletest.Conformance` with `WithSeed` (or
    `WithoutPersonData` when the spec's Data section lists nothing about a person) and
    `WithCapability` for each capability the module requires or uses".
17. **`plan/design/telemetry.md`, `plan/design/app.md`:** none; decision 17 relies on `Scrub`'s
    documented `[email]` substitution as it stands, and decision 20 needs no app.md change.
18. **`plan/design/settings.md` (row 15) and `notify.md` (row 11), the key grammar and the types (D39):** (applied 2026-10-19) see the Amended line, "Settings" and "Types" above: setting keys use underscores, `SettingKind`/`SettingDecl` are aliases of `settings.Kind`/`settings.Decl`, `regional` is reserved, `Manifest.Templates` is optional.
