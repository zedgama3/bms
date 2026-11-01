# Boundary cleanup

- **Status:** Approved
- **Package:** `internal/app` (shrinks), `internal/core/*` (ownership moves), new `internal/core/auditlog`, `internal/core/regional`, `internal/core/datarights`; `internal/lib/httpx` (one type); `scripts/`, `Makefile`, `.golangci.yml` (guards)   **UI:** `web/src/core/settings`, `web/src/core/people`, `web/src/shell` (route paths only)
- **Depends on specs:** app, audit, authz, identity, people, notify, settings, files, demo, jobs, signals, departments-events, module-contract, api (Summaries)   **Used by:** every later spec (the tenets and the seam table bind them)

## Summary

**Purpose:** make every piece of core code belong to exactly one abstraction, with the composition root wiring and never deciding, and keep it that way. A refactor: the schema, permissions, user-visible behavior and the module contract stay; the HTTP paths of two extracted areas, one signal name and the audit action names of moved code change (decisions 7, 8, 11). The owner approved the spec and its answers to the former open questions on 2026-10-31 (decisions 12–17).

**Two new tenets** (Proposed changes 1–3): *app wires, it never decides* (app calls constructors, adapts shapes and forwards; it parses no setting and makes no domain decision; each package reads its own settings area) and *no hidden two-way dependencies* (if component A needs B at runtime and B needs A, one of them is split; every surviving late-bound adapter is listed in the seam table in `07-abstractions.md` with its reason). The unit of the second rule is the **constructed component**, not the Go package: a package may expose a read-side `Lookup` built below and a `Service` built above (decision 2).

**Decisions:**
1. The superuser alert, the settings→policy translations (`settingsPolicy`, `peoplePolicy`, `smtpSource`, `retentionSource`, `siteSource`, `addressSource`, `languageSource`, `nameSource`, `filesPolicy`), `auditPurge`, `auditActor`, `authzActor`, `demoObjects`, `demoAccess`, `demoProfiles` and the store probes in `check.go` leave `app` for their owners. `check.go` (printing, exit status), `usage.go`, `demoSchema` and the commands stay: they are app's.
2. Read-side components built below the services: `authz.Lookup` (decisions, `PermissionsOf`, `TitlesOf`, `HoldersOf`, `StarterRoleIDs`), `departments.Lookup` (`NamesOf`, `Active`), `people.Lookup` (`NamesOf`, `PlayaNameOf`, `LanguageOf`, `ContactEmailOf`, `ConsentOf`), `identity.Lookup` (`EmailOf`), `demo.Guard`, `demo.Capture`, `jobs.Deferrer`. Each is permission-free system-level reading (or one append), needs nothing built after it, and is its package's own code.
3. `audit` splits: `audit` keeps the recorder (`Entry`, `Recorder`, `Log`, spool, importer, ctx markers `WithSystem`/`WithOperator`); new `auditlog` owns `Search`, `Export`, `CountAbout`, the `audit.*` settings (replacing `auditsettings`), the `audit.purge` job and `/api/audit/`.
4. New `regional` owns the regional's profile: the `regional.*` settings area, `Profile`, the public document, `theme.css`, the logo, under `/api/regional/`. `settings` is only the typed store, its screens and terminology, and no longer imports `files`.
5. New `datarights` owns per-person export and deletion across the core and the modules, deletion requests (`core.data_request`), the admin notice, the `export` and `delete` commands, `/api/datarights/`; `module.PersonData`, `Export`, `Deletion`, `Kept` become aliases of its types. `people` keeps profiles, contacts, consents, search, linking, the person page, and answers for its own rows as one more source.
6. Each package reads its own settings area through a `settings.Reader` in its `Options` (`identity`, `people`, `notify`, `files`, `auditlog`, `regional`); `filesettings` and `auditsettings` are deleted.
7. One route shape: `httpx.Route` in `lib/httpx`; `module.CoreRoutes(area, routes, openapi)` converts; the nine per-package `Route` types and nine app converters go.
8. New build order (Behavior, "The build order") removes all 14 `lazy` types, the three `demo` structs that hold the assembly and the one closure. The seam table ends empty; the check asserts it.
9. `files` gains `Options.Settings`, `Options.Ceiling` (demo's constant caps applied by files) and `Purge`; `notify` gains `Options.Accounts`, `Profiles`, `Regional`, `Terms`, `Channel`, `Settings` and the core-only `AlertSender`; `identity` gains `Options.Settings`, `Options.Alerts`, `Alert` moves in; `demo` gains `NewGuard`, `NewCapture`, `FileCeiling`, `StateOf`; `jobs` gains `NewDeferrer`; `notify` gains `Probe` for `bms check`.
10. Guards: `TestSeamTable_MatchesApp` (go/ast over `internal/app`, against the table) under `make imports`; depguard rule `app-no-stores`; `make arch-report`; a whole-repo architecture review at every milestone close (`build-loop` step 5); reviewer and architect checklist lines.
11. Names that change: routes `GET /api/settings/public|theme.css|logo`, `PUT|DELETE /api/settings/logo` → `/api/regional/...`; people's export, deletion and request routes → `/api/datarights/...` (table under Interface); signal `people.deletion-requested` → `datarights.deletion-requested` (signals' owner rule; no subscriber exists); audit actions `settings.logo.*` → `regional.logo.*`, `people.data.*` and `people.request.*` → `datarights.*` (audit F9: an action belongs to its recorder's area). Permission names, tables, columns and migrations do not change.
12. Permission names `people.data.export` and `people.data.delete` stay for now (owner, 2026-10-31); a rename with a `core.role_permission` data migration may come later, recorded in datarights' spec.
13. `regional` and `datarights` record audit entries under their own areas, not under `settings` and `people` (owner, 2026-10-31); old entries keep the old names.
14. The path moves to `/api/regional/` and `/api/datarights/` are accepted (owner, 2026-10-31).
15. The milestone architecture review is run by `go-reviewer` with a whole-repo prompt; no new agent (owner, 2026-10-31).
16. The four runners (`importer`, `refresher`, `notifyRunner`, `jobsRunner`) collapse into one generic runner over `Run`/`Close` in step 13, if the step-13 reviewer agrees (owner, 2026-10-31).
17. `profiles.go` is not split now; the arch report flags it at the next milestone close (owner, 2026-10-31).

**Exported names added:**
- `audit.WithSystem`, `audit.WithOperator`; `auditlog.New`, `auditlog.Options`, `auditlog.Settings`.
- `authz.NewLookup`, `authz.Lookup`, `authz.LookupOptions`; `departments.NewLookup`; `people.NewLookup`, `people.Lookup`; `identity.NewLookup`, `identity.Lookup`, `identity.Actor`, `identity.AuditActor`.
- `notify.Accounts`, `notify.Profiles`, `notify.RegionalSource`, `notify.TermSource`, `notify.AlertSender`, `notify.Probe`, `notify.EmailState`.
- `regional.New`, `regional.Profile`, `regional.Settings`, `regional.LogoRef`, `regional.MaxLogoBytes`, `regional.RevisionSource`, `regional.TermSource` (A2).
- `settings.Service.Revision`, `settings.Service.Revise`, `settings.Decl.Normalize` (A2).
- `datarights.New`, `datarights.Options`, `datarights.PersonData`, `datarights.Export`, `datarights.Deletion`, `datarights.Kept`, `datarights.Source`.
- `files.Purge`, `files.Settings`, `files.Options.Ceiling`; `demo.NewGuard`, `demo.NewCapture`, `demo.FileCeiling`, `demo.StateOf`; `jobs.NewDeferrer`; `httpx.Route`; `module.CoreRoutes`.

**Removed:**
- `app`: all `lazy*` types, `alert.go`, the eight settings adapters, `auditActor`, `authzActor`, `auditPurge`, `demoAccess`, `demoProfiles`, `demoObjects`, `demoChannelFactory`, `personData`, `notifyData`, `filesData`, the nine `*Routes` converters, and the `permissions`, `titles`, `lister`, `names`, `notifiers`, `filesPolicy`, `operator` assembly fields.
- Packages `audit/auditsettings`, `files/filesettings`.
- `settings.Regional`, `settings.Public`, `settings.ThemeCSS`, the logo methods; `people`'s data-rights methods.
- Seams: `identity.Alerter`, `identity.PolicySource`, `people.PolicySource`, `files.PolicySource`, `notify.SMTPSource`, `SiteSource`, `RetentionSource`, `AddressSource`, `LanguageSource`, `NameSource`, `ChannelFactory`; `demo.Service.Policy`, `Channel`, `Protected`; `audit.Options.Permissions` (`Titles` stays, required).

**errs kinds a caller must handle:** unchanged for every moved method. New: `files.Purge` `Unavailable` (store, read-only copy); `notify.AlertSender.SendAlert` `Unsupported` (no address configured); `demo.Guard.Protected` fails only with the store's kinds (`Unavailable`), never "not built"; `jobs.Deferrer.Defer` `Unavailable` wrapping `db.ErrReadOnly` on a read-only copy; `module.CoreRoutes` `Invalid` for a bad route; every `New` `Invalid` for a nil dependency.

**Routes, signals, permissions, settings owned here:** none new. Ownership moves are listed under Interface, Signals, Permissions and Settings.

**Migration:** thirteen steps (Behavior, "Staged migration"), each leaving `make check` green, each naming the adapters it removes and the tests that prove behavior unchanged. Step 1 lands the seam test with today's 18 rows (19 after A1); step 13 empties the table.

**Expects of other specs:** app.md gives up "The superuser alert", the lazy amendments and the part order; audit, authz, identity, people, notify, settings, files, demo, jobs, departments-events, module-contract and api take the amendments listed under Proposed changes elsewhere; `spec-editor` applies them after approval.

## Purpose

Make each piece of core code belong to exactly one abstraction, and keep it so.

## Responsibilities

- Settle, for every finding in the review, which abstraction owns the code, with the reason.
- Define the components that replace the late-bound adapters, with their interfaces and failure modes.
- Define the two tenets, the seam table, the mechanical checks and the review cadence that keep ownership from drifting again.
- Give an order of steps, each small enough for the build loop and each leaving the tree green.

## Not responsible for

- New user-visible features (none; the owners' specs).
- Changing the database schema, any migration, any permission name or the module contract's semantics (unchanged; `db`, `authz`, `module-contract`).
- The wording of the tenets in `00-principles.md`, `07-abstractions.md` and `CLAUDE.md`, the checklist lines in the agents and skills, and the amendments to other specs: proposed here, applied by `spec-editor` after approval.
- Privacy classifications of the moved fields (unchanged; the owning specs).
- The offline switchover, packaging, or anything on the roadmap after this.

## Sub-abstractions

| Name | Purpose (one line) | Notes |
|---|---|---|
| `auditlog` | Let the right people read, export and expire what the audit recorder wrote. | New package, split from `audit` (decision 3). |
| `regional` | Hold the regional's public profile: name, contact, URL, language, timezone, colours, logo. | New package, split from `settings` (decision 4). |
| `datarights` | Answer "everything about person X" and "delete person X" across the core and every module. | New package, split from `people` (decision 5). |
| `authz.Lookup` | Decide and look up permissions without writing. | Read side of `authz`, built below `audit` (decision 2). |
| `departments.Lookup`, `people.Lookup`, `identity.Lookup` | Answer system-level reads of a package's rows for other core parts. | Read sides, built below the services that need them. |
| `demo.Guard`, `demo.Capture` | Protect the shared demo accounts; capture outgoing mail. | Built below `identity` and `notify`; inert unless demo mode. |
| `jobs.Deferrer` | Store a failed signal delivery for redelivery. | Built below the dispatcher. |
| Seam table and `TestSeamTable_MatchesApp` | Make every late-bound adapter in `app` visible and justified, or absent. | The guard for tenet 2. |
| `arch-report` and the milestone review | Find ownership drift before it compounds. | The guard for tenet 1. |

## Findings, verified, and the decisions

Each finding was checked against the code on `feat/m1` (2026-10-30). Line counts are production lines.

### F1. `internal/app` holds domain behavior

Verified. `internal/app` has 5,673 production lines across 32 files. Of those, the following decide rather than wire:

| Code | What it decides | Owner | Why |
|---|---|---|---|
| `alert.go` (112 lines): `lazyAlerter`, `alertSeams.send` | who is told of a superuser sign-in, the deadline, the link, that an empty `Key` is used | `identity` | the alert is identity's reaction to its own event (identity.md F27); it lived in app only because notify and authz were built later. |
| `core_settings.go`: `settingsPolicy`, `peoplePolicy` | how `accounts.*` and `people.*` keys map onto `identity.Policy` and `people.Policy` | `identity`, `people` | the owner declares the keys (`identity.Settings`, `people.Settings`) and validates the policy; the mapping between them is the same knowledge. |
| `core_notify.go`: `smtpSource`, `retentionSource` | how `notify.*` keys map onto `SMTPConfig` and the retention | `notify` | same. |
| `core_notify.go`: `siteSource` | the language fallback (primary subtag, then the regional's default) and the four-form flattening of terms | `notify` | a rendering rule of notify's templates. |
| `core_notify.go`: `addressSource`, `languageSource`, `nameSource` | login address first, else contact address; `NotFound` means "none" | `notify` | the delivery rule. The two lookups stay where the rows are (`identity`, `people`). |
| `core_files.go`: `filesPolicy` | how `files.*` keys map onto `files.Policy` | `files` | same as the policies above. |
| `core_jobs.go`: `auditPurge` | the retention job's rule (0 keeps forever; purge older than now minus retention) | `auditlog` | the retention setting is audit's (D30, D39). |
| `core_audit.go`: `auditActor` | who the actor is: the system inside a job, else the session, else the operator | `audit` (the order), `identity` (the session) | "who is this" is identity's; "a job runs as the system" is a marker jobs sets and audit reads. |
| `core_adapters.go`: `authzActor` | the person of a full session plus the superuser flag | `identity` | same. |
| `core_demo.go`: `demoObjects.Clear` (buckets, the temp directory layout) | which buckets exist, where temp files live | `files` | files knows the owners it scoped and its temp directory. |
| `core_demo.go`: `demoAccess`, `demoProfiles` | nothing; they forward and project because the demo seams did not match the services' signatures | `demo` | the seams take the services' shapes. |
| `check.go`: `smtpConfig` and `email` build a `notify/postgres` store and an `smtp` channel; `demo` builds a `demo/postgres` store | how to probe notify and demo | `notify`, `demo` | app must not touch another package's store (Proposed change 6 adds the depguard rule). |

Kept in app, with the reason:

- `check.go` (616 lines) is the `check` command: the step order, printing, status ranking. After the probes move it calls `notify.Probe`, `demo.StateOf`, `audit.SpoolCounts` and prints. Wiring.
- `usage.go` (132 lines) turns the web build's client manifest into the allowlists the two intake services take. Adapting one package's output into another's `Options` is wiring.
- `core_demo.go`: `runDemo`, `demoStatus` (exit statuses are app's), `demoSchema` (the reset's migrate step is `bms migrate` re-run: app's own command sequence), `logDemo`.
- The runners (`importer`, `refresher`, `notifyRunner`, `jobsRunner`): they start and stop a service's `Run` under app's lifecycle. Wiring. They may later collapse into one generic runner (decision 16); done in step 13, not before.
- `settingsAreas`, `declaredPermissions`, `demoSchemas`, the `link*` functions: collecting declarations from the parts and the plan.

### F2. Hidden runtime cycles

Verified: 14 late-bound types (`lazyPermissions`, `lazyTitles`, `lazyLister`, `lazyNames`, `lazyDepartments`, `lazyAuthorizer`, `lazyFilesStore`, `lazyIdentityPolicy`, `lazyPeoplePolicy`, `lazyAlerter`, `lazyGuard`, `lazyFilesPolicy`, `lazyNotifier`, `lazyDeferrer`) and one closure (`demoChannelFactory`). Corrections to the brief:

- `audit` does **not** import `authz`; it imports the leaf `authz/perm`. The import graph is acyclic as stated. The runtime cycle is real: `authz` records through `audit.Recorder`; `audit` asks `authz` for titles **when it records** (`record.go: stamp → titlesOf`), not only when it reads. Splitting audit into recorder and reader therefore does not break this pair on its own. What breaks it is that every authz call audit makes is a read: so `authz` splits into a read-side `Lookup` (built before audit, no recorder needed because decisions and lookups are not audited) and the admin `Service` (built after audit).
- `identity` already imports `settings` (for `identity.Settings`). The policy adapter sits in app only because identity is built before settings. Order, not imports, is the cause.
- `jobs ↔ signals` is a fifteenth pair the brief did not list: the dispatcher needs a `Deferrer` (jobs) and the runner needs the `Redeliverer` (the dispatcher).

The pairs and their resolutions:

| Pair | Runtime edges today | Resolution |
|---|---|---|
| audit ↔ authz | authz → Recorder; audit → `TitlesOf`, `Allow` | `authz.Lookup` below audit (titles, decisions); `authz.Service` above (grants, audited). `auditlog` above both (search needs `Allow`). |
| authz ↔ departments | departments → Authorizer; authz → `Active` | `departments.Lookup` below authz; `departments.Service` above. |
| identity ↔ authz | identity → `Allow`, `PermissionsOf`; authz → nothing of identity's at runtime (`identity.PersonFrom` is a ctx accessor) | order only: identity after authz. |
| identity ↔ people | people → `EmailOf`, `LinkAccount`; identity → `PlayaNameOf` | `people.Lookup` below identity; `people.Service` above. |
| identity ↔ notify | identity → `Notifier`, the alert; notify → `EmailOf` | `identity.Lookup` below notify; `identity.Service` above; the alert moves into identity. |
| people ↔ notify | people → `Notifier`; notify → `PlayaNameOf`, `LanguageOf`, `ContactEmailOf` | `people.Lookup` below notify (above); people's only remaining use of notify, the deletion-request notice, moves to `datarights`, built after notify. |
| settings ↔ authz | settings → Authorizer; authz → nothing | order only: settings after `authz.Lookup`. |
| settings ↔ files | settings → `files.Store` (logo); files → its policy from settings | the logo moves to `regional`, built after files; files reads its own area. |
| files ↔ demo | files → the demo cap; demo → `files` (Reset clears objects) | files takes a constant `Ceiling`; `files.Purge` serves the reset. |
| identity ↔ demo | identity → `Protected`; demo → `EnsureAccount` | `demo.Guard` below identity (a read of `core.demo_account`); `demo.Service` last. |
| notify ↔ demo | notify → the capture channel; demo → nothing of notify's at runtime, but is built last | `demo.Capture` below notify. |
| signals ↔ jobs | dispatcher → `Defer`; runner → `Redeliver` | `jobs.Deferrer` (one insert) below the dispatcher; the runner above it. |

No seam survives. The seam table in `07-abstractions.md` reads "None" at the end of step 13.

### F3. `core/settings` does two jobs

Verified: `regional.go` (333), `logo.go` (303), `public.go` (145) and parts of `http.go` are the regional's profile; the rest (decl, write, reader, snapshot, seal, terms, screens) is the store. The profile is why settings imports `files`. Decision: extract `regional` (decision 4). Terminology stays in settings: terms are declared per area by modules and the core and stored and edited like settings; `regional.language` is the default language terms are read in, which regional owns and settings reads through `TermSource`'s caller (notify).

### F4. `core/people` does two jobs

Verified: `datarights.go` (773), `notice.go` (147), the `Access`, `AuditCounter` and half of the `Accounts` seams, the `export` and `delete` commands and nine routes are the system-wide export and deletion; `profiles.go` (1,157), `contacts.go`, `consents.go`, `linking.go`, `membership.go` are the directory. Decision: extract `datarights` (decision 5). `profiles.go` itself is not split here: it is one concern (the profile use cases) and the size budget in the review (Behavior, "The architecture review") flags it for the owner's judgment at the next milestone close.

### F5. Smaller items

- **Route types.** Verified: nine core packages declare a `Route` (audit, authz, identity, people, departments, events, jobs, notify, settings, demo) and app converts each. Decision 7: `httpx.Route` in `lib/httpx` (the foundation; every layer may import it) and one converter `module.CoreRoutes` in `core/module` (the top of core, so rule 2 holds). `module.Route` and the module contract are untouched.
- **`core/demo` imports 12 core packages.** That is its purpose (a seed through the real services) and it is built last, so the imports are fine. What was wrong is the glue in app (F1) and the two adapters on its seams; both go.

## Interface

Signatures compile in principle; doc comments say how they fail. `ctx` first, `error` last. Only new or changed surface is listed; moved methods keep their signatures.

### `lib/httpx` and `core/module`: one route shape (decision 7)

```go
package httpx

// Route is a route a core area or module exposes, relative to its set's prefix. Public
// routes need no session; every other route needs one, and checks its permission inside
// the use case. MaxBodyBytes and Timeout raise the shell's defaults (zero keeps them).
type Route struct {
    Method       string
    Pattern      string
    Public       bool
    Handler      http.Handler
    MaxBodyBytes int64
    Timeout      time.Duration
}
```

```go
package module

// CoreRoutes converts a core area's routes to a route set: Public becomes AccessPublic,
// everything else AccessSignedIn. Fails errs.Invalid (field "routes[i].<field>") when a
// route would fail Route.Validate, or area is not a reserved core area name.
func CoreRoutes(area string, routes []httpx.Route, openapi []byte) (ModuleRoutes, error)
```

Consumer: app, once per core area. Every core `Routes()` returns `[]httpx.Route`.

### `audit`: the recorder (decision 3)

```go
package audit

// WithSystem marks ctx as running as the system. jobs sets it on every run (beside its own
// InJob marker). A signal delivery needs no marker: audit imports signals and already
// recognises a delivery's ctx. Pure.
func WithSystem(ctx context.Context) context.Context

// WithOperator marks ctx as a command the operator runs (create-superuser, export, delete,
// migrate, demo). app sets it on the command's ctx. Pure.
func WithOperator(ctx context.Context) context.Context

// Options loses Permissions (search moved to auditlog). Actor is identity.AuditActor;
// Titles is the authz.Lookup. The actor of an entry that names none is resolved in this
// order: a signal delivery (as today), WithSystem, Actor, WithOperator; none means
// anonymous (audit.md decision 6, unchanged).
type Options struct {
    DB             db.DB
    Clock          clock.Clock
    Logger         *slog.Logger
    Tracer         telemetry.Tracer
    Meter          telemetry.Meter
    Instance       string
    DataDir        string
    Actor          ActorFunc   // required
    Titles         TitleSource // required from this spec on (the authz.Lookup)
    ImportInterval time.Duration
}
```

`Log` keeps `Scoped`, `Record` (through `scoped`), `Run`, `ImportOnce`, `Close`, `SpoolCounts`. `Search`, `Export`, `CountAbout`, `Purge`, `Routes`, `OpenAPI` and `Permissions` move to `auditlog`. Consumers of the markers: `jobs` (run), `signals` (delivery), app (commands).

### `auditlog`: reading and expiring the log (decision 3)

```go
package auditlog // imports audit, authz, settings, jobs, db, telemetry, lib, model

// Settings declares audit.retention and audit.self_view (from auditsettings, verbatim).
var Settings []settings.Decl

// Permissions declares audit.search and audit.export (moved from audit).
var Permissions []authz.PermissionDecl

type Options struct {
    DB       db.DB
    Clock    clock.Clock
    Logger   *slog.Logger
    Tracer   telemetry.Tracer
    Authz    authz.Authorizer // the Lookup
    Actor    authz.ActorFunc  // identity.Actor
    Settings settings.Reader  // scoped "audit"
    Audit    audit.Recorder   // scoped "audit"; required
    Jobs     jobs.Registrar   // scoped "audit": Start registers audit.purge
}

// New validates o (errs.Invalid, one FieldError per nil dependency) and does no I/O.
func New(o Options) (*Service, error)

// Start registers audit.purge with Jobs. Fails with the Registrar's kinds (Conflict for a
// name registered twice). Never writes, so it succeeds on a read-only copy.
func (s *Service) Start(ctx context.Context) error

// Search, Export and CountAbout keep audit.md's signatures and failure modes (audit.search,
// audit.export; F15–F22). Routes returns GET /entries and GET /people/{person}/views as
// httpx.Route; OpenAPI the fragment.
func (s *Service) Search(ctx context.Context, q audit.Query) (audit.Page, error)
func (s *Service) Export(ctx context.Context, person model.PersonID, w io.Writer) (int, error)
func (s *Service) CountAbout(ctx context.Context, person model.PersonID) (int, error)
func (s *Service) Routes() []httpx.Route
func (s *Service) OpenAPI() []byte
```

`audit.Query`, `Page`, `Cursor`, `RecordID` stay in `audit` (the vocabulary). The purge job's rule (0 keeps forever; purge rows older than now minus `audit.retention`, writer only) moves from `app.auditPurge` unchanged. Consumers: app (routes, link), `datarights` (`Export`, `CountAbout`), the web audit screen (unchanged paths).

### `authz`: the Lookup (decision 2)

```go
package authz

// LookupOptions builds the read side: decisions and lookups over the roles and grants,
// nothing audited, nothing written.
type LookupOptions struct {
    DB          db.DB
    Clock       clock.Clock
    Logger      *slog.Logger
    Tracer      telemetry.Tracer
    Meter       telemetry.Meter
    Actor       ActorFunc        // identity.Actor
    Declared    []PermissionDecl // the core's and the enabled modules'
    Departments DepartmentSource // the departments.Lookup; required
}

// NewLookup validates o (errs.Invalid) and does no I/O.
func NewLookup(o LookupOptions) (*Lookup, error)

// Lookup implements Authorizer (Allow, AllowIn, ExtentOf), PermissionLister (PermissionsOf),
// audit.TitleSource (TitlesOf), HoldersOf and StarterRoleIDs, with authz.md's failure modes
// unchanged (F1–F16, F67–F71).
type Lookup struct{ /* unexported */ }

// Options (the admin Service) loses Departments and gains Lookup; the Service forwards the
// decision methods to it so existing callers compile, and owns roles, grants, Seed,
// ExportPerson, DeletePerson, Routes.
type Options struct {
    // Logger, Tracer, Audit, Signals, Lookup (DB, Clock, Actor, Meter and Declared come from the Lookup)
    Lookup *Lookup // required
}
```

Consumers of the Lookup: audit (`Titles`), api (`PermissionChecker`), identity (`Permissions`, `Lister`, `Holders`), settings, auditlog, jobs, files, regional, notify, people, datarights, departments, events, every module through `Deps.Authz`.

### `departments`, `people`, `identity`: Lookups (decision 2)

```go
package departments

// NewLookup builds the Directory (NamesOf, Active) over core.department alone; no
// permission, no audit (internal level). Fails errs.Invalid for a nil dependency.
func NewLookup(o LookupOptions) (*Lookup, error) // LookupOptions{DB, Tracer}
// Options gains Lookup *Lookup; the Service forwards NamesOf and Active to it.
```

```go
package people

// NewLookup builds the system-level reads of core.person: NamesOf, PlayaNameOf (internal),
// LanguageOf (internal), ConsentOf (internal), ContactEmailOf (personal: used only to
// deliver mail, never shown to a viewer; notify.md "Addresses"). No permission, no audit,
// as today on the Service. NotFound for an unknown or deleted person. Fails errs.Invalid
// for a nil dependency.
func NewLookup(o LookupOptions) (*Lookup, error) // LookupOptions{DB, Tracer}
// Options gains Lookup *Lookup and loses Access, AuditCount, Notify, Holders, Policy
// (replaced by Settings settings.Reader scoped "people"); the Service forwards the five
// reads and keeps LegalNamesOf (audited, people.contacts.read) and the Directory contract.
```

```go
package identity

// NewLookup builds EmailOf over core.account alone: NotFound when the person has no
// account; no permission, no audit (the address is used to deliver mail and by people's
// link flow, both system-level or already authorized by the caller). Fails errs.Invalid.
func NewLookup(o LookupOptions) (*Lookup, error) // LookupOptions{DB, Tracer}

// Actor is authz.ActorFunc over the session in ctx: the person of a full session and
// whether it is the superuser's; false without a session and for a restricted one. Pure.
func Actor(ctx context.Context) (authz.Actor, bool)

// AuditActor is audit.ActorFunc over the session in ctx: a restricted session is still the
// person for audit. Pure.
func AuditActor(ctx context.Context) (audit.Actor, bool)

// Options changes: Permissions and Lister are the authz.Lookup (unchanged types); Names is
// the people.Lookup; Policy PolicySource is replaced by Settings settings.Reader (scoped
// "accounts"; the eleven-key mapping from app.settingsPolicy moves here, read per use
// case, never cached); Alerter is replaced by Holders and Alerts; Guard stays.
type Options struct {
    // ... as today
    Settings settings.Reader    // required; identity.DefaultPolicy when a key is unset is settings' default
    Holders  HolderSource       // the authz.Lookup; required
    Notify   notify.Notifier    // scoped "identity"; required from this spec on
    Alerts   notify.AlertSender // scoped "identity"; required
    Guard    Guard              // optional (demo.Guard)
}

type HolderSource interface {
    HoldersOf(ctx context.Context, p authz.Permission) ([]model.PersonID, error)
}
```

The superuser alert moves to `identity/alert.go` as the unexported `alert(ctx, person)` with app.md's rule verbatim (holders of `accounts.manage` by `Notify.Send`, the alert addresses by `Alerts.SendAlert`, detached `alertTimeout` of 5s, empty `Key`, `Unsupported` when nobody to tell (only `notify.ErrNoAlertAddress` counts as nobody; any other `Unsupported`, such as `ErrNotConfigured`, is a failure), failures joined under the first kind, identity logs and counts and never fails the sign-in: identity F27). Its test seams are the three Options fields; hand-written fakes in identity's tests (`holdersFake`, `alertSenderFake`) serve.

### `notify`: own settings and the delivery seams (decisions 1, 6, 9)

```go
package notify

// Accounts is the identity.Lookup: the login address; NotFound when the person has none.
type Accounts interface {
    EmailOf(ctx context.Context, person model.PersonID) (model.Email, error)
}

// Profiles is the people.Lookup: the playa name, the saved language and the contact
// address; NotFound when the person has no profile.
type Profiles interface {
    PlayaNameOf(ctx context.Context, person model.PersonID) (string, error)
    LanguageOf(ctx context.Context, person model.PersonID) (string, error)
    ContactEmailOf(ctx context.Context, person model.PersonID) (model.Email, error)
}

// RegionalSource is regional's Profile: name, public URL, default language.
type RegionalSource interface {
    Profile(ctx context.Context) (regional.Profile, error)
}

// TermSource is settings' TermsFor.
type TermSource interface {
    TermsFor(ctx context.Context, lang string) (map[string]settings.Forms, error)
}

// AlertSender sends one message to every notify.alert_addresses entry (core only: the
// superuser alert). Fails errs.Unsupported wrapping ErrNoAlertAddress when no address is
// configured (identity tells this from SMTP being unset, Unsupported wrapping
// ErrNotConfigured, which is a failure), errs.Invalid for a template of another owner, else
// as Send. It reads only notify.alert_addresses; each enqueue checks Configured as Send does.
type AlertSender interface {
    SendAlert(ctx context.Context, m Message) error
}

// Options: SMTPSource, RetentionSource, SiteSource, AddressSource, LanguageSource,
// NameSource and ChannelFactory are replaced. Settings is the "notify" reader (the key
// mapping of app.smtpSource and retentionSource moves here). Channel is optional: nil
// builds the SMTP channel from the settings per batch, as today; demo passes its Capture.
type Options struct {
    // ... as today
    Settings settings.Reader
    Regional RegionalSource
    Terms    TermSource
    Accounts Accounts
    Profiles Profiles
    Channel  Channel
}

// Alerts returns the AlertSender of owner (scoped like Scoped; fails Invalid for a bad
// owner, Internal once Run has started).
func (s *Service) Alerts(owner string) (AlertSender, error)

// Probe is bms check's e-mail line over a real database: whether SMTP is configured and
// how, the outbox counts, and whether the server accepts a conversation. It reads through
// its own store on d and never returns an address, a user name or a password. Fails with
// the store's and the channel's kinds; a schema behind is errs.Unavailable.
func Probe(ctx context.Context, d db.DB, r settings.Reader) (EmailState, error)
```

The delivery rules that move in from app: an address is the login address, else the contact address, else the message is undeliverable (`NotFound`); `NotFound` from `Profiles` means no name and no language; the language falls back to its primary subtag, then the regional's default; a term is rendered under `key`, `key_plural`, `key_title`, `key_plural_title`.

### `regional`: the regional's profile (decision 4)

```go
package regional // imports settings, files, audit, authz, db, telemetry, lib, model; never notify, identity, people

// Settings declares the regional.* keys (settings.regionalDecls today, verbatim): name,
// contact_email, public_url, language, timezone, color_primary, color_primary_dark.
var Settings []settings.Decl

// Profile is settings.Regional today.
type Profile struct {
    Name, ContactEmail, PublicURL, Language, Timezone, ColorPrimary, ColorPrimaryDark string
}

// LogoRef and MaxLogoBytes move from settings unchanged; the files owner of the logo stays
// "settings" (decision 12: the bucket and core.file.owner of deployed logos keep working).
var LogoRef audit.Ref
const MaxLogoBytes int64

type Options struct {
    Clock    clock.Clock
    Logger   *slog.Logger
    Tracer   telemetry.Tracer
    Audit    audit.Recorder  // scoped "regional"
    Authz    authz.Authorizer
    Actor    authz.ActorFunc
    DB       db.DB           // ReadOnly() alone: a logo write refuses a read-only copy first (A2)
    Settings settings.Reader // scoped "regional"
    Terms    TermSource      // settings (TermsFor); required (A2)
    Revision RevisionSource  // settings: Revision and Revise (the revision lock); *settings.Service (A2)
    Files    files.Store     // scoped "settings" (the logo); required
}

// TermSource is satisfied by *settings.Service.
type TermSource interface {
    TermsFor(ctx context.Context, lang string) (map[string]settings.Forms, error)
}

// RevisionSource is satisfied by *settings.Service.
type RevisionSource interface {
    Revision(ctx context.Context) (settings.Revision, error)
    Revise(ctx context.Context, fn func(ctx context.Context) (changed bool, err error)) (settings.Revision, error)
}

func New(o Options) (*Service, error)

// Profile reads the seven keys (settings' kinds). Public, ThemeCSS, Document (the public
// document and the stylesheet), SetLogo, RemoveLogo, OpenLogo keep settings.md's rules and
// failure modes; the audit actions become regional.logo.changed|removed (A2 point 4); the logo writes run inside settings.Revise (A2 point 1). Routes returns
// GET /public, GET /theme.css, GET /logo (public), PUT /logo, DELETE /logo under
// /api/regional. The permission of PUT and DELETE /logo stays settings.manage.
func (s *Service) Profile(ctx context.Context) (Profile, error)
func (s *Service) Routes() []httpx.Route
```

`settings.New` stops adding the `regional` area itself; app passes `regional.Settings` as a core area like `accounts`. `settings.Service` loses `Regional`, `Public`, `ThemeCSS`, `Document`'s public half and the logo; keeps `Document` (the settings screen), `Set`, `Scoped`, `TermsFor`, `Terms`, `SetTerms`, `Revision` and `Revise` (the revision lock as a seam for `regional`, A2 point 1). `Decl` gains `Normalize func(value string) string` (A2 point 5).

### `datarights`: export and deletion (decision 5)

```go
package datarights // imports people, identity, authz, auditlog, notify, files, audit, signals, settings?, db, telemetry, lib, model; never module

// The types module-contract aliases (module.PersonData = datarights.PersonData, ...).
type Export struct{ Data json.RawMessage }
type Kept struct{ Kind string; Count int }
type Deletion struct{ Deleted, Anonymized int; Kept []Kept }
type PersonData interface {
    ExportPerson(ctx context.Context, person model.PersonID) (Export, error)
    DeletePerson(ctx context.Context, person model.PersonID) (Deletion, error)
}
type Source struct{ Name string; PersonData }

// The core sources, in the shapes the services already have (no adapter anywhere).
type RawSource interface { // identity, authz, notify
    ExportPerson(ctx context.Context, person model.PersonID) (json.RawMessage, error)
    DeletePerson(ctx context.Context, person model.PersonID) (int, error)
}
type FilesSource interface {
    ExportPerson(ctx context.Context, person model.PersonID) (json.RawMessage, error)
    DeletePerson(ctx context.Context, person model.PersonID) (files.Deletion, error)
}
type PeopleSource interface { // people.Service: its own rows, last
    ExportPerson(ctx context.Context, person model.PersonID) (json.RawMessage, error)
    // DeletePerson deletes the person row, contacts and consents inside the caller's Write
    // transaction and publishes people.person-deleted after commit; Conflict when a module
    // still references the person (FK RESTRICT: a programming error in a module).
    DeletePerson(ctx context.Context, person model.PersonID) (int, error)
    // Exists reports whether the person row exists (NotFound otherwise).
    Exists(ctx context.Context, person model.PersonID) error
}

var Permissions []authz.PermissionDecl // people.data.export, people.data.delete (names unchanged, decision 11)
var PublishedSignals = []signals.Name{SignalDeletionRequested} // "datarights.deletion-requested" {request, person}

type Options struct {
    DB       db.DB
    Clock    clock.Clock
    Logger   *slog.Logger
    Tracer   telemetry.Tracer
    Meter    telemetry.Meter
    Audit    audit.Recorder   // scoped "datarights"
    Signals  signals.Bus      // scoped "datarights"
    Authz    authz.Authorizer // the Lookup
    Actor    authz.ActorFunc
    Session  identity.SessionFunc
    Holders  HolderSource     // the Lookup: who is told of a new request (people.data.delete)
    Notify   notify.Notifier  // scoped "datarights": the admin notice (people.Templates' request template moves here)
    AuditLog AuditLog         // auditlog: Export (the person's audit rows) and CountAbout
    Accounts RawSource        // identity
    Access   RawSource        // authz.Service
    Outbox   RawSource        // notify
    Files    FilesSource
    People   PeopleSource
}

func New(o Options) (*Service, error)

// Link gives the service the modules' sources, in build order. Fails Conflict when called
// twice, Invalid for a duplicate name. The deletion order is: modules in build order,
// files, notify, access, identity, people (foreign keys point at core.person with
// RESTRICT; people.md decision 11).
func (s *Service) Link(modules []Source) error

// Export, ExportCommand, Delete, DeleteCommand, RequestDeletion, MyRequests,
// WithdrawRequest, ListRequests, DeclineRequest, FulfilRequest keep people.md's
// signatures, permissions, audit reasons and failure modes (people F40–F58), recorded under
// the datarights area (datarights.export, datarights.delete, datarights.request.*).
func (s *Service) Routes() []httpx.Route
```

Routes under `/api/datarights/`: `GET /me/export`, `GET /me/requests`, `POST /me/requests`, `POST /me/requests/{request}/withdraw`, `GET /requests`, `POST /requests/{request}/decline`, `POST /requests/{request}/fulfil`, `GET /people/{person}/export`, `DELETE /people/{person}`. Commands `export` and `delete` move with their exit statuses.

### `files` (decision 9)

```go
package files

// Settings declares files.max_bytes, files.quota_bytes, files.max_per_record (from
// filesettings, verbatim).
var Settings []settings.Decl

type Options struct {
    // ... as today, minus Policy
    Settings settings.Reader // scoped "files"; required; the mapping of app.filesPolicy moves here
    // Ceiling bounds the settings-derived policy: each non-zero field is a maximum the
    // policy never exceeds, and a zero QuotaBytes setting becomes Ceiling.QuotaBytes. Zero
    // means no ceiling. demo.FileCeiling in demo mode.
    Ceiling Policy
    // Until step 10 (A2 point 7): Policy PolicySource stays as an optional override and
    // PolicyFrom stays exported; Ceiling replaces Policy and PolicyFrom is unexported then.
    Jobs    jobs.Registrar // scoped "files": Start registers files.reap and files.sweep-orphans
}

// Purge deletes every object of every bucket this service has scoped and every file in
// TempDir, and counts them. Writer only (Unavailable wrapping db.ErrReadOnly on a read-only
// copy), store errors Unavailable. demo's Reset calls it.
func (s *Service) Purge(ctx context.Context) (int, error)
```

### `demo` (decision 9)

```go
package demo

// FileCeiling is files.Policy{MaxBytes: MaxUploadBytes, QuotaBytes: MaxQuotaBytes, MaxPerRecord: MaxPerRecord}.
var FileCeiling files.Policy

// NewGuard builds identity's Guard over core.demo_account alone. Protected fails only with
// the store's kinds; it never answers "not built". Fails errs.Invalid for a nil DB.
func NewGuard(o GuardOptions) (*Guard, error) // GuardOptions{DB, Tracer}

// NewCapture builds notify's Channel that stores mail in core.demo_mail instead of sending
// it (demo.md "Mail capture"). Fails errs.Invalid for a nil dependency.
func NewCapture(o CaptureOptions) (*Capture, error) // CaptureOptions{DB, Clock, Logger, Tracer}

// StateOf reads the marker: bms check's demo line. NotFound when not seeded.
func StateOf(ctx context.Context, d db.DB) (State, error)

// Options: Access, Profiles, Objects take the services' own shapes (authz.Service,
// people.Service, files.Service satisfy them directly); Files PolicySource is removed.
```

### `jobs` (decision 2)

```go
package jobs

// NewDeferrer builds signals.Deferrer over core.job alone: Defer inserts one
// signals.redeliver row in the runner's row format. Fails Invalid for a nil dependency;
// Defer fails Unavailable wrapping db.ErrReadOnly on a read-only copy.
func NewDeferrer(o DeferrerOptions) (*Deferrer, error) // DeferrerOptions{DB, Clock, Instance}
```

The runner keeps `Redeliverer` and registers the `signals.redeliver` handler as today.

### `settings`

`Options` loses `Files`; `New` no longer adds the `regional` area (app passes `regional.Settings`). `Routes` returns `GET /`, `PUT /values`, `GET /terms`, `PUT /terms/{lang}`. Everything else is unchanged.

### `app` after the refactor

`assembly` keeps `cfg`, `tel`, `dbc`, `pool`, `migrator`, `disp`, `plan`, `set`, `ui`, the `core*` declaration slices, `routeSets`, `authenticator`, `deps`, `runnables`, `commands`, the built services it must call from commands, `parts`, `started`, `srv`. It loses `permissions`, `titles`, `lister`, `names`, `notifiers`, `filesPolicy`, `operator`. `core_adapters.go` and `alert.go` are deleted. No type in `app` holds a `*assembly` field except `assembly`'s own methods.

## Types

Uses `model.PersonID`, `model.AccountID`, `model.Email`, `model.DepartmentID`; the owning packages' types as listed. New: `httpx.Route`, `regional.Profile`, `datarights.{Export, Kept, Deletion, PersonData, Source}`, `notify.EmailState`, `demo.State`.

## Data

No table, column, index or migration changes. Ownership moves:

| Table | Owner today | Owner after | Notes |
|---|---|---|---|
| `core.audit_entry` (appends) | audit | audit | — |
| `core.audit_entry` (reads, deletes by retention) | audit | auditlog | two sub-abstractions of audit.md on one table; the delete is the retention job's only write. |
| `core.data_request` | people | datarights | migration files stay in people's set (forward-only); a future change to the table goes in a `datarights` set added to `coreSetOrder` after `people` when first needed. Personal data: `note` is `personal`, retention as people.md says. |
| `core.setting` rows of area `regional` | settings | regional declares, settings stores | as every other area. |
| `core.file` rows with owner `settings` (the logo) | settings | regional | owner string unchanged (decision 12). |
| `core.demo_account` (reads) | demo.Service | demo.Guard | same package. |
| `core.demo_mail` (inserts) | demo.Service | demo.Capture | same package. |
| `core.job` (one insert kind) | jobs.Runner | jobs.Deferrer | same package. |

Personal-data columns keep their levels and retention from their specs; the Lookups expose the same fields the Services exposed to the same callers (identity → playa name; notify → address, language, name; modules → names and consent).

## Permissions

None new, none renamed. Declared-by moves: `audit.search`, `audit.export` → `auditlog`; `people.data.export`, `people.data.delete` → `datarights` (decision 12: the names stay; a rename with a data migration may come later); `settings.manage` stays settings' and guards regional's logo and the `regional` area as today.

## Signals

- Published: `datarights.deletion-requested` `{request, person}` replaces `people.deletion-requested` (signals' owner rule: the publisher segment must equal the scope's owner; no subscriber exists in the core or `volunteers`). `people.person-created` and `people.person-deleted` stay people's: `people.Service.DeletePerson` publishes the latter inside datarights' deletion, after commit, as today.
- Subscribed: unchanged (`people` subscribes `identity.account-created`).
- A handler's audit entries are the system's, as today: audit recognises a delivery's ctx itself, and jobs sets `audit.WithSystem` on every run.

## Settings

None new. Declared-by moves: `audit.*` → `auditlog`; `files.*` → `files`; `regional.*` → `regional`. Read-by: each owner through its own `settings.Reader`. No key, default, range or title changes.

## UI

No screen changes. Paths the web calls change in two steps: `web/src/shell` (`publicDoc.ts`, `regional.ts`, `App.tsx`, `Nav.tsx`), `web/src/core/settings` (`LogoControls.tsx`, `queries.ts`) and `web/src/api/public.ts` move to `/api/regional/...` (step 7); `web/src/core/people` (`YourData.tsx`, `screens/Requests.tsx`) move to `/api/datarights/...` (step 12). The generated client (`make openapi-check`) carries the new operations. The audit screen's filters by action prefix gain the new prefixes beside the old (old entries keep their names).

## Behavior

### The tenets (Proposed changes 1–2)

**App wires, it never decides.** `internal/app` calls constructors with values from `config`, from other parts' outputs and from the plan; it converts shapes (`module.CoreRoutes`), collects declarations, orders the lifecycle and maps errors to exit statuses. It parses no setting (every package takes a `settings.Reader` for its own area), implements no interface another package declares except by forwarding to a built service, and holds no rule a burner or operator could name. A test of app proves wiring (a service receives what it was given), never a rule.

**No hidden two-way dependencies.** The unit is the constructed component. If component A takes B in its `Options` and B takes A, split one of them into a part built below and a part built above (a read-side `Lookup`, a feature package like `auditlog`), or move the behavior to the side that already has what it needs. A package may hold both halves when its imports allow; it must split into two packages when the upper half needs imports the lower half's importers already have (audit/auditlog). Every late-bound adapter that survives (a value that reads a field set after it was constructed) is listed in the seam table in `07-abstractions.md` with the component it waits for and why no split is possible; `TestSeamTable_MatchesApp` fails on one that is not.

### The build order

`serve`, `check`, `openapi` and the operator commands build in this order; `TestParts_OrderAndNames_Pinned` pins it. Each item names what it takes; nothing takes anything built later.

| # | Component | Takes |
|---|---|---|
| 0 | telemetry, config, db, migrator | as today |
| 1 | `jobs.NewDeferrer`; `signals.New(Deferrer)` | db |
| 2 | `departments.NewLookup` | db |
| 3 | `authz.NewLookup` | db, declared permissions, departments.Lookup, `identity.Actor` |
| 4 | `audit.New` | db, `identity.AuditActor`, authz.Lookup (titles) |
| 5 | `authz.New` | audit, signals, authz.Lookup |
| 6 | `settings.New` | audit, authz.Lookup, the areas (declarations only) |
| 7 | `jobs.New` | audit, authz.Lookup, the dispatcher |
| 8 | `auditlog.New` | audit (none at build: its own store), authz.Lookup, settings "audit", jobs "audit" |
| 9 | `departments.New`, `events.New` | audit, signals, authz.Lookup, departments.Lookup |
| 10 | `files.New` | audit, authz.Lookup, settings "files", objects, jobs "files", `demo.FileCeiling` when demo mode |
| 11 | `regional.New` | audit, authz.Lookup, db (ReadOnly), settings "regional", settings (revision, `Revise`, terms), files "settings" |
| 12 | `identity.NewLookup`, `people.NewLookup` | db (built with the other Lookups, departments' and authz's, by app's `buildLookups` before any part; they take only the db and a tracer) |
| 13 | `demo.NewGuard`, `demo.NewCapture` (demo mode only) | db |
| 14 | `notify.New` | audit, authz.Lookup, settings "notify", regional, settings (terms), identity.Lookup, people.Lookup, Capture or nil |
| 15 | `identity.New` | audit, signals, authz.Lookup, people.Lookup, settings "accounts", notify "identity", alerts "identity", Guard or nil |
| 16 | `people.New` | audit, signals, authz.Lookup, people.Lookup, identity (`EmailOf`, `LinkAccount`), settings "people" |
| 17 | `datarights.New` | audit, signals, authz.Lookup, identity, authz.Service, notify "datarights", auditlog, files, people |
| 18 | modules (`Build`), then `Link`: datarights (module sources), people (person page, membership), notify (templates), demo (seed contributors) | |
| 19 | `demo.New` | every service (built last; routes only in demo mode) |
| 20 | api | authenticator (identity), permission checker (authz.Lookup), route sets, schema-status reader (from step 13; a closure in `runServe` until then) |

Runnables start after `Set.Start` in the order audit importer, settings refresher, notify sender, files probe, jobs runner (unchanged), and stop in reverse.

`openapi` and `check` build every component against `db.None()`; every `New` above does no I/O (app.md decision 7, unchanged). `Start` methods that register jobs never write.

### Concurrency and lifecycle

Unchanged for every moved method. Each Lookup is safe for concurrent use once built and has no `Close`. `auditlog.Service`, `regional.Service`, `datarights.Service` have no background work; `auditlog.Start` and `files.Start` register jobs. `demo.Capture` and `demo.Guard` have no lifecycle.

### The seam table and its check (decision 10)

`07-abstractions.md` gains a section **"Late-bound seams"** with a table `| Type | Waits for | Reason |` whose rows name a type, a top-level function or an `assembly` method (`assembly.<m>`) in `internal/app` (a function or method for the closure and method-value shapes of rule 5). "None." is the empty table.

`internal/app/seams_test.go`, `TestSeamTable_MatchesApp` (no build tag, no database):

1. Parses every non-test `.go` file in `internal/app` with `go/parser`.
2. Collects the late-bound candidates: a named struct type with a field of type `*assembly` (whatever the field's name), and any type whose name starts with `lazy`. The `assembly` type itself and `*process` holders are excluded by name.
3. Reads `../../plan/07-abstractions.md`, finds the "Late-bound seams" section and parses the backticked first column of each row. A missing section is a failure of its own, distinct from a section that reads "None.".
4. Fails naming each candidate not in the table ("late-bound adapter %s is not in the seam table") and each row whose type no longer exists ("seam table row %s names no type or function in app": a stale row means something is no longer late-bound and the table must say so).
5. **Closures and method values** (amended 2026-10-31, A1). A seam is a value a package keeps and calls later; a value app itself calls is wiring. The test tells them apart by **where the value is stored**, which `go/ast` can see, and never by the value's signature, because a seam's signature is whatever the package asked for (`jobs.Decl.Handler` returns `error`; `notify.ChannelFactory` returns an interface) and says nothing about who holds it. A *capture* is a func literal whose body uses an identifier of type `*assembly` (the enclosing function's receiver or parameter; any use counts, a field read, a method call or passing it on) or a method value `a.<m>` where `m` is a method declared on `assembly` and the selector is not called. Two shapes make a capture a candidate:
   - **5a, returned:** the enclosing function or method has an `*assembly` receiver or parameter, a result list containing a named non-builtin type or a func type (named or not), and contains such a capture: a capturing literal, **or a method value `a.<m>` bound to that receiver or parameter** (`func (a *assembly) send() func() { return a.run }` is late-bound exactly like the literal `func() { a.run() }`, and the test finds it the same way: an uncalled selector whose operand is the parameter and whose name is a method declared on `assembly`, collected in a first pass over the receivers). The candidate is the function, keyed `name` or `assembly.<m>`. This is `demoChannelFactory`'s shape.
   - **5b, stored in a package's struct:** the literal or method value is the value of a field (keyed or positional) of the nearest enclosing composite literal whose type is a qualified name `pkg.T` where `pkg` is imported in that file from a path under the module path (`github.com/zedgama3/bms/`). Examples: `notify.Options{Channel: func(notify.SMTPConfig) (notify.Channel, error) { return a.demo.Channel(), nil }}` inside `buildNotify`, `audit.Options{Actor: a.auditActor}`, `jobs.Decl{Handler: func(ctx context.Context, _ jobs.Job) error { return a.files.Reap(ctx) }}`. A method value is the candidate `assembly.<m>` and may be listed. A **literal always fails**, listed or not: it has no declaration, so a row cannot name it and the stale-row check of rule 4 could never see it go; the message reads "func literal in %s captures the assembly and is stored in %s: extract it to a named function or type and list it, or remove it". Extracting it yields shape 5a or a type of rule 2, both of which the table can name.
6. **Wiring, never a candidate:** a capture stored in a struct type declared in `app` (`closer`, `runnable`, `command`, `part`, `alertSeams`), because app calls it; a capture assigned to a local or run as a goroutine; and a capture passed as a **call argument** (`a.plan.Build(bctx, schema, a.provision)`, `slices.IndexFunc(a.runnables, ...)`, `a.apiOptions(..., func(ctx context.Context) ([]db.Status, error) {...})`). The argument case is left to review, not the test, because without `go/types` the test cannot see whether the callee keeps the value past the call, and the project's rule makes storage the shape that matters: **a core package takes a seam as an `Options` or `Decl` field, never as a method argument it retains** (the line for `go-conventions`, Proposed change 9). A package that wants a retained func argument is an architecture finding for the milestone review. Indirection is not followed either: a literal reaching a package struct through a local (`f := func() {...a...}; pkg.Options{F: f}`) or through an app function is the reviewer's (Proposed change 8, check 3).

Messages, one per kind, so the fix is named (the committed test, 2026-10-31, keeps a kind per candidate, `type` or `closure`): "late-bound adapter %s is not in the seam table" (rule 2); "late-bound closure %s is not in the seam table: name the type and list it, or remove it" (5a and the method value of 5b, keyed `name` or `assembly.<m>`); the literal message of 5b; "seam table row %s names no type or function in app" and "seam table row %s is no longer late-bound; remove it" (rule 4); and for the document itself, no "Late-bound seams" section, "None." beside rows, and neither rows nor "None." are each a fatal error (rule 3); a "None." table with any candidate fails naming the count. The fixture under `testdata/seams` (parsed, never compiled; it may import a project path for 5b) gains one case per shape: `TestSeamTable_FlagsStoredLiteral` (5b literal, with the extraction message), `TestSeamTable_FlagsStoredMethodValue` (5b method value, keyed `assembly.<m>`), `TestSeamTable_IgnoresWiring` (a capture in an app-declared struct, as a call argument, and in a standard-library composite literal, none of them candidates).

`make imports` runs `go test -count=1 -run 'TestSeamTable_MatchesApp' ./internal/app/` beside the two scripts; `make test` runs it too. At step 1 the table holds 18 rows (the 14 `lazy` types, `demoAccess`, `demoSchema`, `demoObjects` and the function `demoChannelFactory`); when the amended rules land (the step-1 follow-up) it holds 19: `assembly.auditActor` (5b, the method value in `audit.Options`; waits for `a.operator`, which a command sets; audit asks app who the actor is and app holds the order system, session, operator, F1) is added and removed in step 3 with `auditActor`. Checked over every func literal and method value in app's non-test files on 2026-10-31: no literal that captures the assembly is stored in a package struct, so 5b's literal shape demands no extraction today; no 5a-shaped function (`scopes`, `provision`, `apiOptions`, `auditActor`, `demoGuard`, `demoChannelFactory`) holds a method value, so 5a's method-value case adds no row; `a.provision` (an argument to `Plan.Build`, called once per module and finished when `Build` returns) and `a.stopRunnables` (in app's `closer`) are wiring under rule 6. `demoAccess`, `demoObjects` and `demoChannelFactory` are removed in step 10, `demoSchema` also in step 10 (it holds its parts directly); at step 13 it reads "None." and the test asserts no candidate exists.

### depguard `app-no-stores` (Proposed change 6)

A rule for `**/internal/app/**` denying `github.com/zedgama3/bms/internal/core/*/postgres` (except `core/db`), `.../core/notify/smtp`, `.../core/notify/channel`, `.../core/objects/*` is allowed (app builds the object store; decision: the store is configuration, not a package's internals). Added at step 10, when the last store import leaves `check.go`.

### `make arch-report` and the architecture review (decision 10)

`scripts/arch-report.sh` prints, for a reviewer: (a) the import edges among `internal/core/*`, `internal/contracts/*`, `internal/api`, `internal/app` (from `go list -f '{{.ImportPath}} {{join .Imports " "}}'`), (b) production line counts per package and the ten largest production files, (c) the `Options` struct of every core `New` with its field count (`grep -A40 'type Options struct'`), (d) the result of the seam test. No judgment in the script.

**At every milestone close** (the last roadmap item of a milestone set Implemented), before the branch is handed to the owner, the main session runs `make arch-report` and sends it with a whole-repo prompt to `go-reviewer` (read-only, opus) asking four questions: Which package has more than one purpose (its `doc.go` or spec needs "and")? Which `Options` struct takes something its package should read itself? Which component depends on one built after it? Which code in `app` decides? Findings go to the owner; a boundary move becomes an `architect` spec like this one before any code. `build-loop` gains this as step 5; `08-agent-workflow.md` gains one line in the loop diagram (Proposed changes 4, 5).

### Staged migration

Every step is one build-loop run (`go-implementer` + `test-author`, `check-runner`, `go-reviewer`; `frontend-implementer` and `web-reviewer` in steps 7 and 12) and ends with `make check` green and a commit on the feature branch. "Unchanged" tests are the existing ones that must still pass as written; "moved" tests keep their names and assertions and change package; "new" tests are written from this spec.

| Step | Change | Removes | Unchanged proof | Moved / new tests |
|---|---|---|---|---|
| 1 | Seam table with today's 18 rows (19 after A1); `seams_test.go`; `make imports` runs it; `scripts/arch-report.sh`, `make arch-report` | nothing | `make check` | new: `TestSeamTable_MatchesApp`, `TestSeamTable_FlagsUnlistedType` (a fixture file under `testdata`, parsed the same way), `TestSeamTable_FlagsStaleRow`, `TestSeamTable_FlagsRowNoLongerLateBound`, `TestSeamRows_MissingSection`, `TestSeamRows_None`, `TestSeamRows_Malformed`; the A1 follow-up adds `TestSeamTable_FlagsStoredLiteral`, `TestSeamTable_FlagsStoredMethodValue`, `TestSeamTable_IgnoresWiring` |
| 2 | `httpx.Route`, `module.CoreRoutes`; every core `Routes()` returns `[]httpx.Route`; app calls `CoreRoutes` | nine `Route` types, nine converters | every core `*_http_test`, `TestOpenAPI_*`, `make openapi-check` (document unchanged) | new: `TestCoreRoutes_PublicBecomesAccessPublic`, `TestCoreRoutes_BadRoute_ReturnsInvalid`, `TestCoreRoutes_NotACoreArea_ReturnsInvalid` |
| 3 | `identity.Actor`, `identity.AuditActor`, `audit.WithSystem`, `audit.WithOperator`; jobs sets the system marker on every run; commands set the operator marker | `authzActor`, `auditActor`, `assembly.operator` | identity, authz, audit, jobs, signals suites; `person_commands_integration_test` | moved: `TestAuditActor_InJob_IsTheSystem` → audit (`TestResolveActor_SystemMarker_WinsOverSession`); new: `TestResolveActor_OperatorMarker_WithoutSession`, `TestActor_RestrictedSession_IsFalse`, `TestAuditActor_RestrictedSession_IsThePerson` |
| 4 | `departments.NewLookup`, `authz.NewLookup`; order 2–5 and 7 of the table; identity, settings, audit, api take the Lookup | `lazyPermissions`, `lazyTitles`, `lazyLister`, `lazyDepartments`, `lazyAuthorizer` | authz, departments, audit, identity, settings suites (the decision tests run against the Lookup through the Service's forwarding); `core_integration_test` | moved: `TestLazyAuthorizer_AfterAuthz_Delegates` deleted (no seam); new: `TestNewLookup_NilDepartments_ReturnsInvalid` (authz), `TestLookup_Active_ArchivedIsFalse` (departments), `TestNew_RecordsTitlesFromLookup` (audit, with `authztest.Fake`) |
| 5 | Each package reads its own area: `identity.Options.Settings`, `people.Options.Settings`, `notify.Options.Settings` (SMTP, retention); settings built before identity; `notify.SMTPFromSettings` exported for `bms check` until step 10's `notify.Probe` | `lazyIdentityPolicy`, `lazyPeoplePolicy`, `settingsPolicy`, `peoplePolicy`, `smtpSource`, `retentionSource` | identity, people, notify suites; `TestLazyPolicies_*` deleted (no seam) | moved: `TestSettingsPolicy_ReadsEveryKey`, `_Defaults_AreIdentityDefaultPolicy`, `_WrongArea_ReturnsInvalid` → identity; `TestPeoplePolicy_*` → people; `TestSMTPSource_*`, `TestRetentionSource_*` → notify; new: `TestPolicy_UnsetKey_IsTheDeclaredDefault` in each of identity, people, notify (uses `settingstest.NewFake`) |
| 6 | `auditlog` package; `auditlog.Settings` (from `auditsettings`); the purge job in `auditlog.Start`; `/api/audit/` served by auditlog | `auditPurge`, package `auditsettings`, `audit.Options.Permissions` | audit's `TestSearch_*`, `TestExport_*` (moved verbatim), the audit screen e2e, `catalogue_jobs_test` (job list unchanged) | moved: the search/export/retention tests → auditlog; new: `TestStart_RegistersAuditPurge`, `TestPurge_RetentionZero_DeletesNothing`, `TestPurge_ReadOnlyCopy_ReturnsUnavailable` |
| 7 | `regional` package; routes under `/api/regional/`; web paths; settings loses `files`; then `files.Options.Settings`, `files.Settings` (from `filesettings`), once settings no longer imports files | `lazyFilesStore`, `filesPolicy`, package `filesettings` | files suite; settings' `TestLogo_*`, `TestPublic_*`, `TestTheme_*` (moved verbatim), `core_settings_logo_test` → regional, the logo and theme e2e | moved as listed; new: `TestNew_NilFiles_ReturnsInvalid`, `TestProfile_ReadsSevenKeys`, `TestRoutes_ArePublicWhereSettingsWere`, `TestPolicy_UnsetKey_IsTheDeclaredDefault` (files); web: `LogoControls.test.tsx`, `publicDoc.test.ts` updated |
| 8 | `identity.NewLookup`, `people.NewLookup`; notify's new seams (`Accounts`, `Profiles`, `Regional`, `Terms`; `Channel`: step 10); notify built before identity and people | `lazyNotifier`, `lazyNames`, `addressSource`, `languageSource`, `nameSource`, `siteSource` | notify, identity, people suites; `core_notify_integration_test` (delivery to a login address, to a contact address, undeliverable) | moved: `TestAddressSource_Table`, `TestLanguageAndNameSources_NotFoundIsEmpty`, `TestSiteSource_*` → notify; new: `TestLookup_EmailOf_NoAccount_ReturnsNotFound` (identity), `TestLookup_ContactEmailOf_Deleted_ReturnsNotFound` (people) |
| 9 | The alert in identity; `notify.AlertSender`, `Service.Alerts` | `lazyAlerter`, `app/alert.go` | identity's sign-in tests (F27: a failed alert never fails the sign-in); `TestAlert_*` moved verbatim | moved: the eight `TestAlert_*` → identity; new: `TestSendAlert_NoAddress_ReturnsUnsupported`, `TestSendAlert_OtherOwnersTemplate_ReturnsInvalid` (notify) |
| 10 | `demo.NewGuard`, `demo.NewCapture`, `demo.FileCeiling`, `demo.StateOf`, `files.Purge`, `notify.Probe`; demo's seams take the services' shapes; `demoSchema` holds the migrator, the plan's ordered sets and the authz seeder directly, not `*assembly`; `check.go` calls the probes; depguard `app-no-stores` | `lazyGuard`, `lazyFilesPolicy`, `demoChannelFactory`, `demoObjects`, `demoAccess`, `demoSchema` (its `*assembly` field), `demoProfiles`, `checkReport.smtpConfig` | `demo_integration_test` (seed, reset, protected accounts, capture), `demo_caps_integration_test`, `commands_test` (`check` output pinned), `usage_check_test` | moved: `TestDemoPolicy_ServiceMissing_FailsClosed` deleted (no gap exists; `TestNew_CeilingNeverRaises` in files replaces it); new: `TestGuard_UnknownAccount_IsFalse`, `TestCapture_StoresInsteadOfSending`, `TestPurge_ClearsScopedBucketsAndTemp`, `TestPurge_ReadOnlyCopy_ReturnsUnavailable`, `TestProbe_NothingSet_IsNotConfigured`, `TestProbe_NeverReturnsAnAddress` |
| 11 | `jobs.NewDeferrer`; built before the dispatcher | `lazyDeferrer` | signals and jobs suites; `internal_jobs_integration_test` (a failed delivery is redelivered) | new: `TestNewDeferrer_NilDB_ReturnsInvalid`, `TestDefer_ReadOnlyCopy_ReturnsUnavailable`, `TestDefer_RowIsRunnableByTheRunner` |
| 12 | `datarights` package; module-contract aliases; routes under `/api/datarights/`; web paths; signal rename; people's `PeopleSource`; the two commands move | `personData`, `notifyData`, `filesData`, `notifySource`, `filesSource`; people's data-rights code | people's `TestExport_*`, `TestDelete_*`, `TestRequest*` (moved verbatim), `person_commands_integration_test`, every module's `moduletest.Conformance` (the aliases keep them compiling and passing), the export and deletion e2e | moved as listed; new: `TestLink_Twice_ReturnsConflict`, `TestDelete_OrderIsModulesFilesNotifyAccessIdentityPeople` (fakes record the order), `TestDeletePerson_ModuleStillReferences_ReturnsConflict` (people), `TestPublishes_DatarightsDeletionRequested`; web: `YourData.test.tsx`, `Requests.test.tsx` updated |
| 13 | Seam table set to "None."; `seams_test.go` asserts no candidate; app.md's body rewritten by `spec-editor` from this spec (the amendments list); `make arch-report` run and attached to the hand-back; the schema-status closure in `runServe` replaced by a named schema-status reader built from the migrator and the migration sets (D1) | the last `lazy` mention; the schema-status closure in `runServe` | `make check`; `make test-integration`; `make test-e2e` in the three modes | new: `TestSeamTable_IsEmpty` |

Steps 2, 3 and 11 are independent of the rest and may run first in any order; 4 precedes 5–9; 7 precedes 8 (notify's `Regional`) and the files part (moved from 5); 8 precedes 9 and 12; 10 may run any time after 5.

### Risks

- **A "nothing yet" default was load-bearing.** Each lazy adapter answers something before its part is built (deny, empty, default policy, `Unsupported`). A command or test that builds a subset of parts relied on that. Mitigation: every `New` validates its dependencies as required, so a missing one fails at build with `Invalid` naming the field rather than silently changing an answer; `check` and `openapi` build everything.
- **`check` and `openapi` must still build against `db.None()`.** The new `New`s do no I/O (every step's reviewer checks); `TestCheck_*` and `TestOpenAPI_*` in `commands_test` prove it per step.
- **Path changes (steps 7 and 12) break a client that is not updated in the same step.** The web, the generated client and the e2e tests change in the same commit; `make openapi-check` fails otherwise. There is one deployed instance (the public demo) and it ships the matching web build.
- **Audit action renames split a history.** Entries before step 6, 7 and 12 keep `people.data.*`, `settings.logo.*`; the audit screen filters both prefixes. The history files of the specs record the rename date.
- **The signal rename.** No subscriber exists; `CoreSignals` changes; the graph check (`Resolve`) would refuse a module subscribing to the old name, loudly.
- **Two sub-abstractions on one table** (audit/auditlog on `core.audit_entry`, people/datarights on `core.data_request`'s migrations). Each spec says who inserts, reads and deletes; `postgres-sql`'s one-schema-per-module rule is about schemas, which do not change.
- **Step size.** Steps 6, 7 and 12 move several hundred lines each. If a step cannot close in two fix rounds, split it (for 12: the aliases and the `PeopleSource` first, the package move second) rather than merge it with the next.
- **Order drift after this spec.** The seam test catches a new late-bound type, a returned or stored closure and a stored method value (rules 2, 5a, 5b); it cannot catch a new rule written as plain wiring. That is what the reviewer line and the milestone review are for.
- **Three blind spots of the seam test (A1).** (1) A capture handed by call argument to a method that keeps it (`Subscribe`-style): not flagged, because the test cannot see retention without `go/types`. (2) A capture that reaches a package struct through a local or an app function (`f := func() {...a...}; pkg.Options{F: f}`; `a.apiOptions(..., func...)`): not flagged, because indirection is not followed. (3) An `*assembly` held in a local variable of a plain function (for example `a := newAssembly(...)` in `runServe`): not tracked, so a func literal there that uses `a` is not a capture at all, even when stored directly in a package struct literal; following it would need per-function variable tracking, which the test does not do. In (2) the closure goes through a local; in (3) the assembly itself does. Mitigation: the rule that a core package takes a seam only as an `Options` or `Decl` field (rule 6; `go-conventions`, Proposed change 9), so (1) is a design finding before it is a test gap; the reviewer line for new captures in `app` (Proposed change 8); and the milestone review's fourth question ("which code in `app` decides?"), which cover all three. Today one instance exists: the schema-status func literal in `runServe` (`internal/app/serve.go`) uses the local `a` and reaches `api.Options.Schema` through `a.apiOptions`; the test does not see it; it is removed in step 13. No func literal using a local assembly is stored directly in a package struct literal.

## Failure modes

Moved methods keep their specs' failure modes and tests. New ones:

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| B1 | `module.CoreRoutes` | a route fails `Route.Validate` | Invalid | fail the build (a programming error) | TestCoreRoutes_BadRoute_ReturnsInvalid |
| B2 | `module.CoreRoutes` | `area` is not a reserved core area name | Invalid | fail the build | TestCoreRoutes_NotACoreArea_ReturnsInvalid |
| B3 | `audit.New` | `Titles` nil | Invalid | fail the build | TestNew_NilTitles_ReturnsInvalid |
| B4 | `audit.Log.Record` | no system or operator marker, no session and `Actor` false | recorded as `anonymous` (audit.md decision 6, unchanged) | none: a failed login or public route is anonymous by design | TestRecord_ActorResolution ("no session is anonymous" row, exists) |
| B5 | `authz.NewLookup` | nil `Departments` or empty `Declared` | Invalid | fail the build | TestNewLookup_NilDepartments_ReturnsInvalid |
| B6 | `auditlog.New` | nil dependency | Invalid | fail the build | TestNew_NilDependency_ReturnsInvalid |
| B7 | `auditlog.Start` | `audit.purge` registered twice | Conflict | fail the start | TestStart_Twice_ReturnsConflict |
| B8 | `auditlog` purge handler | read-only copy | Unavailable wrapping `db.ErrReadOnly` | jobs retries later | TestPurge_ReadOnlyCopy_ReturnsUnavailable |
| B9 | `regional.New` | nil `Files`, `Settings`, `Terms`, `Revision`, `DB` or any dependency | Invalid | fail the build | TestNew_NilFiles_ReturnsInvalid, TestNew_NilTerms_ReturnsInvalid |
| B10 | `regional.Profile` | a key fails to read | the Reader's kind | fail the request | TestProfile_ReaderFails_KeepsTheKind |
| B11 | `notify.AlertSender.SendAlert` | no alert address configured | Unsupported wrapping `ErrNoAlertAddress` | log once, continue (identity) | TestSendAlert_NoAddress_ReturnsUnsupported |
| B12 | `notify.AlertSender.SendAlert` | template of another owner | Invalid | fail (a programming error) | TestSendAlert_OtherOwnersTemplate_ReturnsInvalid |
| B13 | `notify.Probe` | schema behind, no table | Unavailable | print "unknown", not a check failure | TestProbe_NoTable_ReturnsUnavailable |
| B14 | `identity.New` | nil `Settings`, `Holders`, `Notify` or `Alerts` | Invalid | fail the build | TestNew_NilAlerts_ReturnsInvalid |
| B15 | `identity.Lookup.EmailOf` | no account for the person | NotFound | notify: try the contact address | TestLookup_EmailOf_NoAccount_ReturnsNotFound |
| B16 | `people.Lookup.*Of` | unknown or deleted person | NotFound | notify: "none"; identity: no name | TestLookup_ContactEmailOf_Deleted_ReturnsNotFound |
| B17 | `datarights.New` | nil dependency | Invalid | fail the build | TestNew_NilPeople_ReturnsInvalid |
| B18 | `datarights.Link` | called twice | Conflict | fail the link | TestLink_Twice_ReturnsConflict |
| B19 | `datarights.Link` | duplicate source name | Invalid | fail the link | TestLink_DuplicateName_ReturnsInvalid |
| B20 | `datarights.Delete` | a module still references the person after its `DeletePerson` | Conflict (from people) | fail the deletion, roll back | TestDeletePerson_ModuleStillReferences_ReturnsConflict |
| B21 | `files.Purge` | read-only copy | Unavailable wrapping `db.ErrReadOnly` | demo reset fails its "objects" step | TestPurge_ReadOnlyCopy_ReturnsUnavailable |
| B22 | `files.Purge` | object store unreachable | Unavailable | same | TestPurge_StoreFails_ReturnsUnavailable |
| B23 | `files.New` | `Settings` nil | Invalid | fail the build | TestNew_NilSettings_ReturnsInvalid |
| B24 | `demo.NewGuard`, `NewCapture` | nil DB | Invalid | fail the build | TestNewGuard_NilDB_ReturnsInvalid |
| B25 | `demo.Guard.Protected` | store fails | Unavailable | identity refuses the change (fail closed, as today) | TestProtected_StoreFails_ReturnsUnavailable |
| B26 | `demo.StateOf` | not seeded | NotFound | `check` prints "not seeded" | TestStateOf_NoMarker_ReturnsNotFound |
| B27 | `jobs.NewDeferrer` | nil DB | Invalid | fail the build | TestNewDeferrer_NilDB_ReturnsInvalid |
| B28 | `jobs.Deferrer.Defer` | read-only copy | Unavailable wrapping `db.ErrReadOnly` | the dispatcher logs it; the delivery is final (signals F-unchanged) | TestDefer_ReadOnlyCopy_ReturnsUnavailable |
| B29 | `TestSeamTable_MatchesApp` | a late-bound type not in the table | test failure | list it with a reason, or remove it | TestSeamTable_FlagsUnlistedType |
| B30 | `TestSeamTable_MatchesApp` | a table row names no type, or a type that is no longer late-bound | test failure | delete the row | TestSeamTable_FlagsStaleRow, TestSeamTable_FlagsRowNoLongerLateBound |
| B31 | `TestSeamTable_MatchesApp` | a func literal capturing the assembly is stored in a project package's struct (5b) | test failure | extract it to a named function or type and list it, or remove it | TestSeamTable_FlagsStoredLiteral |
| B32 | `TestSeamTable_MatchesApp` | a method value `a.<m>` is stored in a project package's struct and `assembly.<m>` is not in the table | test failure | list `assembly.<m>` with a reason, or remove it | TestSeamTable_FlagsStoredMethodValue |
| B33 | `TestSeamTable_MatchesApp` | the "Late-bound seams" section is missing, "None." sits beside rows, or neither rows nor "None." are present | test failure | fix the document | TestSeamRows_MissingSection, TestSeamRows_None, TestSeamRows_Malformed |
| B34 | `settings.Revise` | fn fails | fn's kind; rollback, no bump, no snapshot change | fail the use case | TestRevise_FnFails_RollsBackAndKeepsRevision |
| B35 | `settings.Revise` | read-only copy | Unavailable wrapping `db.ErrReadOnly`, before fn runs | fail the use case | TestRevise_ReadOnlyCopy_ReturnsUnavailable |
| B36 | `settings.Revise` | fn reports no change | none: the current revision, no bump, no snapshot replace | none | TestRevise_Unchanged_ReturnsCurrentRevision |
| B37 | `settings.Revise` | `core.setting_revision` has no row | Internal (settings F26) | fail the use case | TestRevise_NoRevisionRow_ReturnsInternal |
| B38 | `settings.Revise` | two callers at once (two instances) | none: serialised by the row lock, the second runs after the first commits | none | TestRevise_Concurrent_Serialised (integration) |

`identity.Actor`, `identity.AuditActor`, `audit.WithSystem`, `audit.WithOperator`, `module.CoreRoutes`'s happy path, `demo.FileCeiling` and the Lookups' constructors beyond nil checks cannot fail: they are pure functions over a ctx or a value.

## Lock-in check

No external technology is wrapped. `objects` (disk, S3-compatible), SMTP and PostgreSQL keep their implementations; nothing here changes which ones exist.

## Test plan

- **Unit, per step** (fakes: `dbtest`, `authztest` (gains `HoldersOf` if missing), `peopletest`, `notifytest`, `settingstest`, `jobstest`, `filestest`, `telemetrytest`): the moved tests verbatim, the new tests in the table above.
- **Permission tests:** unchanged for every moved use case (the same names run in the new package).
- **Integration** (`//go:build integration`, the compose PostgreSQL): `core_integration_test`, `core_notify_integration_test`, `demo_integration_test`, `demo_caps_integration_test`, `person_commands_integration_test`, `internal_jobs_integration_test` run unchanged at every step; `TestDefer_RowIsRunnableByTheRunner`, `TestPurge_ClearsScopedBucketsAndTemp` and `TestRevise_Concurrent_Serialised` (A2) are new; `TestRevise_Changed_BumpsAndInstallsSnapshot` is the unit happy path.
- **Conformance:** `moduletest.Conformance` for `hello` and `volunteers` at every step (the aliases in step 12 must keep both green without a change in either module).
- **End to end:** `make test-e2e` in the three modes at steps 7, 12 and 13.
- **Document:** `make openapi-check` at every step; steps 7 and 12 commit the new document.
- **Guards:** `TestSeamTable_*` from step 1; `golangci-lint` with `app-no-stores` from step 10; `make arch-report` at step 13 and every milestone close after.

## Open questions

None. Q1–Q6 were answered by the owner on 2026-10-31 and are decisions 12 (Q1), 13 (Q2), 14 (Q3), 15 (Q4), 16 (Q5) and 17 (Q6).

## Proposed changes elsewhere

Applied by `spec-editor` after approval, in the step that needs each.

1. *(applied 2026-10-31)* **`plan/00-principles.md`**, Engineering tenets: add **5. App wires, it never decides** and **6. No hidden two-way dependencies**, with the two paragraphs under Behavior, "The tenets" (shortened to the principle and one sentence of consequence each).
2. **`plan/07-abstractions.md`** *(rules 11 and 12 and the "Late-bound seams" section applied 2026-10-31; partly applied 2026-11-01, step 6: the new **auditlog** entry, the **audit** entry losing search, export, retention and the settings, and `auditlog` in the core diagram; partly applied 2026-11-01, step 8: the **identity**, **people** and **notify** entries (`Lookup`s; notify's delivery rules and `Regional`/`Terms`/`Accounts`/`Profiles`); partly applied 2026-11-01, step 7: the new **regional** entry and `regional` in the core diagram, **settings** losing the regional profile, the public document and the logo and gaining "declares no area of its own", and the **files** entry's settings sentence; partly applied 2026-11-01, step 9: the **identity** entry's alert, `Holders` and `Alerts`, and the **notify** entry's `AlertSender`; the rest of the entry edits and the new **datarights** entry pending: land with their steps)*:
   - rule 11 (app wires) and rule 12 (no hidden two-way dependencies; the seam table) under "Layers and dependency rules";
   - a new section **"Late-bound seams"** with the table (18 rows at step 1, 19 after A1, "None." at step 13);
   - the **app** entry: "Doesn't do: anything any assembled package does; parse a setting; implement a package's seam except by forwarding";
   - new entries **auditlog**, **regional**, **datarights**; the core diagram lists `auditlog · regional · datarights`;
   - **audit** loses search, export, retention and the settings; **settings** loses the regional profile and the logo and gains "declares no area of its own"; **people** loses export, deletion, requests and the two commands and gains `Lookup` and "answers for its own rows as a datarights source";
   - **authz**, **departments** gain `Lookup`; **identity** gains `Lookup`, `Actor`, `AuditActor`, the alert; **notify** gains `AlertSender`, `Probe` and the delivery rules; **files** gains `Settings`, `Ceiling`, `Purge`; **demo** gains `Guard`, `Capture`, `FileCeiling`, `StateOf`; **jobs** gains `Deferrer`.
3. *(applied 2026-10-31)* **`CLAUDE.md`**, Non-negotiables 1: add "**App wires, it never decides.** `internal/app` calls constructors and forwards; it parses no setting and holds no rule. **No hidden two-way dependencies:** if A needs B at runtime and B needs A, split one; a late-bound adapter exists only in the seam table in `plan/07-abstractions.md`."
4. *(applied 2026-10-31)* **`.claude/skills/build-loop/SKILL.md`**: new **5. Milestone close: architecture review** (Behavior, "`make arch-report` and the architecture review"), and in step 2 the note that `make imports` includes the seam test.
5. *(applied 2026-10-31)* **`plan/08-agent-workflow.md`**: one line after the loop diagram: "At a milestone close the main session runs `make arch-report` and a whole-repo review (build-loop step 5) before handing the branch to the owner."
6. **`.golangci.yml`**: depguard rule `app-no-stores` (Behavior). **`Makefile`**: `imports` runs the seam test; new `arch-report` target. **`scripts/arch-report.sh`**: new.
7. *(applied 2026-10-31)* **`.claude/skills/design-spec/SKILL.md`**, checklist: "- [ ] For each dependency: what does this need that is built after it? (If anything, the design names the split; nothing is late-bound.)" and "- [ ] Does any of this belong in app? (Only constructors, forwarding and lifecycle may.)". **`.claude/agents/architect.md`**, Rules: the same two questions as bullets.
8. *(applied 2026-10-31)* **`.claude/agents/go-reviewer.md`**, check 3 Boundaries: "- new code in `internal/app` that is not a constructor call, a shape conversion, a declaration collection or lifecycle (app wires, it never decides)" and "- a new type in `internal/app` that reads the assembly after construction, or a func literal capturing it, that is not in the seam table of `07-abstractions.md`".
9. **`.claude/skills/go-conventions/SKILL.md`**, Layers *(the sentence "A core package reads its own settings area…" applied 2026-11-01, step 5; partly applied 2026-11-01, step 6: `auditlog` added to the core list, which is an open-ended example list, so `regional` and `datarights` join it with their steps; `regional` added 2026-11-01, step 7; the rest lands with its steps)*: add `internal/core/auditlog`, `regional`, `datarights` to the core list; the sentence "A core package reads its own settings area through a `settings.Reader` in its `Options`; it never receives a parsed policy from app." and "A core area's `Routes()` returns `[]httpx.Route`; app converts with `module.CoreRoutes`."
   - *(applied 2026-10-31)* "a core package takes a seam as an `Options` or `Decl` field, never as a method argument it retains" (A1). The rest of this item lands with its steps.
10. *(partly applied 2026-11-01, step 9: "The superuser alert" removed, a one-line pointer left; the rest pending: step 13)* **`plan/design/app.md`**: remove "The superuser alert", the four lazy amendments (2026-10-19, 2026-10-21, the demo adapters of 2026-10-28), `authzActor`/`auditActor`; replace "How later core services plug in" with the build order table above; the Imports section drops `core/demo/postgres`, `core/notify/postgres`, `core/notify/smtp`, `core/notify/channel` and adds `core/auditlog`, `core/regional`, `core/datarights`; narratives go to `history/app.md`.
11. *(applied 2026-11-01, step 6; the reader's text lives in `auditlog.md` and `audit.md` keeps a pointer section; the failure rows that moved are F23 to F31, F40, F41, F44, not F15 to F22, which are the recorder's)* **`plan/design/audit.md`**: the recorder keeps F1–F14 and the spool; Search, Export, CountAbout, retention, `audit.search|export`, the settings and the routes move to a new section "The log reader (`auditlog`)" with F15–F22 unchanged; `Options.Permissions` removed, `Titles` required; `WithSystem`, `WithOperator` and the actor order added.
12. (applied 2026-11-01, step 4; see E2) **`plan/design/authz.md`**: `Lookup` and `LookupOptions`; `Departments` moves to the Lookup; the Service forwards decisions; F1–F16, F67–F71 are the Lookup's.
13. *(partly applied 2026-11-01, step 5: `Options.Settings` replaces `PolicySource`; partly applied 2026-11-01, step 8: `Lookup`; `Actor`, `AuditActor` (code at step 3, documented in identity.md at step 8); `Options.Lookup` required, `Service.EmailOf` forwards; `Holders`, `Alerts` and the alert applied 2026-11-01, step 9)* **`plan/design/identity.md`**: `Lookup`, `Actor`, `AuditActor`; `Options.Settings` replaces `PolicySource`; `Holders` and `Alerts` replace `Alerter`; "The superuser alert" section moved in from app.md verbatim; identity built after notify and authz.
14. *(partly applied 2026-11-01, step 5: `Options.Settings` replaces `Policy`/`PolicySource`; partly applied 2026-11-01, step 8: `Lookup`, `Options.Lookup` and the Service's forwarding; the data-rights move and the `Options` removals (`Access`, `AuditCount`, `Notify`, `Holders`) land with step 12, where datarights' `Options` takes `Access`, `AuditLog`, `Notify` and `Holders`; `Accounts` stays)* **`plan/design/people.md`**: `Lookup`; the data-rights sections (export, deletion, requests, the notice, the two commands, their routes, F40–F58, the signal `people.deletion-requested`) move to datarights.md; people answers as a `PeopleSource`; `Options` loses `Access`, `AuditCount`, `Notify`, `Holders`, `Policy` and gains `Lookup`, `Settings`.
15. *(partly applied 2026-11-01, step 7: `auditlog.md` (step 6) and `regional.md` with its README row (row 19e) written; `datarights.md` pending)* **New `plan/design/datarights.md`**, **`regional.md`**, **`auditlog.md`**: written by `spec-editor` from the moved sections and this spec's Interface, each with its own Summary; README rows added as Approved-on-approval-of-this-spec.
16. *(partly applied 2026-11-01, step 5: `Settings` replaces `SMTPSource` and `RetentionSource`, `SMTPFromSettings` exported; partly applied 2026-11-01, step 8: `Regional`, `Terms`, `Accounts`, `Profiles` replace `Site`, `Addresses`, `Languages`, `Names`, the delivery rules moved in, the imports line; `AlertSender`/`Alerts` applied 2026-11-01, step 9; `Channel` and `Probe` land with step 10)* **`plan/design/notify.md`**: the six seams replaced by `Settings`, `Regional`, `Terms`, `Accounts`, `Profiles`, `Channel`; `AlertSender`, `Alerts`, `Probe`; the delivery rules moved in; the imports line gains `core/regional` and `core/settings`.
17. *(applied 2026-11-01, step 7; the moved text, rows F23 and F36 to F54, and the logo tests are in `regional.md`; Open questions 1 and 2 there are what the split left unsettled)* **`plan/design/settings.md`**: regional profile, public document, theme, logo and the `regional` area move to regional.md; `Options.Files` removed; routes reduced to four.
18. *(partly applied 2026-11-01, step 7: `Settings`, `Options.Settings`, `PolicySource` removed, `filesettings` removed; `Options.Ceiling`, `Options.Jobs` and `Purge` pending: step 10)* **`plan/design/files.md`**: `Settings`, `Options.Settings`, `Options.Ceiling`, `Options.Jobs`, `Purge`; `PolicySource` removed; `filesettings` removed.
19. **`plan/design/demo.md`**: `Guard`, `Capture`, `FileCeiling`, `StateOf`; `Service.Policy`, `Channel`, `Protected` removed; `Objects` is `files.Purge`; the seams take the services' shapes.
20. **`plan/design/jobs.md`**: `NewDeferrer`; the runner sets `audit.WithSystem` on every run.
21. **`plan/design/signals.md`**: the `Deferrer` is `jobs.Deferrer`, built before the dispatcher; the delivery marker audit reads is unchanged.
22. **`plan/design/departments-events.md`**: `departments.Lookup`; authz's `DepartmentSource` is the Lookup.
23. **`plan/design/module-contract.md`**: `PersonData`, `Export`, `Deletion`, `Kept` are aliases of `datarights`'; `CoreRoutes`; `Deps` unchanged.
24. *(partly applied 2026-11-01, step 7: route set `regional` reserved and the moved `/api/regional/` paths; `PermissionChecker` as `authz.Lookup` and the `datarights` set and paths pending with their steps)* **`plan/design/api.md`**: `PermissionChecker` is the `authz.Lookup`; route sets `regional` and `datarights` added to the reserved core area names; the moved paths.
25. *(applied 2026-11-01, step 7)* **`plan/design/web-shell.md`**: the public document, theme and logo come from `/api/regional/`.
26. **`plan/03-decisions.md`** (by `plan-keeper` on approval): the two tenets, the path moves, the signal rename and the audit action renames as one decision with this spec as its source.

## Amendments

- **A1, 2026-10-31** (approved by the owner 2026-10-31; folded in 2026-10-31): the seam check's closure rule is defined by where a capture is stored. The narrative, the reason and what was folded in are in `plan/design/history/boundaries.md`.
- **E1, 2026-10-31** (erratum, approved by the owner 2026-10-31): an entry with no actor, no marker and no session is anonymous, as before; the earlier text said `errs.Invalid` citing F8, which is the action-grammar failure.
- **D1, 2026-11-01** (owner): the schema-status closure in `runServe`, found by the blind-spot check, is removed in step 13 by a named reader; it is not listed in the seam table meanwhile.
- **2026-11-01, boundaries step 9:** Proposed changes 13 and 16 (identity's alert, `Holders`, `Alerts`; notify's `AlertSender`, `Service.Alerts`), 10 in part (app.md loses "The superuser alert") and 2 in part (the identity and notify entries) are marked applied for step 9; the Interface text above is unchanged.
- **D5, 2026-11-01** (step 9 review): `notify.ErrNoAlertAddress` marks SendAlert's 'no alert address' Unsupported, so identity tells it from 'SMTP not configured' (`ErrNotConfigured`) positively; failures still join under the first kind, in the code's order: holders lookup, holders send, then `SendAlert` (which reads the addresses, then sends). The only difference from app's old order (holders lookup, address read, holders send, address sends) is which kind is first when the holders send and the address read both fail: now the holders-send kind, before the address-read kind. Matching exactly would need a wider notify API.
- **E2, 2026-11-01** (erratum): the Lookup's failure modes are authz.md's decision rows (F1–F16, F67–F71); the earlier text cited F20 and F21, which are `CreateRole` rows.
- **D2, 2026-11-01** (owner, from step 4's review): `authz.Options` keeps only Logger, Tracer, Audit, Signals and Lookup; the Service uses the Lookup's DB, clock, actor and catalogue, so the two cannot diverge.
- **D3, 2026-11-01** (staging, forced by an import cycle): the files part of step 5 (`files.Settings`, `files.Options.Settings`, removing `filesPolicy` and `filesettings`) moves to step 7, because files cannot import settings until regional takes the logo out of settings. Step 5 also exports `notify.SMTPFromSettings` for `bms check` until step 10.
- **E3, 2026-11-01** (erratum): item 11's F15–F22 are `Record` rows; the reader's rows are F23–F31, F40, F41, F44. `auditlog.Options` also has `Audit audit.Recorder` (scoped "audit", required), which build-order row 8 implies and the Options list omitted.
- **D4, 2026-11-01** (step 6 review): `auditlog`'s purge is unexported (job-only) and `auditlog.Options` drops the unused `Meter`; the build-order table lists settings before jobs, as built.
- **S8, 2026-11-01** (step 8 applied by `spec-editor`, docs only; the implementation lands in parallel): `identity.md`, `people.md` and `notify.md` gain their step-8 text (Proposed changes 13, 14, 16, partly), and `07-abstractions.md` its identity, people and notify entries (item 2, partly; the Late-bound seams table is untouched). The `Channel` seam appears in the step-8 table row but is step 10's (`demoChannelFactory` goes then), so notify keeps `Channel ChannelFactory` until step 10. `people.LookupOptions` drops the unused `Meter` (as D2/D4). `people.Options` keeps `Access`, `AuditCount`, `Notify` and `Holders` until step 12 (datarights' `Options` has `Access`, `AuditLog`, `Notify`, `Holders`), and keeps `Accounts` after it. `identity.Options.Lookup` is required; `Service.EmailOf` forwards; `Actor`/`AuditActor` are documented in identity.md at step 8.
- **S7, 2026-11-01** (step 7 applied by `spec-editor`): `regional.md` written from the moved `settings.md` text and this spec's Interface. It leaves two questions for the step-7 implementer: where the public document's terms and languages come from (this spec's `regional.Options` has no terms source) and how a logo change takes part in settings' revision lock and snapshot now that `settings` does not import `files` and `regional` has no `DB` (`regional.md`, Open questions 1 and 2). Until they are settled, treat the `Options` list under Interface (`regional`) as incomplete. Settled by A2 below.

- **A2, 2026-11-01 (approved by the owner 2026-11-01)**: the design gaps step 7 (`regional`) exposed, read against the uncommitted implementation (`internal/core/regional`, `internal/core/settings`, `internal/core/files`, `internal/app/core_regional.go`). Nine points; each gives the decision, the reason, the alternative considered, and what changes in this spec and in `regional.md` (the folding is `spec-editor`'s, after approval). The rule throughout: the behaviour settings decision 16 bought (one logo row across instances, a cheap boot document, an `ETag` that moves with the logo) stays; `regional` depends on `settings`, never the reverse; `settings` does not import `files`.

  1. **The logo and the settings revision (regional.md Open question 2).** *Decision:* `settings.Service` gains one write seam, `Revise`, and `regional`'s two logo writes run inside it; the logo bumps the revision as before, the `ETag` of `GET /public` and `GET /theme.css` is the revision alone again, and `Public` reads the logo's file ID from a small per-process cache keyed by the revision, not from the files store on every request. *Reason:* the revision row lock is the one thing that serialises writers across instances, and only settings may hold it; the logo ID left settings' snapshot with `files`, so the cache is what keeps the boot document a map lookup. *Alternatives:* (a) a hidden `regional.logo` key written through `Set` under the lock: settings has no "internal-only" key (decision 16 refused it for that reason), and `Set` is one transaction that cannot carry files' `Attach`, so the row and the key could disagree; (c) keep the implementation (own `DB`, an in-process mutex, `ByRecord` on every `/public`, `ETag` `"regional-<rev>[-<file>]"`): two uploads on two instances can leave two rows (F49 fails), and the boot document does I/O on every boot.
     - **Interface, `settings`:**
       ```go
       // Revise runs fn inside one Write transaction after taking the revision row lock, which
       // serialises it with Set, SetTerms and every other Revise across instances. When fn
       // reports changed, Revise bumps the revision and, after commit, installs the snapshot it
       // built under the lock (as Set does); when fn reports no change, it commits nothing new
       // and returns the current revision. fn's error rolls everything back and is returned with
       // its kind. Revise records no audit entry (fn records under its own area) and checks no
       // permission: it is a method of *settings.Service, which only app holds (modules see a
       // Reader), and the caller has checked its own permission first. Call it outside any
       // transaction. Consumer: regional (SetLogo, RemoveLogo).
       func (s *Service) Revise(ctx context.Context, fn func(ctx context.Context) (changed bool, err error)) (Revision, error)
       ```
       `Revision(ctx) (Revision, error)` (the current snapshot's revision; consumer: regional's `ETag` and cache key) is accepted as written. The `settings` paragraph under Interface lists both among what `settings.Service` keeps.
     - **Interface, `regional`:** `Options` gains `DB db.DB` (used for `ReadOnly()` alone, so a logo write refuses a read-only copy before a byte of the body is read, settings decision 18; `Revise` owns the transaction) and keeps `Revision RevisionSource`, now two methods, both satisfied by `*settings.Service`:
       ```go
       type RevisionSource interface {
           Revision(ctx context.Context) (settings.Revision, error)
           Revise(ctx context.Context, fn func(ctx context.Context) (changed bool, err error)) (settings.Revision, error)
       }
       ```
       `SetLogo`: `Allow(settings.manage)`; `DB.ReadOnly()`; `Stage` outside any transaction; `Revise(fn)` where fn runs `Attach{Record: LogoRef, Replace: true}` and `Audit.Record` and reports `true`; `LogoResult{Revision: the bumped revision, Logo: id}`. `RemoveLogo`: `Allow`; `ReadOnly()`; `Revise(fn)` where fn runs `DeleteByRecord(LogoRef)`, reports `false` when it deleted nothing (no entry, no bump, the current revision is returned: idempotent), else records and reports `true`. The in-process mutex goes. `Public` and `ThemeCSS`: `Revision()` first, then the body, so the body is at least as new as the tag; the `ETag` is `"regional-<revision>"`; the logo's file ID comes from `(revision, id)` held in an `atomic.Pointer`, refreshed with one `ByRecord` when the revision read differs from the held one (so one query per revision change per process, and none while nothing changes). The cache is correct because every logo change bumps the revision; a replica's poll moves its revision and its next `/public` re-reads.
     - **Failure modes (new rows):** B34 `settings.Revise`, fn fails → fn's kind, rollback, no bump, no snapshot change; caller fails its use case; `TestRevise_FnFails_RollsBackAndKeepsRevision`. B35 `settings.Revise`, read-only copy → `Unavailable` wrapping `db.ErrReadOnly` before fn runs; `TestRevise_ReadOnlyCopy_ReturnsUnavailable`. B36 `settings.Revise`, fn reports no change → none: the current revision, no bump, no snapshot replace; `TestRevise_Unchanged_ReturnsCurrentRevision`. B37 `settings.Revise`, `core.setting_revision` has no row → `Internal` (settings F26); `TestRevise_NoRevisionRow_ReturnsInternal`. B38 `settings.Revise`, two callers at once (two instances) → none: serialised by the row lock, the second runs after the first commits; `TestRevise_Concurrent_Serialised` (integration). `TestRevise_Changed_BumpsAndInstallsSnapshot` proves the happy path (the local snapshot carries the new revision at once). B9 is widened by points 2 and 3.
     - **regional.md:** `Options` and `RevisionSource` as above; Behavior "The logo": "`Transact(Write)`: `LockRevision` first ... `BumpRevision`" becomes "`Revise(fn)`", and "the next snapshot is the old one with `logo` = the new ID" becomes "after commit settings installs the bumped revision; the next `Public` sees it and re-reads the ID"; "Why the snapshot carries the ID (decision 16)" becomes "Why the logo bumps the revision" with the cache in place of the snapshot field; F49 keeps its test (now through `Revise`); F54 becomes "`Public`: the cache refresh's `ByRecord` fails → the store's kind (Unavailable, Timeout, Canceled)", test `TestPublic_LogoLookupFails_ReturnsUnavailable`; the existing `TestPublic_ETag_FollowsTheRevisionAndTheLogo`, `TestPublic_ETagAndLogoMoveTogether` and `TestPublic_AfterLoad_DoesNotTouchTheFilesStore` keep their names with the cache semantics (one `ByRecord` per revision; the fake counts calls); Open question 2 closed.
  2. **Terms for the public document (regional.md Open question 1).** *Decision:* `Options.Terms TermSource` (`TermsFor(ctx, lang) (map[string]settings.Forms, error)`, satisfied by `*settings.Service`) is **required**; `Languages` comes from `settings.Languages`, a build constant, so it needs no seam; the document's JSON field set (`name`, `contact_email`, `public_url`, `language`, `timezone`, `languages`, `theme`, `logo`, `terms`) is unchanged and pinned by `TestGetPublic_FieldSetIsPinned`. *Reason:* a public document with empty term maps is a different document (the shell merges `terms` over its catalogue, so a regional's renamed words would silently vanish); `settings` is always built before `regional`; an optional seam that changes a document's content hides a wiring mistake. *Alternative:* optional with empty maps, as implemented: rejected for the reason above. *Changes:* this spec's `regional.Options` gains `Terms TermSource // settings; required`; B9 reads "nil `Files`, `Settings`, `Terms`, `Revision`, `DB` or any dependency"; new test `TestNew_NilTerms_ReturnsInvalid`; regional.md Summary, Interface and Open question 1 updated.
  3. **The other `Options` additions.** `DB`: kept, for `ReadOnly()` only (point 1). `Revision`: the two-method `RevisionSource` (point 1); `settings.Service.Revision` accepted. `Meter`: dropped from `regional.Options`, as D2 and D4 dropped unused Meters; nothing in `regional` counts (point 9a says why the stale-URL count does not move here). *Changes:* this spec's `regional.Options` block (no `Meter`; `DB`, `Terms` added; `Revision` documented as above); Summary "Exported names added" gains `regional.RevisionSource`, `regional.TermSource`, `settings.Service.Revision`, `settings.Service.Revise`; `regional.New` on a `Meter`-less `Options` is what the app passes.
  4. **Audit action names.** The old actions were `settings.logo.changed` and `settings.logo.removed` (verified in `internal/core/settings/settings.go` at HEAD). *Decision:* `regional.logo.changed` and `regional.logo.removed`: a prefix change only, as Summary item 11 says and the owner intended; `changed` is also the verb of `settings.setting.changed` and `settings.term.changed`. The Interface text under `regional` ("the audit actions become regional.logo.set|removed") and the implementation's `regional.logo.set` are corrected. Spans stay `regional.logo.set|remove|open`: a span name is not an audit action. *Changes:* this spec's `regional` Interface comment; regional.md Summary (decision 5 and "Owns"), Interface comments, Behavior, and the tests `TestSetLogo_RecordsRegionalLogoSet` → `TestSetLogo_RecordsRegionalLogoChanged` (`TestRemoveLogo_RecordsRegionalLogoRemoved` unchanged); settings.md's amendment line names the new actions.
  5. **`settings.Decl.Normalize`.** *Decision:* accepted: `Normalize func(value string) string` on `Decl`, an optional rewrite of a trimmed value before `Check` runs and the value is stored. *Reason:* the canonical form of a value is the declaring area's rule, as `Check` already is; the settings-private map keyed by full key (`regionalNormalizers`) was the coupling that kept the `regional.*` declarations inside settings. *Rules:* pure; a fixed point (`Normalize(Normalize(v)) == Normalize(v)`), so a value it cannot improve is returned unchanged and `Check` reports why it is refused; it runs only for non-`Secret` kinds, and a `Secret` declaration with a `Normalize` fails `Decl.Validate` (`Invalid`, the declaration's field, code `unsupported`), because a secret is never trimmed or rewritten. *Alternative:* keep the map in settings and have regional register normalizers at construction: a second registration path for one key kind, and a seam taken outside `Options`. *Changes:* settings.md Interface (`Decl` gains the field with the rules above; Behavior "Writes" says "trimmed, normalised, parsed by kind, the `Decl.Check` run"); tests `TestSet_NormalizeRunsBeforeCheck` (settings) and `TestDeclValidate_SecretWithNormalize_ReturnsInvalid`; `TestSet_Regional_Table` keeps the canonical-form rows in `regional`. This spec: the `settings` Interface paragraph names the field.
  6. **OpenAPI names.** *Decision:* accepted: operationIds `regionalPublic`, `regionalTheme`, `regionalLogo`, `regionalSetLogo`, `regionalRemoveLogo`; schemas `RegionalPublic`, `RegionalLogoUpload`, `RegionalLogoResult`; tag `regional`; the five leave settings' fragment. *Reason:* ids and schema names carry the area prefix (api decision 33's convention), and an id named for an area that no longer serves it would mislead the generated client. The only consumer affected is the generated `web/src/api/schema.d.ts`; the shell calls by path. *Changes:* regional.md Summary ("Owns" names the ids and schemas); api.md item 24 lists the renamed ids with the moved paths.
  7. **The interim demo ceiling in `files`.** *Decision:* accepted as staging, like D3: `files.Options.Settings` is required now (B23 holds) and the service maps it itself; `files.Options.Policy` stays as an optional override and `files.PolicyFrom` stays exported until step 10, when `Options.Ceiling` replaces `Policy`, `PolicyFrom` is unexported and `lazyFilesPolicy` leaves the seam table (its row already says "removed in step 10"). Condition: no caller of `PolicyFrom` but app's demo cap. *Alternative:* land `Ceiling` in step 7: it belongs with demo's `FileCeiling` and the files jobs, which are step 10's. *Changes:* this spec's `files` Interface block gains a one-line note "until step 10: `Policy PolicySource` (optional override) and `PolicyFrom` exported"; files.md item 18's "pending" clause names them.
  8. **Part order.** No change to the build-order table: files (10) and regional (11) already precede notify (14), and notify's site source reads `regional.Profile`. Row 11's "Takes" becomes "audit, authz.Lookup, db (ReadOnly), settings "regional", settings (revision, `Revise`, terms), files "settings"". As built at step 7 the parts run audit, authz, settings, jobs, auditlog, identity, people, departments, events, files, regional, notify, demo; identity and people move below notify in steps 8 and 9 as the table says, and `TestParts_OrderAndNames_Pinned` pins each step's order.
  9. **Other observable changes in the diff, accepted or corrected:**
     - (a) *accepted:* `bms.settings.stale_values` no longer counts a stored `regional.public_url` the canonical-form rule refuses; `regional.Profile` warns once per process (`setting` attribute, no value) and reads it as unset. The counter is settings' own signal for kind and option drift, which it still counts; one key does not earn a `bms.regional.*` instrument (hence no `Meter`, point 3). regional.md's Settings text ("reads as unset with the decision-5 warning") gains "logged once; not counted".
     - (b) *accepted:* the `ETag` text changes from `"settings-<rev>"` to `"regional-<rev>"`; harmless, because the path changed with it, so no client holds the old tag for the new URL.
     - (c) *corrected by point 1:* `LogoResult.Revision` is the bumped revision again; the implementation returned the revision read before the write.
     - (d) *accepted:* `settings.Options.Validate` no longer refuses an outside area named `regional`: the duplicate check refuses a second `regional` area, and `module` reserves the name (api item 24); settings.md Behavior "Construction" drops the clause.
     - (e) *accepted:* `Public` flattens `Profile`'s fields instead of embedding `settings.Regional`; the JSON is the same (point 2's pinned test). The colours are not in the document (they are in `theme.css`).
     - (f) *unchanged, confirmed:* the admin document's area order (regional first: app passes it first); the handler order of `PUT /logo` (`settings.manage` before `ReadUpload`, D52(e)); `OpenLogo` through `ByRecord`, never the cache, so a replica never serves a deleted ID.
  - **Proposed changes elsewhere added by A2** (applied 2026-11-01 by `spec-editor`, all items; the body of this spec, `regional.md`, `settings.md`, `files.md`, `api.md` and `07-abstractions.md`): settings.md (item 17): `Revise`, `Revision`, `Decl.Normalize` and its rules, the construction clause (9d), the action names (4); files.md (item 18): the interim `Policy`/`PolicyFrom` (7); api.md (item 24): the operationIds (6); regional.md: everything listed under "regional.md" above, and Open questions 1 and 2 closed; this spec's Summary: exported names (3) and the `Options` list; `07-abstractions.md` (item 2): the settings entry gains "`Revise`: the revision lock as a seam for `regional`".
