# app: amendment history

Amendment narratives, the old part table, the lazy-adapter amendments, the superuser alert pointer, the demo adapters, the superseded wording of the decisions and steps, and the earlier Summary, moved verbatim out of `plan/design/app.md` on 2026-11-01, when boundaries step 13 rewrote the body to describe `app` as it now is (`plan/design/boundaries.md`, Proposed change 10). Cross-references inside the moved text to "How later core services plug in" mean the table below.

## Notes moved from the spec header

- **Amended 2026-11-01 (boundaries step 9, `boundaries.md` Proposed change 10, in part):** "The superuser alert" moves to `identity.md` and leaves a one-line pointer here; `internal/app/alert.go` and `lazyAlerter` go. The rest of change 10 (the part table, the lazy amendments, Imports) lands at step 13; the `notify` row and the Proposed change 20 below still describe the old adapter until then.
- **Amended 2026-10-30 (owner, D57; module-volunteers Amendment V3 in `plan/design/history/module-volunteers.md`, Proposed change 19):** `settingsAreas` builds an area for a module that declares settings or terms; two catalogue tests (F44). See "Amendment 2026-10-30" before Open questions.
- **Amended 2026-10-28 (D53, D54, owner approval of demo and telemetry T1):** the `demo` part (11) and the commands `demo seed|reset` (demo.md Proposed change 2), and the client-telemetry wiring (telemetry.md Proposed change 21: `web.ClientManifest`, `api.RouteTokens`, `intake.NewUsage`, the `usage counts:` lines); section "Amendment 2026-10-28" before Open questions; F38 to F43.
- **Amended 2026-10-17 (owner):** `check` ignores a never-migrated disabled module and exits 2 without a session key; `openapi` prints indented JSON; `migrate` creates the data directory only when it writes the secrets file; the part-name check uses `module.IsCoreArea`; debug logging is announced (decision 18); reviewer-accepted implementation deviations are recorded ("Accepted deviations").
- **Amended 2026-10-18 (owner approval of audit/identity):** core part order is audit 1, identity 2 (identity's `build` takes the audit recorder; audit needs nothing from identity at build time); the audit and identity rows of "How later core services plug in" are filled in (audit.md Proposed change 1, identity.md Proposed change 3).
- **Amended 2026-10-18 (owner approval of authz/people):** the `authz` and `people` rows of "How later core services plug in" are filled in (authz.md Proposed change 1, people.md Proposed change 1): the actor closure, the lazy adapters for identity, the `migrate` `seed` step, the `export` and `delete` commands, `link` for `PersonData` and the page contributors.
- **Amended 2026-10-30 (D58; people P-W1 Proposed change 30, jobs J-W1 Proposed change 19, in `plan/design/history/jobs.md`):** the `people` row sets `people.Options.Session = identity.SessionFrom`; the catalogue tests gain `TestMessageCatalogues_EnglishCoversJobs` and its fixture test (F30a); `bms check` is unchanged. See "Amendment 2026-10-30 (D58)" before Open questions.
- **Amended 2026-10-18 (M1 build, accepted deviations):** implementation deviations recorded in Behavior, "Accepted implementation deviations (M1 build, 2026-10-18)"; `serve` step 8 and the shutdown closer list updated (a seventh closer, `audit`).
- **Amended 2026-10-18 (M2 build, accepted deviations):** implementation deviations recorded in Behavior, "Accepted implementation deviations (M2 build, 2026-10-18)": operator attribution, `migrate` and its exit statuses, command pre-parsing, new exported surface (`api.Permissions`, `core.modules.read`), the Imports list, and three open spec gaps for the architect.
- **Amended 2026-10-19 (D39, owner approval of settings and notify):** the `settings` (part 3) and `notify` (part 9) rows of "How later core services plug in" are filled in (settings.md Proposed change 3, notify.md Proposed change 3); `internal/app/alert.go` implements `identity.Alerter` (notify.md Proposed change 7); the adapters `settingsPolicy`, `smtpSource`, `siteSource`, `retentionSource`, `lazyAlerter` and `lazyNotifier` are named; `coreSetOrder` gains `notify`; `bms check` prints the e-mail lines. No existing behaviour changes.
- **Amendment 2026-10-19 (approved, owner, 2026-10-20, D40):** part 3 (`settings`) receives `Authz: lazyAuthorizer{a}` over `a.authz`, which part 4 sets, so no part is reordered (settings.md amendment of the same date, in `plan/design/history/settings.md`); F31 to F33. `api.Options.PublicOrigin` waits (api.md amendment); when it lands `app` fills it over `a.settings.Regional`. See "Amendment 2026-10-19 (approved, owner, 2026-10-20, D40): `lazyAuthorizer` for settings" before Open questions.
- **Amended 2026-10-20 (D44, owner approval of files):** the `files` row (part 10) of "How later core services plug in" is filled in (files.md Proposed change 4): the store from `cfg.Files`, `people.Source`, the two jobs, `migrate` creating `<files.dir>`, `check` printing the store, `bms files verify`, `svc.Start` failing on a public bucket; the files directory is part of the data directory's contents.
- **Amended 2026-10-20 (D43, owner approval of jobs):** the `jobs` row (part 8) of "How later core services plug in" is filled in (jobs.md Proposed change 2): built after audit, settings and authz, `signals.New` receives `lazyDeferrer{a}` (no setter), `link` registers `audit.purge`, `auditActor` answers `ActorSystem` inside a job, `bms check` prints the registered jobs; Proposed change 8 applied.
- **Amended 2026-10-21 (D47, owner approval of the settings logo amendment; settings.md Proposed change 14):** part 3 receives `Files: lazyFilesStore{a}` (a lazy `files.Store` scoped `settings` over `a.files`, `Unavailable` before the files part is built), and `settingsRoutes` copies `MaxBodyBytes` and `Timeout` from `settings.Route` to `module.Route`; F36, F37. See "Amendment 2026-10-21 (D47): `lazyFilesStore` for the settings logo" before Open questions.
- **Amendment 2026-10-20 (approved, D44; jobs Q-JOBS6):** `auditActor` answers `{Kind: audit.ActorSystem}, true` when `jobs.InJob(ctx)`, checked before the session and the operator; it depends on audit.md's amendment of the same date (the `ActorFunc` may answer the system), without which every `Record` from a module's job handler is `Internal`. Section "Amendment 2026-10-20" below; F34, F35.
- **Accepted 2026-10-20 (owner, D40):** the implementation deviations marked "(implementation, 2026-10-19)" in this spec are accepted as written; no text changed.

## The Summary as it stood before 2026-11-01

## Summary

**Purpose:** run the command the operator asked for over the core assembled from its configuration. Composition root: `internal/app`, helper `internal/app/secrets`, binary `cmd/bms` (a one-liner over `app.Main`). The open questions are answered (owner, 2026-10-17).

**Exported (`app`):**
- `Main(ctx, args, environ []string, stdout, stderr io.Writer) int` runs one command in process, never panics on operator input, never calls `os.Exit`.
- `Version() (string, error)`: `-ldflags -X ...internal/app.version`, else `debug.ReadBuildInfo`, else `dev`; must be a telemetry token of at most 64 bytes.
- Exit statuses (pinned, `TestExitCodes_Pinned`): `ExitOK=0` (also `serve` stopped by signal), `ExitFailure=1` (runtime failure, closer failed, interrupted), `ExitUsage=2` (bad config/OTEL variable/unknown module name/registry or routing problem/missing session key, also for `check`), `ExitSchema=3` (schema behind or ahead, changed migration file, lock held).
- `secrets.Ensure(ctx, path, fields) (created []string, err)`: creates the secrets file once (0600, dir 0700, 64 random bytes base64 per field), adds missing fields, never overwrites, atomic replace; `secrets.FieldBytes = 64`.
- Unexported seams every later core spec writes against: `command{name, summary, usage, run}`, `process`, `assembly` (cfg, tel, dbc, pool, migrator, disp, plan, set, ui, coreSignals, corePoints, coreSets, corePerms, routeSets, authenticator, permissions, deps, runnables, commands), `coreDeps`, `part{name, declare, build, link}`, `runnable{name, start, stop}`, `closer`, `shutdown(...)`, `provision`, `skipSchema`.

**Commands:** `serve`, `migrate`, `check`, `openapi`, `version`, `help` (later `create-superuser`, `rotate-secret` from identity; `export`, `delete` from people; `files verify` from files, `demo seed|reset` from demo (D53), added through parts).

**Key decisions:**
- D1 `app.Main` owns command line, config loading and exit status; second SIGINT/SIGTERM terminates immediately with the OS default (escape hatch; owner, 2026-10-17).
- D2 commands are a table that parts' `declare` appends to; names unique.
- D3 exactly four exit statuses; `check` uses the same numbers as `serve` would for everything it can observe without starting: configuration, telemetry, the registry, the database, the schema, the build and `api.New`; what only `serve` does (`Set.Start` against a real database, the files probe, listening) is reported, not scored (Q-JOBS10, D44).
- D4 `serve` logs to stdout; all other commands log to stderr so stdout carries the command's output alone (owner, 2026-10-17; D20 clarification).
- D5 each command loads config itself; `version`, `help`, `openapi` load none.
- D6 `serve` requires the session key from M0 (owner, 2026-10-17).
- D7 `openapi` and `check` build modules against `db.None()` with the schema check skipped; a factory that reads the database fails.
- D8 missing or stale web build and a missing collector are warnings/nothing, never fatal.
- D9 start order: config, telemetry, registry, database, signals, parts, modules, HTTP shell, `Set.Start`, runnables, `Listen`.
- D10 stop order HTTP, runnables, signals, modules, database, telemetry within `http.shutdown_timeout`; HTTP gets `max(1s, 2*budget/3)`; every closer is called even after failure; shutdown ctx is `WithoutCancel`.
- D11 `serve` starts on a read-only copy; `migrate` refuses one.
- (D57) `settingsAreas` builds an area for a module that declares settings or terms; the catalogue tests check module terms (F44).
- D12 later core services plug in as ordered parts with `declare`, `build`, `link`, `start`/`stop` (order: audit, identity, settings, authz, people, departments, events, jobs, notify, files, demo; D37, D53), not a registry. Settings' `Options.Files` is `lazyFilesStore{a}` (resolved on first use, D47), like `lazyAuthorizer`.
- D13 end-to-end test runs the real assembly in process through `Main`, plus one subprocess signal test.
- D14 `migrate` creates data dir and secrets file before touching the database, only when `cfg.Session.Key` is unset (a read-only filesystem with `BMS_SESSION_KEY` injected works), then reloads config.
- D15 the module list is `internal/app/modules.go` (blank imports) mirrored by `web/src/modules/index.ts`.
- D16 `TestMessageCatalogues_EnglishCoversManifests` lives here and requires `messages/en.json` for every registered module.
- D58 (2026-10-30) `people.Options.Session` is `identity.SessionFrom` (people decision 34); `internal/app` also holds `TestMessageCatalogues_EnglishCoversJobs`, which requires `module.<owner>.title` and `module.<owner>.job.<ident>.title` in the English catalogues for every job a `Start` registers (jobs decision 24).
- D17 no drain delay in M0 (owner, 2026-10-17); `http.drain_delay` and `api.Server.BeginDrain()` are designed with Phase 1 packaging if a regional needs them.
- D18 debug logging is announced (owner, 2026-10-17): `serve` warns after "bms starting" (`log.level=debug`), other commands print the same warning to stderr, `check` prints `log level: debug (logs may contain personal data)`; scrubbing stays on at every level. `check` without a session key exits 2 like `serve`; a never-migrated disabled module is not a `check` problem; `openapi` prints indented JSON.
- D19 accepted deviations (reviewed, 2026-10-17) listed under Behavior "Accepted deviations" (Version `+` to `-`, per-closer "step stopped" and one final "stopped", wall-time shutdown deadlines, `check` with a broken `OTEL_*` still runs later sections, "interrupted: rerun the command", `Load`/`Ensure` under `checkTimeout`, temp file + `link(2)`, `e2ehang` module in `internal/app`, `make check` needs Node, `openapi-check` temp file).

**Errors callers handle:** `Main` converts everything to exit statuses (F3 config `Invalid` to 2; F4 OTEL `Invalid` to 2; F5 unknown `BMS_MODULES_DISABLED` name to 2; F6 no session key to 2; F7 `Resolve` failure to 2; F8 `db.Open` `Invalid` to 2, `Unavailable` to 1; F9 `Build`: `Conflict` from `CheckSchema` to 3, `Invalid` to 2, others to 1; F10 `api.New` to 2; F11-F13 start/listen/serve failures to 1 after stopping what started; F19 read-only `migrate` to 1; F20 migrate `Conflict` to 3). `Version` and `secrets.Ensure`: `Invalid` (never echoes content or path contents), `Internal` (rand failure), `Canceled`/`Timeout`. F28 duplicate part/command names are `Internal`.

**Owns (config, files, constants, no permissions/signals/settings):** consumes `config.Config` (all sections) and copies sections into each package's options; writes `cfg.Secrets.File` (default `<data.dir>/secrets.json`) and creates `cfg.Data.Dir` (0700). Registers permission `core.modules.read` for `api` via `authz`'s part. Part names are checked with `module.IsCoreArea` (F28). Constants (not keys): `buildTimeout` 30s, `startTimeout` 30s, `listenTimeout` 5s, `migrateTimeout` 30m, `checkTimeout` 30s, `signals.HandlerTimeout` 5s, `openapi` `info.version` `dev`. Log lines: "bms starting", the debug-logging warning (D18), "listening", "stopping", "stopped" (fields `version, instance, go_version, exporting, addr, modules, read_only, ui, ui_version`). Makefile targets: `fmt vet build test lint test-integration imports web-check web-build openapi-check bin run migrate test-e2e check check-go up observe down ps`.

**Expects of dependencies:** config (`Load`, `Keys`, `SecretsFieldSessionKey`, decisions 7/12, F22/F22b); module-contract (`Registry`, `Resolve`, `Plan`, `Provision`, `Deps` incl. `Tracer`, `SchemaChecker`, `Set`, `ModuleRoutes`); db (`Open`, `Migrator`, `Pool.Ping/ReadOnly/Close`, `ErrReadOnly`, plus the proposed `db.None()` and `ErrNoDatabase`); telemetry (`New` with `Stdout`/`Environ`, `Close` last); signals (`New`, `Scope`, `Close`); api (`New`, `Listen`, `Serve`, `Shutdown`, `OpenAPI`); web (`web.FS`, `web.Version`); authz (`PermissionDecl`); from 2026-10-28 (D53, D54): demo (`demo.New`, `Link`, `Protected`, `Channel`, `Policy`), identity `Guard`, notify `ChannelFactory`, files `PolicySource`, api `Options.NoIndex` and `Options.Usage`, `api.RouteTokens`, `web.ClientManifest`, telemetry `intake.NewUsage`.

**Open questions:**
- None. Answered by the owner (2026-10-17): no drain delay in M0 (D17); non-`serve` commands log to stderr (D4); `serve` requires the session key from M0 (D6); a second signal terminates immediately (D1).

**Proposed changes elsewhere (2026-10-17):** items 11 (settings, notify) applied 2026-10-19 (D39) as the part 3 and part 9 rows, plus 18 to 20 below. Applied to config, module-contract, db, telemetry, signals, web-shell, api, 07-abstractions, 09 §1, add-module and go-conventions; items 9 (identity) and the audit part of 11 applied 2026-10-18; item 10 (people) and 11 (authz) applied 2026-10-18 (owner approval of authz/people). Item 10 (departments-events) applied 2026-10-18 (D37): parts 6 `departments` and 7 `events`, jobs 8, notify 9, files 10. Item 8 (jobs) applied 2026-10-20 (D43). Pending: files (target not written), the D20 clarification (plan-keeper), the Makefile targets and `scripts/e2e.sh` (implementation, created when app is built), `docs/operating.md`.

**M2 build deviations (2026-10-18).** authz is part 4 and people part 5; the audit actor is the system inside a job (`jobs.InJob`; amendment 2026-10-20, approved D44), else the session's person, else operator while `a.operator` is set (create-superuser, export, delete, migrate); `migrate` builds the parts and seeds after Migrate; `export`/`delete` pre-parse the ID (exit 2); commands build modules but do not start them; `api.Permissions` and `core.modules.read` (`StarterRoles: authz.SiteAdminRoles`) are exported. Three open spec gaps are listed in Behavior.

## Superseded wording in the body

**Responsibilities, "The seams for the rest of the core":**

- **The seams for the rest of the core.** Define where each later core service hooks in (a
  *part*) so that `identity`, `authz`, `audit`, `settings`, `people`,
  `departments`, `events`, `jobs`, `notify` and `files` arrive as one new file each and no
  change to the commands or the lifecycle.

**Not responsible for, rows for people, export and deletion, and authz:**

| Sessions, the superuser, `create-superuser`, `rotate-secret` | `identity` (`identity.md`). The commands are registered through `app`'s command table by `identity`'s part. |
| Permissions and the `Authorizer`; registering the declared permissions | `authz` (`authz.md`). `app` hands it `Plan.Permissions()` and the core's declarations (`api.PermissionModulesRead`) through its part. |
| Export and deletion of one person across modules; the `export` and `delete` commands | `people` (`people.md`), over `Set.PersonData()`, through its part. |

**Sub-abstractions, the Parts row:**

| Parts | The seam each later core service plugs into: `declare`, `build`, `link`, `start`, `stop`, in a fixed order. | M0 has no parts; the table is the design. |

**Permissions, the paragraph on commands and `core.modules.read`:**

None. Every command runs on the host as the operator, with no actor; the operating
system's access to the environment, the data directory and the database URL is the
control. Commands that act on a person's data (`export`, `delete`, M2) are `people.md`'s:
it names the permission the command is audited under and `app` only dispatches them.
`app` registers one permission on behalf of `api`: `core.modules.read`
(`api.PermissionModulesRead`, scope site, reaches internal), handed to `authz` with the
core's other declarations through `authz`'s part (api.md Proposed change 10).

**Decision 12:**

12. **Later core services plug in as parts with a fixed order and five optional hooks,
    not as a plugin registry.** The core is a known, finite list; each service needs the
    plan (authz: `Permissions()`), the built set (people: `PersonData()`), or neither, and
    some have background work (jobs). Five hooks (`declare`, `build`, `link`, and the
    runnable's `start`/`stop`) cover every case named in the plan, and an ordered slice
    in `core.go` with one file per part (`core_identity.go`, ...) means adding a service
    is one file plus one line, and the commands and the lifecycle are not touched
    (Responsibilities: "The seams for the rest of the core"). A `init`-time registry for
    core parts was rejected: the core is not forkable the way modules are, and an
    explicit order is what the dependencies between parts need (authz after identity,
    people after authz, jobs after signals). The table in "How later core services plug
    in" is the contract each later spec writes against.

**`serve`, steps 6 to 8 and 10 as they read before the Deferrer and the Lookups were built first and the schema check was folded into the build:**

6. `signals.New(signals.Options{Logger: ...With("component", "signals"), Tracer:
   tel.Tracer("signals"), Meter: tel.Meter("signals"), Clock: p.clock, HandlerTimeout:
   5 * time.Second, Deferrer: lazyDeferrer{a} /* jobs, M4: resolves a.jobs at call time */})`.
7. Run every part's `build` (F28); the registry's and the parts' permission
   declarations are gathered as `corePerms` with `api.PermissionModulesRead` first.
8. `checkSchemas` first checks the core sets (exit 3 when one is behind; only then does it also name the module sets); then `a.plan.Build(ctx /* buildTimeout */, migrator, a.provision)` (F9 stays with `Plan.Build`). Run every part's
   `link` (people takes `set.PersonData()`).
9. `web.FS()`: ok → `a.ui`; `NotFound` → nil and warn "serving without a UI" (F17).

10. `intake.New(intake.Options{Logger: ...With("component", "intake"), Meter:
    tel.Meter("intake"), Clock: p.clock})`. `api.New(api.Options{Addr, RequestTimeout,
    MaxBodyBytes, TrustedProxies, AllowPlaintext from cfg.HTTP; Logger:
    ...With("component", "api"); Tracer: tel.Tracer("api"); Meter: tel.Meter("api");
    Clock; Version; Routes: append(set.Routes(), a.routeSets...); Database: pool; Intake;
    Graph: plan.Graph(); Schema: a status func over migrator.Status with the core sets
    and the module sets; Authenticator: a.authenticator; Permissions: a.permissions; UI:
    a.ui})` (F10).

**Imports:**

`app`: standard library (`context`, `errors`, `fmt`, `io`, `io/fs`, `log/slog`, `net`,
`os`, `path/filepath`, `runtime`, `runtime/debug`, `strings`, `time`), `internal/config`,
`internal/lib/*`, `internal/model`, `internal/core/module`, `internal/core/db`,
`internal/core/telemetry`, `internal/core/telemetry/intake`, `internal/core/signals`,
`internal/core/authz`, `internal/core/identity`, `internal/core/audit`, `internal/core/people`, `internal/contracts/personpage`, `internal/api`, `web`, `internal/app/secrets`,
and in `modules.go` every shipped module as a blank import (`_ ".../internal/modules/
hello"`). Never pgx (depguard `storage-seam`: that is why `db.None()` must live in `db`),
never `go.opentelemetry.io`, never `dbtest` or `telemetrytest` outside test files.

## The internal shape later core specs hook into (before 2026-11-01)

### The internal shape later core specs hook into

These identifiers are unexported: nothing outside `internal/app` uses them. They are in
the spec because every later core spec says where it plugs in, and a reviewer must be able
to check that against something written down. Fields typed by specs that are not yet
Approved are listed as comments, as module-contract does for `Deps`.

```go
// command is one entry of the command table. Core parts add theirs in declare (identity:
// create-superuser, rotate-secret; people: export, delete). Names are "[a-z][a-z0-9-]*".
type command struct {
	name    string
	summary string // one English line for `bms help` (operator text, like log lines)
	// usage is the argument synopsis, "" when the command takes none: "<person-id>".
	usage string
	// run does the work. p carries the writers, the environment, the registry, the parts
	// and the clock; it loads configuration itself when it needs it (decision 5). It
	// returns the exit status and never calls os.Exit.
	run func(ctx context.Context, p *process) int
}

// process is one invocation of Main: what every command starts from.
type process struct {
	args     []string
	environ  []string
	lookup   func(key string) (string, bool) // over environ; telemetry.Options.Environ
	stdout   io.Writer
	stderr   io.Writer
	clock    clock.Clock      // clock.Real() in Main; a fake in tests
	registry *module.Registry // module.Default() in Main; a fresh one in tests
	parts    []part           // coreParts in Main; a test's own in tests
	version  string           // Version()
}

// assembly is the core being built: what the commands fill in and what the parts read and
// extend. One per command run. It is not safe for concurrent use; it is built on one
// goroutine before anything serves.
type assembly struct {
	cfg      config.Config
	version  string
	clock    clock.Clock
	tel      *telemetry.Telemetry
	logger   *slog.Logger // tel.Logger().With("component", "app")
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
	coreSets      []db.Set                    // the core's migration sets, in part order (identity, people, authz, departments, events, audit, settings, jobs, files; db.md Proposed change 5)
	corePerms     []authz.PermissionDecl      // the core's permission declarations: api.PermissionModulesRead first, then each part's
	routeSets     []module.ModuleRoutes       // one per core area with routes (accounts, people, ...); Module is the area name
	authenticator api.Authenticator           // identity (M1); nil until then
	permissions   api.PermissionChecker       // authz (M2); nil until then
	deps          coreDeps                    // copied into every module's Deps by provision
	runnables     []runnable                  // started after Set.Start, stopped before Set.Stop (jobs, M4)
	commands      []command                   // added by declare
}

// coreDeps holds the Deps fields the core provides to every module beyond Logger, Tracer,
// Clock, Links, DB and Signals (module-contract "Deps fields by spec"). Each field is
// added here, and copied in provision, when its spec is Approved:
//   Settings settings.Reader   (scoped per module by provision)   settings.md
//   Authz    authz.Authorizer                                      authz.md
//   People   people.Directory                                      people.md
//   Audit    audit.Recorder                                        audit.md
//   Jobs     jobs.Registrar    (scoped per module by provision)    jobs.md
//   Notify   notify.Notifier   (scoped per module by provision)    notify.md
//   Files    files.Store                                           files.md
type coreDeps struct{}

// part is how one core service plugs in. Every func is optional (nil means nothing to
// do). The order of coreParts is fixed (Behavior: "How later core services plug in") and
// a part never reads another part's output except through the assembly fields above.
type part struct {
	name string // a reserved core area name (module-contract "Reserved module names"); unique
	// declare runs for every command that resolves the registry, before Resolve, with no
	// I/O: it appends to coreSignals, corePoints, coreSets, corePerms and commands.
	declare func(a *assembly) error
	// build runs after Resolve and before module Build, in serve, check and openapi: it
	// constructs the service from a.cfg, a.dbc, a.disp (a scope for its own signals),
	// a.plan (Permissions, Settings) and a.tel, and sets deps, authenticator, permissions,
	// routeSets and runnables. It does no I/O beyond reads and succeeds against db.None()
	// (decision 7).
	build func(ctx context.Context, a *assembly) error
	// link runs after module Build, before api.New, in serve and openapi: it receives
	// a.set (people: PersonData; definers: contributions) and may add route sets.
	link func(ctx context.Context, a *assembly) error
}

// runnable is a core component with background work, started after the modules and
// stopped before them. jobs is the first (M4).
type runnable struct {
	name  string
	start func(ctx context.Context) error
	stop  func(ctx context.Context) error
}

// closer is one shutdown step. shutdown runs them in order within budget: the first
// (HTTP) under max(1s, 2*budget/3), every other under the shared deadline start+budget;
// each is called even after an earlier failure or an exhausted deadline (every closer is
// idempotent and returns at once on a done ctx); errors are joined and returned (F15,
// F16). ctx is detached from cancellation (context.WithoutCancel): the signal that
// started the shutdown must not cut it short. Consumers: serve; the lifecycle tests.
type closer struct {
	name  string
	close func(ctx context.Context) error
}

func shutdown(ctx context.Context, logger *slog.Logger, clk clock.Clock, budget time.Duration, steps []closer) error

// provision is the module.Provision for this assembly (module-contract Proposed change
// 9; telemetry.md and signals.md Proposed change 5): Logger = tel.Logger().With("module",
// m.Name); Tracer = tel.Tracer("module." + m.Name); Clock = a.clock; DB = a.dbc; Signals
// = a.disp.Scope(m.Name, m.Publishes, m.Subscribes); plus a.deps, scoped where the field's
// spec says so. It fails with the kind of whatever refused (F29).
func (a *assembly) provision(ctx context.Context, m module.Manifest) (module.Deps, error)

// skipSchema is the module.SchemaChecker openapi and check use for Build: CheckSchema
// returns nil (the schema is reported by Status instead, decision 7).
type skipSchema struct{}
```

## How later core services plug in (the part table, before 2026-11-01)

### How later core services plug in

Each row is one `part` in `coreParts` (`internal/app/core.go`), in this order, defined in
`internal/app/core_<name>.go` when its spec is Approved. The order is a dependency order:
a later part may read what an earlier one set on the `assembly`.

| Order | Part | `declare` | `build` | `link` | Runnable | Deps field |
|---|---|---|---|---|---|---|
| 1 | `audit` (M1) | migration set `audit` (`coreSets` order unchanged: no foreign keys); `corePerms` += `audit.Permissions`; settings declarations | `audit.New` over `cfg.Instance.Name`, `cfg.Data.Dir`, the pool, clock, telemetry, the actor closure (over `identity.SessionFrom`, called per request only), and `a.titles`/`a.permissions` (nil until authz's part sets them: read lazily through small adapters); `a.deps.Audit` left unscoped and scoped in `provision` with `Scoped(m.Name)`; route set `audit` from `Routes()`/`OpenAPI()` as `module.ModuleRoutes{Module: "audit"}` (`audit.Route` converted to `module.Route`) | — | `Run` (the spool importer) starts after `Set.Start` and stops with the runnables; `Close` after `Set.Stop`, before `db.Close` | `Audit` (scoped per module) |
| 2 | `identity` (M1) | `identity.Set` in `coreSets` at its place in db.md's fixed order (identity first; part order does not change migration order); `identity.Permissions` to `corePerms`; `SignalAccountCreated` to `coreSignals`; command `create-superuser` (`usage: <email>`; `rotate-secret` later) | `identity.New` over `cfg.Session.Key`, `cfg.HTTP.AllowPlaintext`, `a.dbc`, the clock, telemetry, `audit.Scoped("identity")` (audit is built first), `a.disp.Scope("identity", {SignalAccountCreated}, nil)`, every seam nil; sets `a.authenticator` and route set `accounts` (`identity.Route` converted to `module.Route`); the audit actor closure is `func(ctx) (audit.Actor, bool)` over `identity.SessionFrom` (`{Kind: ActorPerson, Person, Superuser}`; a restricted session is still the person for audit) | — | — | — |
| 3 | `settings` (M4; D39) | `settings.Set` in `coreSets` (set name `settings`, schema `core`, already in db's fixed order); `settings.Permissions` to `corePerms`; route set `settings` | `settings.New` over `a.dbc`, the clock, telemetry scope `settings`, `audit.Scoped("settings")`, the authz actor closure, `Authz: lazyAuthorizer{a}` (amendment 2026-10-19, pending: over `a.authz`, which part 4 sets), `Areas`: the core areas `identity.Settings` (with `identity.CheckSettings`), `people.Settings`, `auditsettings.Settings` (leaf package `internal/core/audit/auditsettings`; `settings` imports `audit`, so `audit` cannot import `settings`; implementation, 2026-10-19) and `notify.Settings` (`Core: true`) and `a.plan.Settings()` converted to `[]settings.Area{Core: false}`, `Files: lazyFilesStore{a}` (D47: over `a.files`, which part 10 sets), and `SealingKey: a.cfg.Settings.SealingKey`; sets `a.settings` and `a.deps.settings`; the `PolicySource` adapters `settingsPolicy{r}` for identity (over `Scoped("accounts")`) and for people (over `Scoped("people")`), set as lazy adapters because both are built earlier; route set `settings` (`settings.Route` converted to `module.Route`, the public routes with `Access: AccessPublic`, `MaxBodyBytes` and `Timeout` copied; D47) | — | `Run` (the refresher) starts after `Set.Start` and stops with the runnables | `Settings` (`provision` sets `Deps.Settings = a.deps.settings.Scoped(m.Name)`) |
| 4 | `authz` (M2) | `authz.Set` in `coreSets` (set name `authz`, already in db's fixed order); `authz.Permissions` to `corePerms`; core signals `access.grant-created`, `access.grant-revoked`; a `seed` step for `migrate` | `authz.New` over `a.dbc`, the clock, telemetry scope `authz`, `audit.Scoped("access")`, `a.disp.Scope("access", {the two}, nil)`, the actor closure `func(ctx) (authz.Actor, bool)` over `identity.PersonFrom` and `identity.SessionFrom` (`Superuser` from the session), `Declared: append(a.corePerms, a.plan.Permissions()...)`, `Departments: lazyDepartments{a}` (D37; answers `Unsupported` until part 6 is built); sets `a.permissions`, `a.titles`, `a.lister`, `a.deps.authz` (`provision` copies it, unscoped); route set `access`. identity is built before authz, so its `Permissions` and `Lister` are the lazy adapters (`lazyPermissions`, `lazyLister`, in a shared file) | — | `migrate` runs each part's `seed func(ctx, *assembly) error` after `db.Migrate` on the writer: authz calls `Seed(ctx, declared)` and prints `access: <n> starter permissions seeded` when `n > 0` | `Authz` |
| 5 | `people` (M2) | `people.Set` in `coreSets` (set name `people`); `people.Permissions` to `corePerms`; core signals `people.person-created`, `people.person-deleted`, `people.deletion-requested`; extension point `people.person-page` (`Accepts`: a `personpage.Provider`); commands `export` and `delete` (`usage: <person-id>`; both require the session key like `create-superuser`; run `ExportCommand`/`DeleteCommand` after `Build` and `Link`) | `people.New` over `a.dbc`, the clock, telemetry scope `people`, `audit.Scoped("people")`, `a.disp.Scope("people", {the three}, {identity.SignalAccountCreated})`, `a.deps.authz`, the authz actor closure, `Accounts: a.identity`, `Access: a.authz`, `AuditCount: a.audit`, `Session: identity.SessionFrom` (D58, people decision 34), `Policy: nil` (settings' part sets a lazy adapter); sets `a.deps.people`; route set `people`; identity's `build` gets `Names: lazyNames{a}` (empty until `a.names` is set) | `link` converts `a.set.PersonData()` to `[]people.Source` and `a.set.Contributors(personpage.Point)` to `[]people.SectionProvider` and calls `Link` (Proposed change 2(a)) | — | `People` |
| 6 | `departments` (M3; D37) | migration set `departments` (already in db's fixed order); `departments.Permissions` to `corePerms`; core signals `departments.department-created`, `departments.department-archived`, `departments.department-restored` | `departments.New` over `a.dbc`, the clock, telemetry scope `departments`, `audit.Scoped("departments")`, `a.disp.Scope("departments", published, nil)`, `a.deps.authz`, the authz actor closure; sets `a.departments` and `a.deps.departments`; route set `departments`; `lazyDepartments{a}` (in `core_adapters.go`) is what `buildAuthz` passes as authz's `Departments` | — | — | `Departments` (unscoped) |
| 7 | `events` (M3; D37) | migration set `events`; `events.Permissions` to `corePerms`; core signals `events.event-created`, `events.event-changed`, `events.event-closed`, `events.event-reopened`; extension point `events.event-setup` (`module.ExtensionPointDecl{Name: eventsetup.Point, Accepts: a eventsetup.Provider}`) | `events.New` likewise (scope `events`, `SetupTimeout: events.DefaultSetupTimeout`); sets `a.events` and `a.deps.events`; route set `events`; `import _ "time/tzdata"` (in `app` or `cmd/bms`) so zone names resolve without system data | `a.set.Contributors(eventsetup.Point)` converted to `[]events.SetupProvider`, passed to `Link` | — | `Events` (unscoped) |
| 8 | `jobs` (M4; D43) | migration set `jobs`; route set `jobs`; `jobs.Permissions` to `corePerms` | built after audit, settings and authz: `jobs.New` over the pool and `ReadOnly()`, `audit.Scoped("jobs")`, `a.deps.authz`, the actor closure, `a.disp` as `Redeliverer`, `cfg.Instance.Name` and the constants of Q-JOBS7 (`Workers` 2, `PollInterval` 10 s, `Lease` 2 m, `RunTimeout` 5 m); `signals.New` still runs before the parts and receives `Deferrer: lazyDeferrer{a}` (resolves `a.jobs` at call time, `Unavailable` before it is built), so no setter is needed; `a.deps.jobs` is scoped per module in `provision`; `auditActor` answers `ActorSystem` when `jobs.InJob(ctx)`, before the session and the operator (Q-JOBS6 as corrected; audit's amendment of 2026-10-20, D44) | `link` registers `audit.purge` (hourly) through `a.jobs.Scoped("audit")` with a closure over `a.deps.audit.Purge` and `a.settings.Scoped("audit").Duration("retention")` (0 = skip); `bms check` prints the registered jobs in its jobs section, and a module whose `Start` fails there (called under `db.None()`) is printed as a problem line and does not set `ExitFailure` (jobs Q-JOBS10, D44; `serve` still stops on a failing `Start`); `migrate` is unaffected | `Run`/`Close`: started after `Set.Start`, stopped after HTTP drained and before `signals.Close` (the first of the runnables to stop) | `Jobs` (scoped) |
| 9 | `notify` (M4; D39) | migration set `notify` (`coreSetOrder` in `internal/app/core.go` gains `"notify"` after `"jobs"`); `notify.Permissions` to `corePerms` | `notify.New` over `a.dbc`, `a.clock`, `a.tel`, `audit.Scoped("notify")`, `a.authz`, the actor closure, the settings adapters `smtpSource` and `retentionSource` (over `Scoped("notify")`) and `siteSource` (over `Regional` and `TermsFor`), the recipient adapters (`AddressSource`: the account's address first, then people's `ContactEmailOf` on `NotFound` or empty; `LanguageSource` and `NameSource` over people's `LanguageOf` and `PlayaNameOf`), `Hostname` from `cfg.Instance.Name`; registers `identity.Templates`, `people.Templates` and `notify.Templates`; sets `a.notify`, `a.deps.notify` (scoped by `provision`: `Deps.Notify = a.deps.notify.Scoped(m.Name)`) and route set `notify`; the lazy adapters `lazyAlerter{a}` (identity's `Alerter`, `internal/app/alert.go`) and `lazyNotifier{a, owner: "people"}` (people's `Notify`) resolve `a.notify` at call time | `link` registers each built module's `Manifest.Templates`; `linkPeople` (not notify's `link`) adds `people.Source{Name: "notify", Exporter: a.notify}` (implementation, 2026-10-19; the part order puts notify after events, because jobs isn't built yet) | `Run`/`Close`: started after `Set.Start`, stopped after HTTP drained and before `signals.Close` (notify decision 2: its own sender loop, not `jobs`) | `Notify` (scoped) |
| 10 | `files` (M4; D44) | migration set `files` (`coreSetOrder` after `jobs`); `filesettings.Settings` as core area `files`; `files.Permissions` if any | the `objects.Store` from `cfg.Files` (`disk.New(filepath.Join(cfg.Files.Dir, "store"))` or `s3.New`); `files.New` with `audit.Scoped("files")`, the actor closure, `TempDir` `<files.dir>/tmp` and a `PolicySource` over a `files` `Reader`; `provision` sets `a.deps.Files = svc.Scoped(m.Name)` | appends `people.Source{Name: "files", Exporter: adapter}` after the modules; gives settings a lazy `files.Store` scoped `settings`; registers through `runner.Scoped("files")` the `jobs.Decl`s `files.reap` (`Every: files.ReapEvery`) and `files.sweep-orphans` (`Every: files.SweepEvery`), both `Timeout: files.JobTimeout`, closures over `svc.Reap` and `svc.SweepOrphans` (as `audit.purge`); `migrate` creates `<files.dir>`, `<files.dir>/tmp` and, for `disk`, `<files.dir>/store` (0700); `check` prints `files: <store> ok` or the `Ping` error; command `bms files verify` (prints the four counts; exit 0 when nothing is missing or mismatched, 1 otherwise, 2 on config) | `start` calls `svc.Start`; `Invalid` wrapping `objects.ErrPublic` is a start failure (exit 1, message under files.md Behavior), an unreachable store is not | `Files` (scoped) |
| 11 | `demo` (D53) | migration set `demo` (`coreSetOrder` after `files`); `demo.Permissions` is empty (none owned); the command `demo` with subcommands `seed` and `reset` (`usage: seed | reset`; both require the session key like `create-superuser`; run `Seed`/`Reset` after `Build` and `Link`); `demoseed.Point` in `Env.CoreExtensionPoints` (`Accepts`: a `demoseed.Provider`) | `demo.New` over `cfg.Demo`, `a.dbc`, the clock, telemetry scope `demo`, `audit.Scoped("demo")`, the authz actor closure (`ListMail`'s viewer), the seams as adapters over the real services (`Accounts` and `Addresses` over identity, `Access` over authz, `Profiles` over people, `Departments`, `Events`, `Settings`), `Schemas` from the core set order plus the plan's manifests, `Schema` = the `migrate` step, `Objects` over the `objects.Store` and `cfg.Files.Dir`, `Files` the settings-backed `PolicySource`; when `cfg.Demo.Enabled`: identity's `Guard` = a lazy adapter over `demo.Protected`, notify's `ChannelFactory` = `func(SMTPConfig) (Channel, error) { return demo.Channel(), nil }`, files' `PolicySource` = the demo service over the settings-backed one, `api.Options.NoIndex` = true; route set `demo` | `Link` over `a.set.Contributors(demoseed.Point)` | none | none |

**The demo part (D53).** Built and linked in every process; with `cfg.Demo.Enabled` false it declares its tables and the command and does nothing else, so a production install never sees it. `serve` logs `demo mode on` (warn) after "bms starting"; `check` prints `demo: off` or `demo: on (seeded <time>|not seeded|seeding)` (a failed state read prints `demo: on (state unknown: <kind>)` and is not a failure). Exit statuses for the two commands: 2 for `Unsupported` and `Conflict` `seeded`, `not_empty` or `not_demo`, 3 for the migrator's `Conflict`, else 1. The actor closures already prefer a session in ctx over the operator, which is what lets the seed act as the demo accounts (M2 deviations). `TestDemo_ChangesOnlyTheListed` (permission catalogue, settings catalogue, signal names and route list differ from the production assembly only by the `demo` route set) and the end-to-end demo test live here.

**The superuser alert (D39)** moved to identity (boundaries step 9): see `identity.md`, "The superuser alert". `app` holds no alert code.

**`bms check` and e-mail (D39).** `check` prints `e-mail: configured host:port (security)` or `e-mail: not configured`, the outbox counts, and `smtp: ok` or the `Ping` failure; none of these fails the check.

Rules every part keeps: `declare` does no I/O; `build` does no I/O beyond reads and
succeeds against `db.None()` (decision 7) and on a read-only copy; a part's signals scope
comes from `a.disp.Scope(<area>, publishes, subscribes)`; a route set's `Module` is the
area name, never `core`, `modules` or `admin` (api.md F10); a part that needs a
configuration key proposes it to `config.md` and reads it from `a.cfg`. Because `jobs`
must exist before `signals.New` and the dispatcher must exist before `provision`, `serve`
step 6 is placed after the parts whose `build` the dispatcher does not need; in M0–M3 the
distinction is moot and the implementation may call `build` for parts 1–7, then
`signals.New`, then `build` for parts 8–10. `TestParts_OrderAndNames_Pinned` pins the
order and that every name is a reserved core area name.

## Accepted implementation deviations (M1 build, 2026-10-18; M2 build, 2026-10-18)

### Accepted implementation deviations (M1 build, 2026-10-18)

- **F11:** a runnable start failure maps through `statusOf`, so an Invalid spool directory exits 2.
- `checkSchemas` checks the core sets before the build: exit 3, and only when a core set is behind does it also name the module sets (F9 stays with `Plan.Build`). Serve step 8 says so.
- `openapi` passes a nil `Authenticator`. `openapi` and `check` use a non-secret `placeholderSessionKey`; `requireSessionKey` refuses that exact value.
- A seventh closer, `audit`, in the order http, runnables, signals, modules, audit, db, telemetry (steps 14 and F15 updated).
- The audit runnable's start creates `<data.dir>/audit/spool` (0700) before `Run`.
- `lazyTitles` and the `a.titles` field are there for authz in M2.
- `create-superuser` resolves modules before building parts, so a `BMS_MODULES_DISABLED` typo exits 2.
- `check` prints `audit spool: N waiting, M failed`, or `empty`, via `audit.SpoolCounts`.
- **Open spec drift (for the architect, not fixed here):** the part table gives `notify` a migration set, but db.md's fixed set order has no `notify`.

**M2 build (2026-10-18):**

- **Part order.** authz is part 4 and people is part 5.
- **Operator attribution.** The audit actor is the session's person, else operator while `a.operator` is set. `a.operator` is set by `create-superuser`, `export`, `delete` and `migrate`. (Amendment 2026-10-20, approved D44: the system inside a job comes first; see below.)
- **`migrate`.** It builds the parts and seeds after `Migrate`. `migrateStatus`: a failing migration file exits 1 whatever its kind, and a migrator `Conflict` exits 3.
- **`export` and `delete`.** They pre-parse the ID (exit 2) and `delete` refuses a read-only copy.
- **Commands build modules but do not start them.**
- **New exported surface.** `api.Permissions`, and the exported `core.modules.read` declaration with `StarterRoles: authz.SiteAdminRoles` (also noted in api.md).
- **Imports.** The `app` list gains `internal/core/identity`, `internal/core/audit`, `internal/core/authz` (beyond `PermissionDecl`), `internal/core/people` and `internal/contracts/personpage`.
- **Open spec gaps (for the architect, not resolved here):**
  - `people.person-deleted` published by `bms delete` reaches no module handler, because modules subscribe in `Start`.
  - A module's `PersonData` must work on a built but unstarted module.
  - `migrate`'s seed step must reuse `serve`'s split sequence once `jobs` arrives (M4).

## Amendment 2026-10-19 (approved, owner, 2026-10-20, D40): `lazyAuthorizer` for settings

Gap found while applying D39: part 3 (`settings`) needs `settings.Options.Authz` (required, settings F1) and authz is part 4. Decision: a lazy adapter, as for identity and audit (`lazyPermissions`), not a part reorder; the reason is in settings.md's amendment of the same date (settings consults the authorizer only on a request, after every part is built; a reorder renumbers four specs and the pinned order test for nothing).

`internal/app/core_adapters.go` gains:

```go
// lazyAuthorizer is settings' authz.Authorizer over a.authz, which authz's part sets after
// settings is built: until then every decision is denied (errs.Permission) and ExtentOf is
// nowhere (a zero Extent, nil), as the services do when they are given no authorizer. It
// does no I/O of its own. settings calls only Allow (every settings permission is
// site-scoped); AllowIn and ExtentOf are implemented so the value is a complete Authorizer.
type lazyAuthorizer struct{ a *assembly }

func (l lazyAuthorizer) Allow(ctx context.Context, p authz.Permission) error
func (l lazyAuthorizer) AllowIn(ctx context.Context, p authz.Permission, d model.DepartmentID) error
func (l lazyAuthorizer) ExtentOf(ctx context.Context, p authz.Permission) (authz.Extent, error)
```

The part 3 row's `build` cell reads `Authz: lazyAuthorizer{a}` (over `a.authz`, nil before part 4's build). A request can never reach a settings use case before `Build` has run every part, so the denying branch is visible only to a test that builds part 3 alone. `TestParts_OrderAndNames_Pinned` is unchanged; `lazyPermissions` stays as it is (identity and audit need only `Allow`). When `api.Options.PublicOrigin` lands (api.md amendment of the same date, in `plan/design/history/api.md`; not M4), the api step of `serve` fills it with a closure over `a.settings.Regional(ctx).PublicURL`.

### Failure modes (added)

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F31 | `lazyAuthorizer.Allow`, `AllowIn` | `a.authz` nil (authz's part not built) | Permission | cannot happen on a request; a test of part 3 alone sees 403 | `TestLazyAuthorizer_BeforeAuthz_DeniesAndEmptyExtent` |
| F32 | `lazyAuthorizer.ExtentOf` | `a.authz` nil | zero `Extent`, nil | as above | the same test |
| F33 | `lazyAuthorizer.*` | `a.authz` set | the service's own answer and error, unwrapped (`//nolint:wrapcheck` as the other adapters) | — | `TestLazyAuthorizer_AfterAuthz_Delegates` |

## Amendment 2026-10-20 (approved, D44; jobs Q-JOBS6): the system actor inside a job

Gap found while implementing jobs.md: Q-JOBS6 (approved, D43) has `auditActor` answer `ActorSystem` when `jobs.InJob(ctx)` "with no change to `audit`", but audit's `resolveActor` refuses anything from the `ActorFunc` but a person or the operator (`Internal`), so the wiring as approved would fail every `Record` in a module's job handler. audit.md's amendment of the same date widens the function's accepted answers by that one kind (and nothing else: an inconsistent system or operator is still `Internal`, F13b there). This amendment records `app`'s side, which is what Q-JOBS6 meant.

`internal/app/core_audit.go`, `auditActor`, in this order:

1. `jobs.InJob(ctx)`: `audit.Actor{Kind: audit.ActorSystem}, true`. A handler runs as the system with no session (jobs decision 5); this comes **before** the session because audit's own rule for a delivery is "the delivery beats the function", a runner's ctx never carries a session, and `jobstest.Fake.Run` runs a handler on the test's ctx, which may carry one: the entry must still say `system`.
2. `actorFromSession(ctx)`: the person, as today.
3. `a.operator`: the operator, as today.
4. otherwise `audit.Actor{}, false` (audit records `anonymous`).

`app` already imports `jobs` (part 8), so no import changes. The doc comment on `auditActor` drops "Audit asks it only outside a signal delivery": it is still true, but the sentence that matters is that the system is answered for a job here and for a delivery in audit, and the two are the same actor kind. `bms check` and the commands are unaffected: no job runs while `a.operator` is set (the runner starts only in `serve`), so rows 1 and 3 never meet.

### Failure modes (added)

`auditActor` cannot fail (it reads ctx and a field; no I/O); the rows are the answers it must give, since a wrong one is an `Internal` from audit on every job's `Record`.

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F34 | `auditActor` | `jobs.InJob(ctx)` true, with or without a session, with or without `a.operator` | `{Kind: ActorSystem}`, `true`, no `Person`, no `Superuser` (audit accepts it, amendment 2026-10-20) | — | `TestAuditActor_InJob_IsTheSystem` (table: a job ctx alone; a job ctx that also carries a session; a job ctx while `a.operator` is set; a job ctx from `jobstest.Fake.Run`) |
| F35 | `auditActor` | `jobs.InJob(ctx)` false | as before: the session's person, else the operator while `a.operator`, else `false` | — | `TestAuditActor_Command_IsTheOperatorOnlyWithoutASession` (unchanged) |

## Amendment 2026-10-21 (approved, owner, D47): `lazyFilesStore` for the settings logo

The settings logo amendment (settings.md, D47) gives `settings.Options` a `Files files.Store` for the regional's logo. Files is part 10 and settings part 3, so the same pattern as `lazyAuthorizer` applies, with no part reordered. `internal/app/core_adapters.go` gains `lazyFilesStore{a *assembly}`: every `files.Store` method resolves `a.files.Scoped("settings")` on first use and delegates, returning the files service's answer and error unwrapped; before the files part is built each method answers `errs.Unavailable`. It does no I/O of its own. A request can never reach a settings use case before `Build` has run every part, so the `Unavailable` branch is visible only to a test that builds part 3 alone. The files part's `link` step no longer needs to hand settings anything (files.md Proposed change 4 said "a lazy `files.Store` scoped `settings`"; this is where it lives). `settingsRoutes` copies `MaxBodyBytes` and `Timeout` from `settings.Route` to `module.Route`. The e2e stack already runs the `disk` store, so the smoke run's logo round trip needs no profile.

### Failure modes (added)

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F36 | `lazyFilesStore.*` | `a.files` nil (files' part not built) | Unavailable | cannot happen on a request; a test of part 3 alone sees 503 | `TestLazyFilesStore_BeforeFiles_ReturnsUnavailable` |
| F37 | `lazyFilesStore.*` | `a.files` set | the files service's own answer and error, unwrapped (`//nolint:wrapcheck` as the other adapters) | — | `TestLazyFilesStore_AfterFiles_Delegates` |

## Amendment 2026-10-28 (approved, owner, D53 and D54): demo part and client-telemetry wiring

Demo: the `demo` row above, the `demo` command, the `Guard`, `ChannelFactory`, `PolicySource` and `NoIndex` wiring only when `cfg.Demo.Enabled` (demo.md decisions 1 and 2). Nothing else in `app` changes in demo mode.

Client telemetry (telemetry.md T1, Proposed change 21): after `web.FS()`, `app` calls `web.ClientManifest()`. `NotFound` or `Invalid` gives one warning and empty lists; `bms check` prints `client manifest: missing` as a failure (`ExitFailure`) in a build that has a UI and as a note in one that has none. `app` builds `intake.New` with `Options.Screens` (the manifest's patterns) and `Options.Routes` (`api.RouteTokens(routes)`), builds `intake.NewUsage` with `UsageOptions{Enabled: cfg.Telemetry.Usage, Screens, Actions, Logger().With("component","usage"), Meter("usage"), clock}` and passes it as `api.Options.Usage`. `check` and the start-up line print `usage counts: on (<n> screens, <m> actions)` or `off`. The `openapi` command builds both services with `Enabled: false` and empty lists.

### Failure modes (added)

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F38 | `demo` command | `Seed`/`Reset` return `Unsupported` or `Conflict` `seeded`/`not_empty`/`not_demo` | as returned | exit 2 | `TestDemoCommand_ExitStatuses` |
| F39 | `demo` command | the migrator's `Conflict` (schema ahead or behind) | `Conflict` | exit 3 | `TestDemoCommand_MigratorConflict_Exit3` |
| F40 | `build` | `cfg.Demo.Enabled` true | — | Guard, ChannelFactory, PolicySource and NoIndex are wired; false: none is | `TestDemo_EnabledWiresTheFourAdapters`, `TestDemo_ChangesOnlyTheListed` |
| F41 | `serve`, `check` | `web.ClientManifest` is `NotFound` or `Invalid` | warning (serve starts); `check` fails in a build with a UI | build the web bundle | `TestServe_ClientManifestMissing_WarnsAndStarts`, `TestCheck_ClientManifestMissing_Fails` |
| F42 | `openapi` | builds `intake` and `Usage` | — | both with `Enabled: false` and empty lists | `TestOpenAPI_UsageBuiltDisabled` |
| F43 | `check` | `telemetry.usage` on or off | — | prints `usage counts: on (<n> screens, <m> actions)` / `off` | `TestCheck_PrintsUsageCounts` |
| F44 (D57) | `settingsAreas`; catalogue test | a module declares terms but no settings; a `module.<name>.term.<key>[.<form>]` entry is missing or differs from the manifest's `en` default; a module message or template names a term that is neither core nor its own | the module still gets an area (its terms appear on `/settings/terms`); the two catalogue failures are test failures, not runtime errors | fix `en.json` or the manifest | `TestSettingsAreas_TermsOnlyModule_HasArea`, `TestCatalogue_ModuleTerms_MatchManifestDefaults`, `TestCatalogue_TermsUsed_AreDeclared` |

## Accepted implementation deviations (demo and telemetry build, 2026-10-29)

- `UsageOptions.Modules map[string]string` (screen pattern to the screen set that owns it) is filled by `app` from the client manifest's `set` field; the `module` attribute falls back to the pattern's first path segment, or `shell` for `/` (telemetry.md T1.3).
- The two intake services take separate meter scopes, `intake` (client errors) and `usage` (client events).
- `bms check` prints `client manifest: N screens, M actions`, or `client manifest: missing`, then the usage-counts line (F43). A missing manifest is `ExitFailure` only when the build has a UI (`missing (no UI is built)` otherwise).
- `bms demo` has the usage `seed|reset`; `bms check` reads the marker through `demo.Service.State`.
- `api.RouteTokens` (given to intake) excludes `/healthz` and `/readyz`.

## Amendment 2026-10-30 (approved, owner, D57): areas for modules that declare terms

From module-volunteers Amendment V3 (Proposed change 19). `settingsAreas` builds an area for a module that declares settings *or* terms (today a module with no settings has no area), carrying `Plan.Settings()`'s `Terms`. The catalogue test (`catalogue_test.go`) gains `TestCatalogue_ModuleTerms_MatchManifestDefaults` (the four `module.<name>.term.*` keys exist and equal the manifest's `en` defaults) and `TestCatalogue_TermsUsed_AreDeclared` (every `{term_<x>...}` in a module's `messages/en.json` and `{{term "<x>"}}` in its templates is in `CoreTerms` or that module's `Terms`).

## Amendment 2026-10-30 (D58): the people session seam and the job-title check

Applied from people Amendment P-W1 (Proposed change 30; `plan/design/history/people.md`) and jobs Amendment J-W1 (Proposed change 19; `plan/design/history/jobs.md`, W1.12), approved 2026-10-30 (D58). (1) `core_people.go` passes `people.Options.Session = identity.SessionFrom` (`Options.Validate` fails `people.session` when nil, people F102). (2) The `internal/app` catalogue tests gain `TestMessageCatalogues_EnglishCoversJobs` (F30a), beside `TestMessageCatalogues_EnglishCoversManifests`, and a fixture test; `bms check` is unchanged.
