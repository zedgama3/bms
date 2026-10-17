# app: the composition root

- **Status:** Draft (2026-10-17)
- **Package:** `internal/app` (Go module `github.com/zedgama3/bms`), plus the helper package
  `internal/app/secrets` (the generated-secrets file) and the binary `cmd/bms`. **UI:**
  none of its own; `serve` hands the built web shell to `api`.
- **Depends on specs:** `lib-errs.md` (Implemented): `errs.E`, `errs.Is`, `errs.KindOf`,
  every kind. `lib-core.md` (Implemented): `clock.Clock`, `clock.Real`, `clock.Wait`,
  `validate.Fields`, `validate.FieldError`, `validate.Errors`. `lib-secret.md`
  (Implemented): `secret.Secret.IsSet`. `model.md` (Approved): `model.ParseID` (the later
  `export` and `delete` commands). `config.md` (Approved): `Load`, `Keys`, `Config` and its
  sections, `SecretsFieldSessionKey`, decision 7 (the secrets file), decision 12 (sections
  copied into each package's options), "`modules.disabled`: how enablement is derived",
  F22, F22b, Proposed change 3. `module-contract.md` (Approved): `Registry`, `Default`,
  `Manifests`, `Resolve`, `Env`, `Plan` (`Order`, `Permissions`, `Settings`, `Graph`,
  `Build`), `Provision`, `Deps`, `SchemaChecker`, `Set` (`Routes`, `PersonData`, `Start`,
  `Stop`), `ModuleRoutes`, Proposed change 9. `db.md` (Approved): `Options`, `Open`, `Pool`
  (`Ping`, `ReadOnly`, `Stats`, `Close`), `Migrator` (`NewMigrator`, `Migrate`, `Status`,
  `CheckSchema`), `Set`, `Applied`, `Status`, `ErrReadOnly`, Proposed change 5.
  `telemetry.md` (Approved): `Options`, `New`, `Telemetry` (`Logger`, `Tracer`, `Meter`,
  `Exporting`, `Close`), `intake.New`, `intake.Options`, Proposed change 5. `signals.md`
  (Approved): `Options`, `New`, `Dispatcher` (`Scope`, `Close`), `Name`, Proposed change 5.
  `api.md` (Approved): `Options`, `New`, `Server` (`Listen`, `Serve`, `Shutdown`,
  `OpenAPI`), `Database`, `SchemaStatus`, `Authenticator`, `PermissionChecker`,
  `PermissionModulesRead`, Proposed change 11. `web-shell.md` (Approved): `web.FS`,
  `web.Version`, the Makefile targets, Proposed change 4. The leaf type
  `authz.PermissionDecl` (module-contract "Leaf types").
  **Used by:** `cmd/bms` (`Main`), the operator (the commands), `check-runner` and CI (the
  Makefile targets), `module-hello.md` (its integration test runs through `Main`), every
  later core spec (`identity`, `authz`, `audit`, `settings`, `people`,
  `departments-events`, `jobs`, `notify`, `files`: each plugs in through one *part*, see
  "How later core services plug in").

## Purpose

Run the command the operator asked for over the core assembled from its configuration.

## Responsibilities

- **Commands.** Parse the command line, run one command (`serve`, `migrate`, `check`,
  `openapi`, `version`, `help`; later `create-superuser`, `export`, `delete`,
  `rotate-secret` from their specs), write its output to stdout and its problems to
  stderr, and return one documented exit status.
- **Assembly.** Turn `config.Config` into the typed options of every core package
  (config.md decision 12), build the core in one fixed order (telemetry, database, signals,
  the core parts, the modules, the HTTP shell), derive the enabled module set from
  `modules.disabled`, and provide each module's scoped `Deps` (`Provision`).
- **Lifecycle.** Start everything in order, serve until the process is told to stop
  (SIGINT, SIGTERM, or a serving failure), and stop everything in the reverse order within
  `http.shutdown_timeout`, giving HTTP a sub-deadline so the closers behind it keep their
  share.
- **The module list.** Hold the one file that imports the modules compiled into this
  binary (D5): `internal/app/modules.go`. A fork adds a module with one import line there
  (and one in `web/src/modules/index.ts`, D31).
- **The installer step.** `bms migrate`: generate the secrets file on a new installation
  (config.md decision 7) and apply the core's and the enabled modules' migrations in order
  (D17).
- **The operator's view.** `bms check`: report configuration, telemetry, modules, database,
  schema state, API and UI without starting, and exit with the status `serve` would.
- **The seams for the rest of the core.** Define where each later core service hooks in (a
  *part*) so that `identity`, `authz`, `audit`, `settings`, `people`,
  `departments-events`, `jobs`, `notify` and `files` arrive as one new file each and no
  change to the commands or the lifecycle.
- **Tests that need everything.** Host the tests only the composition root can run: the
  message-catalogue check over every registered manifest (web-shell.md Proposed change 4),
  the compile-time `SchemaChecker` assertion (db.md), and the end-to-end start, request
  and stop of the real binary with the `hello` module.

## Not responsible for

| Concern | Owner |
|---|---|
| Reading and validating the environment; the catalogue of keys; the secrets file's format | `config` (`config.md`). `app` calls `Load` once per command, prints what it returns, and copies sections into options. `app` *writes* the secrets file (`secrets.Ensure`); `config` reads it. |
| The module contract: manifests, the registry, `Resolve`, `Build`, `Set.Start`/`Stop`, verification against the manifest, the graph | `core/module` (`module-contract.md`). `app` calls them in order and supplies `Env`, a `SchemaChecker` and a `Provision`. |
| The pool, statements, read-only detection, migrations, schema versions | `db` (`db.md`). `app` opens the pool, builds the migration sets, calls `Migrate`, `Status` and `Close`. |
| The logger, tracer, meter, OTLP export, the privacy policy, the error reference | `telemetry` (`telemetry.md`). `app` builds it first and closes it last. |
| Delivering signals, scoping, the after-commit hook | `signals` (`signals.md`). `app` builds one `Dispatcher`, scopes it per module and per core publisher, and closes it. |
| Routing, the middleware chain, the error mapper, OpenAPI assembly, serving the UI, `/healthz`, `/readyz` | `api` (`api.md`). `app` builds `api.Options`, calls `Listen`, `Serve` and `Shutdown`, and decides the shutdown deadline and order. |
| The browser application, its build, `dist/`, `version.txt` | `web-shell.md`. `app` embeds the result through the `web` package and warns when it is missing or stale. |
| Sessions, the superuser, `create-superuser`, `rotate-secret` | `identity` (`identity.md`). The commands are registered through `app`'s command table by `identity`'s part. |
| Permissions and the `Authorizer`; registering the declared permissions | `authz` (`authz.md`). `app` hands it `Plan.Permissions()` and the core's declarations (`api.PermissionModulesRead`) through its part. |
| Export and deletion of one person across modules; the `export` and `delete` commands | `people` (`people.md`), over `Set.PersonData()`, through its part. |
| Background work; running jobs only on the writing copy; retrying signal deliveries | `jobs` (`jobs.md`), a core part with `start` and `stop`. |
| Any module's behaviour; what a module needs from another | the module's spec; contracts and signals (D19). `app` never calls a module beyond the contract. |
| Packaging: the container image, systemd unit, compose file for production, backups, upgrades | `docs/operating.md` and `deploy/` (Phase 1 roadmap item). This spec fixes what they rely on: the commands, exit statuses, signals and the shutdown budget. |
| Reloading configuration on `SIGHUP` | nobody (config.md "Not responsible for": a restart). `SIGHUP` keeps its default disposition. |
| A drain delay between the stop signal and closing the listener, for probe-based balancers | not in M0 (Open question 1; api.md "Concurrency and lifecycle" notes the timing). |
| Cross-process coordination between the on-site and off-site copies | the later `switchover` spec (Q-OFFLINE, D27). Every command here works on a read-only copy except `migrate`, which refuses it. |

## Sub-abstractions

| Name | Purpose (one line) | Notes |
|---|---|---|
| `Main` and the command table | Turn a command line into one command run with one exit status. | `cmd/bms` is a one-liner over it; later specs add commands to the table. |
| `serve` | Build, start, serve, stop. | The only command that listens; the only one whose logs go to stdout. |
| `migrate` | Create the secrets file on a new installation and apply migrations in order. | Refuses a read-only copy; 30 min budget. |
| `check` | Report everything `serve` would check, without starting, and exit as `serve` would. | Continues past problems and reports them all. |
| `openapi` | Print the assembled OpenAPI document with no configuration and no I/O. | For `make openapi-check`; builds the modules against a database that refuses every statement. |
| `version`, `help` | Print the build version; print usage. | No configuration, no I/O. |
| Assembly and `provision` | Build the core in order and give each module its scoped `Deps`. | `assembly` is the one struct every part and every command works on. |
| Parts | The seam each later core service plugs into: `declare`, `build`, `link`, `start`, `stop`, in a fixed order. | M0 has no parts; the table is the design. |
| Lifecycle (`run`, `shutdown`) | Start in order; stop in reverse within `http.shutdown_timeout`, HTTP first with a sub-deadline. | A pure function over named closers, so the order is tested with fakes. |
| `secrets.Ensure` | Create the generated-secrets file once, 0600, and add missing fields without ever overwriting one. | config.md decision 7 and Proposed change 3. |
| `modules.go` | The list of compiled-in modules, one blank import each. | D5: a fork adds one line. |
| Makefile targets | The developer and CI entry points, one check per target. | `check-runner` runs them one at a time. |

## Interface

```go
// Package app is the composition root (plan/07-abstractions.md, layer rule 5): the only
// package, with cmd/bms, that imports config, the modules, the core implementations and
// the web embed. It builds the process from its configuration, runs one command, and
// stops cleanly. It may import anything in the repository.
package app

import (
	"context"
	"io"
)

// Exit statuses. They are the operator's API (systemd, compose, scripts) and are pinned
// by a test (decision 3).
const (
	ExitOK      = 0 // the command did its work; `serve` stopped cleanly after a signal
	ExitFailure = 1 // a runtime failure: database unreachable, start failed, a closer timed out, interrupted during start-up
	ExitUsage   = 2 // the operator's input: unknown command, invalid configuration or OTEL_* variable, missing session key, unknown module in BMS_MODULES_DISABLED, a registry or routing problem the build has
	ExitSchema  = 3 // the database schema is behind or ahead of the binary, or migrate found a changed file or another run: run `bms migrate` or upgrade the binary
)

// Main runs the command named by args[0] (the program name already stripped) with the
// environment environ (os.Environ() form) and returns the exit status. stdout receives the
// command's output; stderr receives problems and, for every command but serve, the log
// lines (decision 4). ctx is the process's lifetime: cmd/bms derives it from SIGINT and
// SIGTERM; a test cancels it. When ctx ends, serve shuts down in order and Main returns
// ExitOK (or ExitFailure if a closer failed); every other command stops where it is and
// returns ExitFailure. It never panics for an operator's input and never calls os.Exit.
// Consumers: cmd/bms (once); app's own tests (every command, in process).
func Main(ctx context.Context, args, environ []string, stdout, stderr io.Writer) int

// Version returns the build's version: the value -ldflags sets on the unexported
// variable `version` (the Makefile passes `git describe --tags --always --dirty`), else
// the main module's version from debug.ReadBuildInfo when it is not "(devel)", else
// "dev". It fails with errs.Invalid when the injected value is not a telemetry token of
// at most 64 bytes (F2): the build is wrong, not the operator. Consumers: Main (every
// command's start-up line, telemetry.Options.Version, api.Options.Version), `bms
// version`, the web-version comparison in serve.
func Version() (string, error)
```

```go
// Package secrets writes the generated-secrets file that config reads (config.md
// decision 7). It imports the standard library, lib/errs and config (for the field and
// file-name constants) only. Consumer: app's `bms migrate`.
package secrets

import "context"

// FieldBytes is how much crypto/rand material one generated field holds before base64.
const FieldBytes = 64

// Ensure makes sure the JSON object at path holds a non-empty string under every name in
// fields, generating FieldBytes random bytes (standard base64) for each missing or empty
// one, and returns the names it generated. When the file does not exist it creates the
// parent directory (0700, every missing level) and the file with O_CREATE|O_EXCL and
// mode 0600. When the file exists it reads it (at most config.MaxSecretFileBytes), adds
// the missing fields, preserves every other field verbatim, and replaces the file
// atomically (a temporary file in the same directory, 0600, fsync, rename, directory
// fsync). It never overwrites a non-empty value. Nothing created returns (nil, nil) and
// leaves the file untouched, byte for byte. ctx bounds the work (checked before each
// file operation). It fails with errs.Invalid when path is empty, fields is empty or
// holds a repeated or empty name, the directory or file cannot be created, opened,
// read or replaced, the existing file is not a regular file, or its content is not a
// JSON object whose known fields are strings (never echoing the content); with
// errs.Internal when crypto/rand fails; with errs.Canceled or errs.Timeout when ctx ends
// (F22, F23, F26).
func Ensure(ctx context.Context, path string, fields []string) (created []string, err error)
```

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

### Who needs each member

| Member | Consumer(s) | Pre-condition | Post-condition |
|---|---|---|---|
| `Exit*` | `cmd/bms` (`os.Exit`), operator docs, `TestExitCodes_Pinned`, the systemd unit example | — | stable numbers |
| `Main` | `cmd/bms`; every test in `app` | `args[0]` is a command name or absent | one exit status; nothing left running |
| `Version` | `Main` (start-up line, `telemetry.Options.Version`, `api.Options.Version`), `bms version`, the web-version comparison | the build set it, or not | a token |
| `secrets.Ensure`, `FieldBytes` | `migrate` (only when `cfg.Session.Key` is unset) | `cfg.Secrets.File` resolved by `config.Load` | the file holds every field; created names returned |
| `command`, `process` | the five built-in commands; later parts' commands | — | — |
| `assembly`, `coreDeps` | every command; every part; `provision` | — | — |
| `part`, `runnable` | the later core specs, one each | — | — |
| `closer`, `shutdown` | `serve`; `TestShutdown_*` | — | every step called once, in order |
| `provision` | `Plan.Build` (passed as the `Provision`) | Resolve done; `dbc`, `disp`, `tel` set | scoped `Deps` |
| `skipSchema` | `openapi`, `check` | — | `Build` proceeds without a database |

## Types

Defines `Exit*`, and in `secrets` `FieldBytes`; internally `command`, `process`,
`assembly`, `coreDeps`, `part`, `runnable`, `closer`, `skipSchema`, and the stub
`api.Database` for `openapi` (`Ping` nil, `ReadOnly` false). Uses `config.Config`,
`module.Registry`, `module.Plan`, `module.Set`, `module.Env`, `module.Deps`,
`module.Manifest`, `module.ModuleRoutes`, `module.ExtensionPointDecl`,
`authz.PermissionDecl`, `signals.Name`, `signals.Dispatcher`, `db.DB`, `db.Pool`,
`db.Migrator`, `db.Set`, `db.Status`, `telemetry.Telemetry`, `intake.Intake`,
`api.Server`, `api.Authenticator`, `api.PermissionChecker`, `clock.Clock`, `fs.FS`,
`slog.Logger`, `io.Writer`.

## Data

No tables. Files `app` writes, all under the operator's control:

| File | Written by | Mode | Content | Personal data |
|---|---|---|---|---|
| `cfg.Secrets.File` (default `<data.dir>/secrets.json`) | `migrate` through `secrets.Ensure`, once; fields added by later versions the same way | file 0600, directory 0700 | `{"session_key": "<base64 of 64 random bytes>", ...}`; unknown fields preserved | none; a credential (config.md "Data"). Never logged, never printed; `check` prints its path and whether it exists |
| `cfg.Data.Dir` | `migrate` (`MkdirAll`, 0700) | 0700 | the secrets file; later `audit`'s spool and `files`' local storage | none here |
| the temporary file beside the secrets file during a rewrite | `secrets.Ensure` | 0600 | the new content | as above; removed on any failure |

What `app` logs, by field (telemetry.md "Data" holds the retention rules): `version`,
`instance`, `go_version`, `exporting`, `addr` (the listen address), `modules` (enabled
names), `read_only`, `ui` and `ui_version`, the effective configuration as
`slog.Any("config", cfg)` (safe: every credential is a `secret.Secret`, config.md's leak
test), each shutdown step's `name`, `duration_ms` and `error.kind`, each applied
migration (`db` logs it). Never the database URL, the session key, a `PG*` value, an
`OTEL_EXPORTER_OTLP_HEADERS` value, or anything about a person: `app` has no actor and
touches no personal record. `check` prints the *names* of `PG*` variables that are set,
never their values (db.md decision 14).

## Permissions

None. Every command runs on the host as the operator, with no actor; the operating
system's access to the environment, the data directory and the database URL is the
control. Commands that act on a person's data (`export`, `delete`, M4) are `people.md`'s:
it names the permission the command is audited under and `app` only dispatches them.
`app` registers one permission on behalf of `api`: `core.modules.read`
(`api.PermissionModulesRead`, scope site, reaches internal), handed to `authz` with the
core's other declarations through `authz`'s part (api.md Proposed change 10).

## Signals

None published, none subscribed. `app` builds the `Dispatcher`, gives each module its
scope through `provision`, gives each core publisher its scope through that publisher's
part, lists the core's names in `Env.CoreSignals`, and closes the dispatcher at shutdown.
In M0 `Env.CoreSignals` and `Env.CoreExtensionPoints` are empty (signals.md "Payload keys
of the core's signals").

## Settings

None. Everything `app` reads is process configuration (`config.md`); nothing here differs
between regionals. Constants inside the package, pinned by `TestConstants_Pinned`, with
the reason each is not a key:

| Constant | Value | Why not configurable |
|---|---|---|
| `buildTimeout` | 30 s | bounds `Plan.Build` (schema checks and factories, reads only); statement deadlines (`db.query_timeout`) already bound each query; module-contract "Concurrency and lifecycle" proposed 30 s |
| `startTimeout` | 30 s | bounds `Set.Start` and the runnables' `start`: subscribing and launching goroutines, no writes (module-contract decision 14) |
| `listenTimeout` | 5 s | `api.Listen` binds a socket; longer means something is wrong |
| `migrateTimeout` | 30 min | bounds one `bms migrate` run (db.md decision 16: a backfill may run long); an operator who needs longer splits the migration |
| `checkTimeout` | 30 s | bounds each probe `check` makes (`Ping`, `Status`); `check` must finish while an operator watches |
| HTTP share of the shutdown budget | `max(1 s, 2 × budget / 3)` | api.md Proposed change 11: when `Shutdown` times out the other closers still have a third |
| `signals.HandlerTimeout` | 5 s | signals.md decision 5 (owner, 2026-10-16) |
| `openapi`'s `info.version` | `dev` | the committed `web/openapi.json` must not change with every commit (decision 7); the served document carries the real version |
| `openapi`'s telemetry | level warn, text, instance `openapi`, stderr, no `OTEL_*` read | the document alone goes to stdout; nothing is exported from a build step |

## UI

None. What this spec provides for screens elsewhere: `serve` passes `web.FS()` as
`api.Options.UI` so the shell is served (api.md decision 12); the start-up log line and
`bms check` say whether a UI is built and which version, so a maintainer can tell a
stale bundle apart from a bug.

## Behavior

### Decisions settled here, with reasons

1. **`cmd/bms` is a one-liner; `app.Main` owns the command line, configuration loading
   and the exit status.** A `main` that did the work could only be tested as a
   subprocess; `Main(ctx, args, environ, stdout, stderr) int` runs every command in
   process with buffers and a cancelable ctx, so the start, request and stop of the real
   assembly is one ordinary integration test. `cmd/bms/main.go` is exactly: derive ctx
   from `signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)`;
   start a goroutine that calls the returned `stop` once ctx is done, so that a **second
   signal terminates the process with the operating system's default disposition**
   (the operator's escape hatch when a closer hangs; decision 10); call
   `os.Exit(app.Main(ctx, os.Args[1:], os.Environ(), os.Stdout, os.Stderr))`. Nothing
   else lives there. config.md's consumer table says `cmd/bms` calls `Load`; it is
   `app.Main` on `cmd/bms`'s behalf (Proposed change 1).
2. **Commands are a table, and later core specs add theirs through their part.** The
   plan already names `create-superuser` (identity, M1), `export` and `delete` (people,
   M4) and `rotate-secret` (identity, later). A `switch` in `Main` would be edited by
   each of those specs; a table that `declare` appends to is edited by none. The
   built-in five are in the table too, so `bms help` is one loop. Names are unique
   (F28).
3. **Four exit statuses: 0, 1, 2, 3.** Scripts, systemd and compose act on the status,
   so it must mean one thing: `2` is "fix your input" (configuration, an unknown
   command, a registry or routing problem baked into the build), `3` is "run `bms
   migrate` or upgrade the binary" (the one condition an operator's playbook handles
   differently from a crash), `1` is everything else that failed, `0` is success,
   including `serve` stopping after a signal. A clean stop after `SIGTERM` is `0`, not
   `143`: the operator asked for it. A shutdown in which a closer failed or timed out is
   `1`, so an operator sees it in the unit's status. `check` uses the same numbers for
   the same conditions, so `bms check; echo $?` predicts `bms serve`.
4. **`serve` logs to stdout; every other command logs to stderr and writes its output to
   stdout.** D20's "JSON on stdout" is about the server a collector tails. `openapi`
   must emit the document and nothing else on stdout (`make openapi-check` redirects
   it); `check` and `migrate` write a plain-text report that an operator reads in a
   terminal, and interleaving JSON log lines with it would make both unreadable.
   `telemetry.Options.Stdout` is `p.stdout` for `serve` and `p.stderr` for the rest.
   (Open question 2 asks the owner to confirm.)
5. **Each command loads configuration itself; `version`, `help` and `openapi` load
   none.** config.md decision 3 says one `Config`, fully validated, for every command;
   that holds for every command that touches the process's environment. `version` and
   `help` answer with no environment at all, and `openapi` must work on a developer's
   machine and in CI with no `BMS_DB_URL` (web-shell.md Proposed change 4: "no network or
   database I/O"), so it reads nothing from the environment, documents every compiled-in
   module whether or not `BMS_MODULES_DISABLED` would turn it off, and writes a fixed
   `info.version`. The document is therefore a function of the binary alone, which is
   what a committed file must be.
6. **`serve` requires the session key from M0, before `identity` exists.** config.md
   F22b says every command that signs or verifies sessions refuses to run without it.
   Making `serve` require it now rather than in M1 costs nothing (`bms migrate`, which
   generates it, is already required before the first `serve` by D17) and spares an
   operator a surprise at the M1 upgrade. `check` does not require it: it reports "run
   `bms migrate`" instead. (Open question 3.)
7. **`openapi` and `check` build the modules against a database that refuses every
   statement, with the schema check skipped.** The OpenAPI document needs every route
   set, and route sets come from built modules (`Set.Routes()`), so the modules must be
   constructed. module-contract says a `Factory` does no I/O beyond reads and no writes;
   this spec tightens it for the command's sake: a factory **needs no database read**
   (Proposed change 2), and `db.None()` (Proposed change 3) makes that a fact a test
   catches: a factory that reads gets `errs.Unavailable` wrapping `db.ErrNoDatabase` and
   `bms openapi` fails naming the module (F25). The same applies to the core parts'
   `build`. `check` reuses the path to count operations (api.md Proposed change 11) and
   to prove the assembly links; it reports schema state through `Status`, which is why
   `Build` runs with `skipSchema` there. Rejected: carrying routes or fragments in the
   manifest (handlers need services; the fragment alone cannot be checked against routes),
   and opening the real pool for `openapi` (a build step must not need a database).
8. **A missing web build is a warning, a stale one too; a missing or old collector is
   nothing at all.** `web.FS()` returning `NotFound` means a checkout without `pnpm
   build`: `serve` passes `UI: nil`, logs "serving without a UI" once, and the API works
   (web-shell.md Proposed change 4). A `web.Version()` that differs from the binary's is
   a developer's stale `dist/`, logged once. Neither stops a server that can do its
   job. `telemetry.New` never dials (telemetry.md decision 12), so a collector's state
   cannot fail start-up, a request or shutdown (09 §1).
9. **Start order: configuration, telemetry, registry, database, signals, parts, modules,
   HTTP shell, modules started, listener.** Telemetry comes before anything that logs.
   The registry is resolved before the database is opened because a registry problem is
   a build or configuration problem and should not wait on a connect timeout. `Set.Start`
   runs **before** `Listen`: a request that arrives before a module subscribed to its
   signals would publish into the void, and the cost of the other order (a `Stop` cycle
   when the port is in use) is paid only on failure. The runnables (jobs) start after
   `Set.Start`, because modules register their jobs in `Start`. The full sequence is
   "`serve`, step by step".
10. **Stop order: HTTP, runnables, signals, modules, database, telemetry, all within
    `http.shutdown_timeout`, HTTP under two thirds of it.** The order the Approved specs
    agreed (signals.md Proposed change 5; api.md Proposed change 11; db.md `Close`;
    telemetry.md `Close`) with one insertion: the runnables (jobs, M4) stop after the
    HTTP server drained and **before** `signals.Close`, so a job that finishes during the
    drain can still publish and commit, and after `signals.Close` nothing new is
    delivered to modules that are about to stop (Proposed change 5). HTTP gets
    `max(1 s, 2 × budget / 3)` (10 s of the default 15 s) so that when `api.Shutdown`
    returns `Timeout` the five closers behind it still have a third of the budget; every
    later closer gets the shared deadline `start + budget` and is called even when that
    deadline has passed (each is idempotent and returns at once on a done ctx), so no
    step is skipped and every failure is logged. Handlers that ignored their ctx may
    still be alive during those closers (api.md "Concurrency and lifecycle"): `Set.Stop`
    and `db.Close` are specified to tolerate them, and process exit ends them. The
    shutdown ctx is `context.WithoutCancel(ctx)` plus the deadline: the signal that
    started the shutdown must not cancel it. A second signal is the escape hatch
    (decision 1).
11. **`serve` starts on a read-only copy; `migrate` refuses one.** 09's ground rules
    make every instance able to be a read-only replica. Every start-up step is a read:
    `CheckSchema` reads, factories read nothing (decision 7), `Start` never writes
    (module-contract decision 14), `Listen` touches no database. `migrate` is the one
    command that writes by definition, and `db.Migrate` refuses a read-only database
    before applying anything (db.md F36); `app` turns that into "run `bms migrate` on the
    copy that writes" and `ExitFailure`. `check` reports the mode. The e2e test proves
    `serve` on a database frozen with `default_transaction_read_only = on`.
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
13. **The e2e start and stop test runs the real assembly in process through `Main`, and
    one subprocess test proves the signal path.** In-process is where the order of
    closers, the exit status and the log lines can be asserted without parsing a child's
    output; the signal path (`signal.NotifyContext`, the second-signal escape) lives in
    `cmd/bms` and is proven once by running the built binary, sending `SIGTERM`, and
    reading its status and output (test plan). The in-process test needs a free port:
    config's `ListenAddr` refuses port `0`, so the test reserves a port by binding
    `127.0.0.1:0`, closing it, and passing the number; `Listen` failing with
    `Unavailable` (the port was taken in between) retries with a new port, at most three
    times.
14. **`migrate` creates the data directory and the secrets file before it touches the
    database, then reloads configuration.** config.md decision 7 (owner, 2026-10-16): the
    key is generated at installation, in a file outside the database, with a notice to
    back it up. Doing it first means an installation whose database is misconfigured
    still gets its key, and the reload means the rest of `migrate` (and `identity`'s
    later use of the key in the same command, if any) sees the same `Config` any other
    command would. `Ensure` runs only when `cfg.Session.Key` is unset: a platform that
    injects `BMS_SESSION_KEY` never gets a file, so config.md F27 cannot trigger.
15. **The module list is one file, `internal/app/modules.go`, and its web mirror is
    `web/src/modules/index.ts`.** D5 and D31 promised one import line each. A test pins
    that every registered manifest has a `web/src/modules/<name>/messages/en.json`
    (decision 16), so the two lists cannot drift silently; a Go module without screens
    still ships its catalogue because the admin "modules" screen shows its title
    (Proposed change 6).
16. **The message-catalogue test lives here and requires `en.json` for every registered
    module.** web-shell.md decision 8 and its Proposed change 4(d): the keys
    module-contract decision 8 derives from a manifest can only be checked where every
    module is imported, which is this package. The test reads
    `web/src/modules/<name>/messages/en.json` relative to the repository root (found by
    walking up from the test's working directory to `go.mod`) and is skipped only when
    `web/src` is absent; a missing file or key fails it, naming module and key.

### `serve`, step by step

1. `config.Load(ctx, environ)` (F3). `Version()` (F2). `cfg.Session.Key.IsSet()` or F6.
2. `telemetry.New(ctx, Options{Level: cfg.Log.Level, Format: cfg.Log.Format, Instance:
   cfg.Instance.Name, Version: version, Stdout: p.stdout, Clock: p.clock, Environ:
   p.lookup})` (F4). From here every problem is also logged. Log at info "bms starting"
   with `version`, `instance`, `go_version`, `exporting`, and `slog.Any("config", cfg)`.
3. Derive `Enabled` (config.md "`modules.disabled`": names from
   `p.registry.Manifests()`, unknown names → F5 under `BMS_MODULES_DISABLED`, all listed).
4. Run every part's `declare` (F28). `p.registry.Resolve(module.Env{Enabled,
   CoreSignals: a.coreSignals, CoreExtensionPoints: a.corePoints})` (F7); log each
   `Warning` at warn with `module` and `message`.
5. `db.Open(ctx, db.Options{URL: cfg.DB.URL, MaxConns, ConnectTimeout, QueryTimeout,
   Tracer: tel.Tracer("db"), Meter: tel.Meter("db")}, p.clock, tel.Logger().With(
   "component", "db"))` (F8). `db.NewMigrator(pool, logger)`. Log `read_only` from
   `pool.ReadOnly()`.
6. `signals.New(signals.Options{Logger: ...With("component", "signals"), Tracer:
   tel.Tracer("signals"), Meter: tel.Meter("signals"), Clock: p.clock, HandlerTimeout:
   5 * time.Second, Deferrer: nil /* jobs, M4, set by its part before this call */})`.
7. Run every part's `build` (F28); the registry's and the parts' permission
   declarations are gathered as `corePerms` with `api.PermissionModulesRead` first.
8. `a.plan.Build(ctx /* buildTimeout */, migrator, a.provision)` (F9). Run every part's
   `link` (people takes `set.PersonData()`).
9. `web.FS()`: ok → `a.ui`; `NotFound` → nil and warn "serving without a UI" (F17).
   `web.Version()`: compare with `version`; warn once when missing, invalid or
   different, only when a UI is present (F18).
10. `intake.New(intake.Options{Logger: ...With("component", "intake"), Meter:
    tel.Meter("intake"), Clock: p.clock})`. `api.New(api.Options{Addr, RequestTimeout,
    MaxBodyBytes, TrustedProxies, AllowPlaintext from cfg.HTTP; Logger:
    ...With("component", "api"); Tracer: tel.Tracer("api"); Meter: tel.Meter("api");
    Clock; Version; Routes: append(set.Routes(), a.routeSets...); Database: pool; Intake;
    Graph: plan.Graph(); Schema: a status func over migrator.Status with the core sets
    and the module sets; Authenticator: a.authenticator; Permissions: a.permissions; UI:
    a.ui})` (F10).
11. `set.Start(ctx /* startTimeout */)` (F11); then each runnable's `start` in order
    (F11; on failure the started runnables are stopped in reverse and `set.Stop` runs).
12. `srv.Listen(ctx /* listenTimeout */)` (F12). Log at info "listening" with `addr` (the
    bound address), `modules` (enabled names), `ui`, `ui_version`.
13. `go srv.Serve()` into a channel. Wait for the first of: `ctx.Done()` (a signal) or
    the channel (F13). Log at info "stopping" with `reason` = `signal` or `serve_error`.
14. `shutdown(ctx, logger, clock, cfg.HTTP.ShutdownTimeout, []closer{http (srv.Shutdown),
    runnables in reverse, signals (disp.Close), modules (set.Stop), db (pool.Close),
    telemetry (tel.Close)})` (F15, F16). Log at info "stopped" with `duration_ms` (before
    `tel.Close`; records logged after it still reach stdout, telemetry.md decision 16).
15. Return `ExitOK` when `Serve` returned nil and every closer returned nil; else
    `ExitFailure`.

A failure at any step before 13 stops what was started, in reverse, with a ctx of
`startTimeout`, runs the closers that apply (telemetry last), prints the problem to
stderr once (and logs it when telemetry exists), and returns the status the failure-mode
table gives. `ctx` ending during start-up is F14.

### `migrate`, step by step

1. `config.Load` (F3); `Version()` (F2).
2. `os.MkdirAll(cfg.Data.Dir, 0o700)` (F23). If `!cfg.Session.Key.IsSet()`:
   `secrets.Ensure(ctx, cfg.Secrets.File, []string{config.SecretsFieldSessionKey})` (F22);
   for each created name print config.md decision 7's notice to stderr, framed by a blank
   line before and after: "Created `<path>`. It holds this installation's session key.
   Back it up separately from the database and copy it to every copy of this
   installation; without it, every session is invalid and anything sealed with it is
   unreadable. Keep it readable by the bms user only."; then `config.Load` again (F3).
3. `telemetry.New` with `Stdout: p.stderr` (F4); log the configuration at debug.
4. Steps 3 and 4 of `serve` (enablement, `declare`, `Resolve`, warnings).
5. `db.Open` with the tracer and meter, `NewMigrator` (F8).
6. `sets` = `a.coreSets` in part order, then for each manifest in `plan.Order()` with
   `Migrations != nil`: `db.Set{Name: m.Name, Schema: m.Name, Files: m.Migrations}`. The
   `db` bootstrap set is the migrator's own.
7. `migrator.Migrate(ctx /* migrateTimeout */, sets)` (F19–F21). Print one line per
   `Applied` to stdout: `applied <set> <NNNN_name> (<duration>)`, then `applied <n>
   migration(s)` or `nothing to apply`.
8. `pool.Close`, `tel.Close` (each under `checkTimeout`); return `ExitOK`.

`migrate` honours `ctx`: a signal during a run cancels the current file, which `db` rolls
back (db.md F42); the message says to rerun; `ExitFailure`.

### `check`, step by step

`check` never stops at the first problem: each section prints its result or its problem,
and the exit status is the worst one found (`ExitUsage` > `ExitSchema` > `ExitFailure` >
`ExitOK` in that order of precedence, since a configuration problem must be fixed first).

1. `config.Load`. On failure print `<Env>: <Reason>` per field, the heading
   `configuration: <n> problem(s)`, and return `ExitUsage` (nothing else can run).
   Otherwise print `configuration` and one line per `config.Keys()` entry:
   `<Env> = <value>` with a secret as `[redacted]` or `[unset]`, an unset `Generated` key
   marked `run bms migrate`, then `secrets file: <path> (present | absent)`.
2. `telemetry.New` with `Stdout: p.stderr` (on failure print `<VAR>: <reason>`, status
   `ExitUsage`, and continue without logs where possible: nothing below logs). Print
   `log: <format> <level>`, `OTLP export: on | off`. Print `PG* variables set: <names>`
   or `none` (names only, db.md decision 14).
3. Enablement, `declare`, `Resolve`. Print `modules`, one line per registered manifest:
   `<name>  enabled | disabled  schema yes | no`; print each warning; on failure print
   each problem and record `ExitUsage`.
4. `db.Open` (under `cfg.DB.ConnectTimeout`): on failure print `database: <kind>:
   <reason without the URL>` and record `ExitUsage` for `Invalid`, `ExitFailure`
   otherwise; skip step 5. On success `Ping` (under `checkTimeout`) and print `database:
   ok, read-write | read-only, pool max <n>`.
5. `migrator.Status(ctx, sets)` over the sets of `migrate` step 6 (every registered
   module with migrations, enabled or not, so an operator sees what a disabled module left
   behind): print `schema` and one line per set: `<set>  built <b>  applied <a>  current |
   <Status.Err text>`; any `Err` records `ExitSchema` (its kind `Conflict`) or
   `ExitFailure` (`Unavailable`, `Timeout`).
6. The parts' `build`, `Build` with `skipSchema` and `db.None()`, `link`, `api.New` with
   the stub `Database`, an empty `SchemaStatus`, `UI: nil` (decision 7): print `api: <n>
   operations` (counted over the document's `paths`), or the problem with `ExitUsage`
   (a routing or fragment problem is the build's) or `ExitFailure` (F25).
7. `web.FS()` and `web.Version()`: print `ui: built, version <v>` or `ui: not built` or
   `ui: built, version unknown`.
8. Close the pool and telemetry. Print `result: ok` or `result: <n> problem(s)` and return
   the recorded status.

### `openapi`, step by step

1. `Version()` (F2) for the start-up line only. `telemetry.New` with the fixed options of
   "Settings" (`Environ` returns nothing). No `config.Load` (decision 5).
2. `Enabled` = every registered name; `declare`; `Resolve` (F7, printed to stderr,
   `ExitUsage`). `signals.New` as in `serve`. `a.dbc = db.None()`. The parts' `build`;
   `Build` with `skipSchema`; `link` (F25, F9 for a factory error).
3. `api.New` with the options of `serve` except `Database`: the stub; `Schema`: returns
   `nil, nil`; `Authenticator`, `Permissions`: nil; `UI`: nil; `Version`: `dev` (F10).
4. Write `srv.OpenAPI()` to stdout, followed by one newline; `ExitOK`.

### `version` and `help`

`bms version` prints `bms <version> (<runtime.Version()> <GOOS>/<GOARCH>)` and returns
`ExitOK`; F2 applies. `bms help`, `bms`, `bms -h`, `bms --help` print the usage to stdout
(`ExitOK`): one line `usage: bms <command>`, one line per command in table order (`name`,
padded, then `summary`, then `usage` when present), then "Configuration comes from `BMS_*`
environment variables; run `bms check` to see it." `bms <command> -h` prints that command's
line alone. An unknown command prints `unknown command "<name>"` and the usage to stderr
and returns `ExitUsage` (F1). A command given arguments it does not take, or missing ones
it needs, is F1 too.

### How later core services plug in

Each row is one `part` in `coreParts` (`internal/app/core.go`), in this order, defined in
`internal/app/core_<name>.go` when its spec is Approved. The order is a dependency order:
a later part may read what an earlier one set on the `assembly`.

| Order | Part | `declare` | `build` | `link` | Runnable | Deps field |
|---|---|---|---|---|---|---|
| 1 | `identity` (M1) | migration set `identity`; commands `create-superuser`, `rotate-secret`; core signals it publishes | `a.authenticator`; route set `accounts`; needs `cfg.Session.Key` | — | — | — |
| 2 | `audit` (M1) | migration set `audit` | the recorder over `cfg.Data.Dir` (spool) and `cfg.Instance.Name`; `a.deps.Audit` | — | the spool importer, if its spec makes it one | `Audit` |
| 3 | `settings` (M2) | migration set `settings`; route set declared as `settings` | the store; `Plan.Settings()`; `a.deps.Settings` (scoped per module in `provision`); the public settings and `theme.css` routes | — | — | `Settings` |
| 4 | `authz` (M2) | migration set `authz`; `corePerms` of its own | the `Authorizer` over `a.corePerms` + `Plan.Permissions()`; `a.permissions`; `a.deps.Authz`; route set `access` | — | — | `Authz` |
| 5 | `people` (M2) | migration set `people`; core signals `people.person-created`, `people.person-deleted`; extension point `people.person-page`; commands `export`, `delete` | the directory; `a.deps.People`; route set `people`; its signals scope | takes `a.set.PersonData()`; contributions to `people.person-page` (Proposed change 2(a)) | — | `People` |
| 6 | `departments-events` (M3) | migration sets `departments`, `events`; its signals; point `events.event-created` | route sets `departments`, `events`; its scopes | contributions to `events.event-created` | — | — |
| 7 | `jobs` (M4) | migration set `jobs` | the runner over the pool and `ReadOnly()`; `a.deps.Jobs` (scoped); it is the `Deferrer`: `signals.New` is called after `build` of this part with `Deferrer` set, and the runner receives the `Dispatcher` for `Redeliver` through the setter `jobs.md` defines (Proposed change 8) | — | `start` after `Set.Start`; `stop` after HTTP drained, before `signals.Close` | `Jobs` |
| 8 | `notify` (M4) | migration set `notify` (the queue) | the notifier over `settings` (SMTP) and `jobs`; `a.deps.Notify` (scoped) | — | — | `Notify` |
| 9 | `files` (Later) | migration set `files` | the store over `cfg.Data.Dir` or S3-compatible options; `a.deps.Files` | — | — | `Files` |

Rules every part keeps: `declare` does no I/O; `build` does no I/O beyond reads and
succeeds against `db.None()` (decision 7) and on a read-only copy; a part's signals scope
comes from `a.disp.Scope(<area>, publishes, subscribes)`; a route set's `Module` is the
area name, never `core`, `modules` or `admin` (api.md F10); a part that needs a
configuration key proposes it to `config.md` and reads it from `a.cfg`. Because `jobs`
must exist before `signals.New` and the dispatcher must exist before `provision`, `serve`
step 6 is placed after the parts whose `build` the dispatcher does not need; in M0–M3 the
distinction is moot and the implementation may call `build` for parts 1–6, then
`signals.New`, then `build` for parts 7–9. `TestParts_OrderAndNames_Pinned` pins the
order and that every name is a reserved core area name.

### `provision`, step by step

1. `Logger = a.tel.Logger().With("module", m.Name)`.
2. `Tracer, err = a.tel.Tracer("module." + m.Name)` (F29; cannot fail for a validated
   name, checked anyway).
3. `Clock = a.clock`; `DB = a.dbc`.
4. `Signals, err = a.disp.Scope(m.Name, m.Publishes, m.Subscribes)` (F29).
5. The `coreDeps` fields, scoped where their spec says (`Settings`, `Jobs`, `Notify` by
   module name). `Links` is left zero: `Build` sets it.

### The Makefile

Each target is one check so `check-runner` runs them one at a time (08-agent-workflow).
Existing targets keep their meaning; `VERSION ?= $(shell git describe --tags --always
--dirty 2>/dev/null || echo dev)` is shared by `bin` and `web-build` (web-shell.md
decision 20). The listing below is what the Makefile becomes (Proposed change 12).

| Target | Does | Needs |
|---|---|---|
| `fmt`, `vet`, `build`, `test`, `lint` | as today (`build` stays `go build ./...`: compiling needs no Node, since `web/dist/.gitkeep` makes the embed compile; web-shell.md Proposed change 9) | Go |
| `test-integration` | `go test -race -tags integration ./...` | `make up` and `.env` loaded |
| `imports` | `scripts/check-module-imports.sh` and `scripts/check-web-module-imports.sh` | Go; shell |
| `web-check` | `pnpm -C web check` | Node LTS, pnpm |
| `web-build` | `BMS_VERSION=$(VERSION) pnpm -C web build` (writes `web/dist/`, including `version.txt`) | Node LTS, pnpm |
| `openapi-check` | `go run ./cmd/bms openapi > web/openapi.json && pnpm -C web generate:api && git diff --exit-code web/openapi.json web/src/api/schema.d.ts` | Go, Node |
| `bin` | `web-build`, then `go build -ldflags "-X github.com/zedgama3/bms/internal/app.version=$(VERSION)" -o bin/bms ./cmd/bms` (the release binary, UI inside) | Go, Node |
| `run` | `set -a; . deploy/compose/.env; set +a; go run ./cmd/bms serve` (development; config.md decision 1) | `.env`, `make up` |
| `migrate` | the same with `migrate` | same |
| `test-e2e` | `bin`, then `scripts/e2e.sh`: load `.env`, `bin/bms migrate`, start `bin/bms serve` on a free port, wait for `/readyz`, `pnpm -C web test:e2e` with `BMS_E2E_BASE_URL`, stop the server with `SIGTERM`, fail if its status is not 0 | `make up`, Node, Playwright browsers |
| `check` | `fmt vet build test lint imports web-check openapi-check` | Go, Node |
| `check-go` | `fmt vet build test lint imports` (the Go-only subset, for a machine without Node) | Go |
| `up`, `observe`, `down`, `ps` | as today | Docker |

### Concurrency and lifecycle

`Main` runs one command on the calling goroutine. `serve` starts exactly one goroutine
of its own (the one running `srv.Serve`) and waits on it; every other goroutine belongs
to a component that documents its exit (`db`'s watcher, the SDK's exporters, the
modules'), and `shutdown` waits for each through its `Close` or `Stop` within the budget.
`assembly` is built on one goroutine and read concurrently only through the components it
holds, each of which is safe for concurrent use by its own spec. `secrets.Ensure` is not
safe for concurrent use on one path from two processes: two `bms migrate` runs at once
are serialised by `db`'s advisory lock only after this step, so the second may see the
file the first created (`O_EXCL` makes it reread rather than overwrite) and prints no
notice; a test covers the sequence, not the race. After `Main` returns nothing it started
is running, except handlers that outlived `api.Shutdown`'s deadline, which the process's
exit ends (decision 10).

### Imports

`app`: standard library (`context`, `errors`, `fmt`, `io`, `io/fs`, `log/slog`, `net`,
`os`, `path/filepath`, `runtime`, `runtime/debug`, `strings`, `time`), `internal/config`,
`internal/lib/*`, `internal/model`, `internal/core/module`, `internal/core/db`,
`internal/core/telemetry`, `internal/core/telemetry/intake`, `internal/core/signals`,
`internal/core/authz` (`PermissionDecl`), `internal/api`, `web`, `internal/app/secrets`,
and in `modules.go` every shipped module as a blank import (`_ ".../internal/modules/
hello"`). Never pgx (depguard `storage-seam`: that is why `db.None()` must live in `db`),
never `go.opentelemetry.io`, never `dbtest` or `telemetrytest` outside test files.
`secrets`: standard library (`context`, `crypto/rand`, `encoding/base64`, `encoding/json`,
`errors`, `fmt`, `io`, `os`, `path/filepath`), `internal/lib/errs`, `internal/config`
(constants only). `cmd/bms`: `context`, `os`, `os/signal`, `syscall`, `internal/app`.
`forbidigo` bans `fmt.Print*`; command output goes through `fmt.Fprint*` on the writers
`Main` was given, which it does not ban.

## Failure modes

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F1 | `Main` | unknown command; wrong arguments for a command | none: usage on stderr, `ExitUsage` (`help` and no command: usage on stdout, `ExitOK`) | fix the command line | `TestMain_UnknownCommand_UsageExit2`, `TestMain_NoArgsAndHelp_UsageExit0`, `TestMain_ExtraArgs_Exit2` |
| F2 | `Version` | the injected version is not a token or over 64 bytes | Invalid; `Main` prints it and returns `ExitUsage` | fix the build flags | `TestVersion_Table` (injected, build info, `dev`; an invalid one) |
| F3 | every command but `version`, `help`, `openapi` | `config.Load` fails | its kind (Invalid); `<Env>: <Reason>` per field on stderr; `ExitUsage` | fix the variables | `TestMain_ConfigInvalid_PrintsEveryFieldExit2` (three problems → three lines, no secret echoed) |
| F4 | same, after `Load` | `telemetry.New` fails (an `OTEL_*` variable) | Invalid; `<VARIABLE>: <reason>` on stderr; `ExitUsage` | fix the variable | `TestMain_BadOTELVariable_Exit2` |
| F5 | `serve`, `migrate`, `check` | a `modules.disabled` entry is not registered (config.md F22) | Invalid under `BMS_MODULES_DISABLED`, listing the compiled-in names; `ExitUsage` | fix the name | `TestEnabledModules_UnknownDisabledName_ReturnsInvalid` |
| F6 | `serve` (later `create-superuser`) | `cfg.Session.Key` unset (config.md F22b) | Invalid under `BMS_SESSION_KEY`: "no session key: run `bms migrate` on a new installation, or restore `<secrets.file>` from your backup"; `ExitUsage` | run migrate or restore the file | `TestCommand_SessionKeyMissing_ReturnsInvalidNamingSecretsFile` |
| F7 | `serve`, `migrate`, `check`, `openapi` | `Resolve` fails (module-contract F14–F22) | its kind; every problem printed (`validate.Fields` lines where present, else the message); `ExitUsage` | fix the build or `modules.disabled` | `TestServe_ResolveFails_PrintsAllExit2` (a fresh registry with a required capability missing and a bad manifest) |
| F8 | `serve`, `migrate`, `check` | `db.Open` fails | Invalid (credentials, old server, driver rejects the URL) → `ExitUsage`; Unavailable (unreachable within `connect_timeout`) → `ExitFailure`; message never carries the URL | fix config / start the database | `TestServe_DBUnreachable_Exit1WithoutURL` (closed port, fake clock drives `db`'s retries), `TestServe_DBInvalid_Exit2` (integration, bad password) |
| F9 | `serve`, `openapi`, `check` | `Plan.Build` fails | `Conflict` from `CheckSchema` (every out-of-date module listed) → `ExitSchema`; Invalid (a module's declarations, `provision`) → `ExitUsage`; Unavailable/Timeout → `ExitFailure`; Internal → `ExitFailure`; the database and telemetry are closed first | run `bms migrate` / fix the module | `TestServe_SchemaOutOfDate_Exit3NamesEveryModule` (integration: empty database), `TestServe_FactoryFails_Exit1` |
| F10 | `serve`, `openapi`, `check` | `api.New` fails (route conflict, fragment mismatch, duplicate schema, bad options) | Invalid or Conflict → `ExitUsage`; the message names the set | fix the module or the core set | `TestServe_APINewFails_Exit2` (two test modules with conflicting patterns) |
| F11 | `serve` | `Set.Start` or a runnable's `start` fails | its kind; what started is stopped in reverse (module-contract F37 for modules); closers run; `ExitFailure` | fix the module / dependency | `TestServe_ModuleStartFails_StopsAndExit1` |
| F12 | `serve` | `Listen` fails | Unavailable (in use) → `ExitFailure`; Invalid → `ExitUsage`; modules stopped, closers run | free the port | `TestServe_AddrInUse_Exit1AfterStoppingModules` (a listener held by the test) |
| F13 | `serve` | `Serve` returns an error (the accept loop failed) | Unavailable; logged at error; the full `shutdown` runs; `ExitFailure` | restart | `TestServe_ServeFails_RunsShutdownExit1` (an injectable server seam in an internal test) |
| F14 | `serve`, `migrate` | `ctx` ends during start-up or during a migration | Canceled; what started is stopped; "interrupted" on stderr; `ExitFailure` (a migration's current file is rolled back by `db`; "rerun") | rerun | `TestServe_InterruptedDuringStart_StopsStartedExit1`, `TestMigrate_Interrupted_Exit1` (integration) |
| F15 | `shutdown` | a closer fails or does not finish within its deadline | Timeout or its kind; the next closers still run (each with the shared deadline, even when passed); errors joined; each logged with `name`, `duration_ms`, `error.kind`; `ExitFailure` | read the log; a second signal forces exit | `TestShutdown_ContinuesPastFailuresAndJoins`, `TestShutdown_OrderAndDeadlines` (fake clock; HTTP's deadline is `max(1s, 2b/3)`, the others' is `start+b`; order http, runnables, signals, modules, db, telemetry) |
| F16 | `shutdown` | `api.Shutdown` times out with handlers alive | Timeout from HTTP; the remaining closers get at least a third of the budget; handlers are left to die with the process | — | `TestShutdown_HTTPTimeout_RestStillRunWithinBudget` (a handler blocked on a channel; `Set.Stop` and `db.Close` are fakes asserting their deadline) |
| F17 | `serve` | `web.FS()` fails with NotFound | none: `UI: nil`, one warn "serving without a UI"; non-API paths answer 404 | run `make web-build` | `TestServe_NoUIBuild_WarnsAndServesAPI` (an injectable `fs.FS` seam; the real `web.FS` is tested by web-shell) |
| F18 | `serve` | `web.Version()` missing, invalid or different from the binary's | none: one warn with `ui_version` and `version` | rebuild the UI | `TestServe_UIVersionMismatch_WarnsOnce` |
| F19 | `migrate` | the database is read-only (db.md F36) | Unavailable wrapping `db.ErrReadOnly`; "run `bms migrate` on the copy that writes"; nothing applied; `ExitFailure` | run it on the writer | `TestMigrate_ReadOnly_Exit1NothingApplied` (integration) |
| F20 | `migrate` | another run holds the lock; an applied file changed; the database is newer than the binary (db.md F37–F39) | Conflict; nothing applied; `ExitSchema` | wait and rerun / restore the file / upgrade | `TestMigrate_Conflict_Exit3` (integration, a changed file) |
| F21 | `migrate` | a migration file fails (db.md F40) | `db.Wrap`'s kind; the message names set and file; earlier files stay; `ExitFailure` | fix the migration, rerun | `TestMigrate_FailingFile_Exit1NamesFile` (integration, a test set with a bad statement) |
| F22 | `migrate` → `secrets.Ensure` | the directory or file cannot be created or replaced; an existing file is malformed or not regular | Invalid under `BMS_SECRETS_FILE` (path, never content); `ExitUsage`; the database is not touched | fix the path or permissions | `TestMigrate_SecretsDirUnwritable_Exit2`, `TestEnsure_DirectoryUnwritable_ReturnsInvalid`, `TestEnsure_MalformedExisting_ReturnsInvalidWithoutEcho` |
| F23 | `migrate` | `cfg.Data.Dir` cannot be created | Invalid under `BMS_DATA_DIR`; `ExitUsage` | fix the path | `TestMigrate_DataDirUnwritable_Exit2` |
| F24 | `check` | the database is unreachable; the schema is behind or ahead; configuration invalid | every section still printed; status by precedence: `ExitUsage` > `ExitSchema` > `ExitFailure` | act on the first problem printed | `TestCheck_ConfigInvalid_Exit2`, `TestCheck_DBUnreachable_ReportsAndExit1`, `TestCheck_SchemaBehind_Exit3` (integration), `TestCheck_AllGood_Exit0PrintsEverySection` (integration) |
| F25 | `openapi`, `check` | a module's factory or a part's `build` reads the database (gets `db.ErrNoDatabase`) | Unavailable wrapping `db.ErrNoDatabase`, wrapped with the module's name; `ExitFailure` | move the read to `Start` or a request | `TestOpenAPI_FactoryReads_Exit1NamesModule` (a test module whose factory queries) |
| F26 | `secrets.Ensure` | `crypto/rand` fails | Internal | retry; a broken host | `TestEnsure_RandFails_ReturnsInternal` (an injectable reader in an internal test) |
| F27 | `cmd/bms` | a second `SIGINT`/`SIGTERM` after the first | none: the default disposition terminates the process; the status is the operating system's | — | `TestBinary_SecondSignal_Terminates` (subprocess; a test module whose `Stop` blocks) |
| F28 | `Main` (the parts) | two parts or two commands share a name; a part's name is not a reserved core area name | Internal (a programming error); `ExitFailure` | fix `core.go` | `TestParts_OrderAndNames_Pinned`, `TestCommands_UniqueNames` |
| F29 | `provision` | `Tracer` or `Scope` refuses (a name the registry already validated) | the refuser's kind, wrapped with the module name (module-contract F24) | fix app | `TestProvision_ScopeConflict_ReturnsKind` (a dispatcher already holding the scope) |
| F30 | `TestMessageCatalogues_EnglishCoversManifests` | a registered module lacks `web/src/modules/<name>/messages/en.json`, or the file lacks a key module-contract decision 8 derives (`module.<name>.title`, `.description`, `.permission.<rest>.title`/`.description`, `.setting.<key>.title`/`.description`/`.option.<opt>`, `.signal.<rest>.title` for each published name) | a test failure naming module and key (not a runtime path); skipped when `web/src` is absent | add the key | the test itself; `TestMessageCatalogues_FixtureMissingKey_Fails` over a fixture |

Behaviours that are not failures but are proven: `serve` starts on a read-only database
(`TestServe_ReadOnlyDatabase_StartsAndReadyzSaysReadOnly`, integration); a down or absent
collector changes nothing (`TestServe_NoCollector_StartsServesStops`; the e2e test runs
with no `OTEL_*` set); `migrate` on an up-to-date database applies nothing and prints
`nothing to apply` (`TestMigrate_Idempotent_PrintsNothingToApply`, integration); a second
`migrate` leaves the secrets file byte-identical and prints no notice
(`TestMigrate_CreatesSecretsFileOnceAndLoadReadsIt`, integration); `Ensure` never
overwrites (`TestEnsure_ExistingValueNeverOverwritten`), adds a missing field and keeps
unknown ones (`TestEnsure_AddsMissingFieldPreservingOthers`), creates `0600` with a
64-byte key and a `0700` directory (`TestEnsure_CreatesFile0600WithKey`).

Members that cannot fail, and why: `Exit*` (constants); `skipSchema.CheckSchema` (returns
nil); the stub `api.Database` (`Ping` nil, `ReadOnly` false). Each has a success-path
test.

## Lock-in check

No external technology. Signals are POSIX (`SIGINT`, `SIGTERM`); on Windows `os.Interrupt`
alone applies and `SIGTERM` is not delivered, which `cmd/bms` tolerates (the
`signal.NotifyContext` call lists both; an unknown signal on a platform is ignored). The
Makefile needs GNU or BSD `make`, a POSIX shell, Go, Node LTS and pnpm (web-shell.md:
every piece OSI-licensed). Nothing here depends on PostgreSQL, OpenTelemetry or a hosted
service beyond what the specs it assembles already proved portable.

## Test plan

**Unit** (no infrastructure, `go test -race`, packages `app` (internal tests for the
seams: a fresh `module.Registry` with invented modules `alpha`, `beta`, a recording
`process` with buffers, fake closers, `clock.NewFake`; `telemetrytest.New(t)` where a
logger is injected) and `secrets_test` (`t.TempDir()`); markers `ZQ9MARKER7` in every
database URL and session key and `!strings.Contains(stdout+stderr, marker)` on every
test; invented module names, no people):

- Commands: F1, F2, F3, F4; `TestExitCodes_Pinned`; `TestHelp_ListsEveryCommandInOrder`;
  `TestVersion_Output`; `TestCommands_UniqueNames`; `TestMain_NeverCallsOSExit` (by
  construction: `Main` returns; the test asserts a return for every command's failure
  path).
- Enablement: F5; `TestEnabledModules_RegistryOrderMinusDisabled`.
- Assembly: F7, F9 (unit half), F10, F25, F28, F29; `TestProvision_ScopesAndLoggerFields`
  (`module=<name>` on the logger, scope `module.<name>` on the tracer, a scope that
  refuses an undeclared publish); `TestAssembly_CorePermsStartWithModulesRead`;
  `TestAssembly_RouteSetsAppendedAfterModules`; `TestOpenAPI_NoEnvironmentNoIO`
  (no `BMS_*` set, no `OTEL_*`, a listener on a closed port never contacted, the document
  parses and lists the test module's routes, `info.version` is `dev`, stdout holds only
  the document); `TestOpenAPI_DisabledModulesStillDocumented`.
- Lifecycle: F11, F12 (a listener held by the test), F13, F14, F15, F16, F17, F18;
  `TestServe_StartOrder` (a recording test module and recording fakes: `Start` before
  `Listen`, runnable `start` after `Set.Start`); `TestShutdown_OrderAndDeadlines`;
  `TestShutdown_DetachedFromCancellation` (the ctx passed to `shutdown` is already
  canceled; every closer still gets its deadline); `TestServe_CancelCtx_StopsCleanlyExit0`
  (a fake `api` seam or a real `api.New` with `Listen` on a reserved port, a real
  `signals.New`, a test module; the log lines `starting`, `listening`, `stopping`,
  `stopped` in order).
- secrets: F22, F26 and the three success tests above; `TestEnsure_EmptyValueIsGenerated`;
  `TestEnsure_RepeatedOrEmptyField_ReturnsInvalid`; `TestEnsure_AtomicReplace_NoTempLeft`;
  `TestEnsure_ContextCanceled_ReturnsCanceled`.
- Catalogues: F30 (the real test over the real tree; a fixture test for the checker).
- Compile-time: `var _ module.SchemaChecker = (*db.Migrator)(nil)` in
  `schema_test.go` (db.md Proposed change 5); `var _ module.SchemaChecker = skipSchema{}`.
- Lint: passes every rule; `app` has no depguard restriction, and the test
  `TestOnlyAppImportsWebAndConfig` is not needed because depguard already denies `web`
  and `config` everywhere else.

**Integration** (`//go:build integration`, `dbtest.TempDB` for a database per test,
`BMS_TEST_DB_DSN` from the compose stack; the `hello` module compiled in through
`modules.go`; environment built as a slice with the temporary database's URL, a 48-byte
marker session key, `BMS_DATA_DIR` in `t.TempDir()`, `BMS_HTTP_ADDR=127.0.0.1:<reserved
port>`, `BMS_LOG_FORMAT=json`, no `OTEL_*`):

- `TestMigrate_CreatesSecretsFileOnceAndLoadReadsIt` (config.md Proposed change 3): with
  no session key in the environment, `migrate` creates `data/secrets.json` 0600 with a
  64-byte key, prints the notice on stderr, `config.Load` then yields the key; a second
  `migrate` leaves the file byte-identical and prints no notice.
- `TestMigrate_EmptyDatabase_AppliesCoreThenModulesInOrder` (the `applied` lines name
  `db` first, then `hello`; `Status` is current after), `TestMigrate_Idempotent_PrintsNothingToApply`,
  F19, F20, F21, F14 (migrate half).
- `TestServe_EndToEnd_StartsServesStops` (decision 13): `migrate` → `serve` in a
  goroutine → poll `GET /readyz` with `clock.Wait` until `200` (bounded by a deadline) →
  `GET /api/modules` lists `hello` enabled → `GET /api/hello/public-greeting` is `200`
  with `X-Trace-Id` → `GET /api/hello/greeting` is `401` (no identity in M0) →
  `GET /healthz` `200` → cancel ctx → `Main` returns `ExitOK` → stdout holds the four
  lifecycle lines in order and the six closers' `stopped` records in the order of
  decision 10, no record carries the markers; the trace of the greeting request has the
  `hello` module's span under the request span (api.md Proposed change 11's assertion
  for `deliver:hello.greeted` runs on the write route the `hello` spec defines, once
  that spec exists; until then the read route's child span is asserted).
- `TestServe_ReadOnlyDatabase_StartsAndReadyzSaysReadOnly` (`ALTER DATABASE ... SET
  default_transaction_read_only = on` after `migrate`, then `serve`; `/readyz` says
  `read-only`; every response carries `X-Read-Only: true`).
- `TestServe_SchemaOutOfDate_Exit3NamesEveryModule` (no `migrate`: the message names
  `hello` and both versions and says to run `bms migrate`).
- `TestServe_NoCollector_StartsServesStops` (the e2e test itself: no endpoint set) and
  `TestServe_CollectorDown_StartsServesStops` (`OTEL_EXPORTER_OTLP_ENDPOINT` on a closed
  port: start, one request and stop all within the budget).
- `TestCheck_AllGood_Exit0PrintsEverySection`, `TestCheck_SchemaBehind_Exit3`,
  `TestCheck_DBUnreachable_ReportsAndExit1`, `TestCheck_NeverPrintsSecrets`.
- `TestBinary_SIGTERM_StopsCleanlyExit0` and F27 (`TestBinary_SecondSignal_Terminates`):
  `go build` the binary into `t.TempDir()` (skipped when `go` is not on the path), run
  `serve` with the environment above, wait for `/readyz`, send `SIGTERM`, assert exit
  status `0` and the `stopped` line; for F27 a build tag `e2ehang` compiles in a test
  module whose `Stop` blocks, two signals are sent, and the status is non-zero within
  `http.shutdown_timeout`.

**End-to-end smoke** (`make test-e2e`): web-shell.md's Playwright scenarios against
`bin/bms serve` with the `hello` module and the production build, at a phone viewport and
a desktop one; `scripts/e2e.sh` owns starting and stopping the server and fails when the
server's exit status is not `0`.

**Conformance:** none of its own. Every module's `moduletest.Conformance` runs in its own
package; `app`'s e2e test is the one place a module is proven inside the real assembly.

## Open questions

1. **A drain delay for probe-based balancers.** api.md notes that `/readyz` answers `503`
   the instant `Shutdown` is called but the listener closes at the same instant, so a
   balancer that polls every few seconds sends a few connections to a closed port during
   a rollout. Closing that gap needs a new configuration key (`http.drain_delay`,
   duration, default `0s`, range `0..30s`) and an `api.Server.BeginDrain()` that flips the
   health routes to `503` while still accepting connections, with `app` waiting the delay
   between the signal and `Shutdown`. **Proposed answer:** not in M0. A regional's
   deployment is one instance behind a tunnel or a reverse proxy on the same host, where
   a restart is a restart; design it with the packaging work (Phase 1) if a regional runs
   two instances behind a probing balancer. Recorded under "Not responsible for" until
   then.
2. **Where the logs of `migrate`, `check` and `openapi` go.** Decision 4 sends them to
   stderr so stdout carries the command's output alone (the OpenAPI document, the check
   report, the applied-migration lines); `serve` keeps stdout per D20. **Proposed
   answer:** yes, stderr for every command but `serve`; D20 gains the clarification
   (Proposed change 13).
3. **Whether `serve` should require the session key before `identity` exists.** Decision
   6 says yes from M0, so a new installation always runs `bms migrate` first and the M1
   upgrade changes nothing an operator sees. The alternative is to add the requirement
   with `identity` in M1. **Proposed answer:** from M0.
4. **Second signal forces termination.** Decision 1 lets a second `SIGINT`/`SIGTERM` kill
   the process at once with the operating system's default status, as the operator's
   escape hatch when a closer hangs past `http.shutdown_timeout`. The alternative is to
   absorb repeated signals and always finish the orderly shutdown. **Proposed answer:**
   the escape hatch; the shutdown budget already bounds the orderly path, and an operator
   who presses twice means it.

## Proposed changes elsewhere

1. **`plan/design/config.md` (row 3):** in `Load`'s consumer wording and "Who needs each
   member", `cmd/bms` becomes "`app.Main` on `cmd/bms`'s behalf, once per command that
   needs configuration (`serve`, `migrate`, `check`, and later `create-superuser`,
   `export`, `delete`); `version`, `help` and `openapi` load none (app.md decision 5)";
   mark Proposed change 3 applied by this spec (F3, F5, F6, F22, the `check` output in
   "`check`, step by step", `secrets.Ensure` with its tests); F22 and F22b's tests are
   named here as `TestEnabledModules_UnknownDisabledName_ReturnsInvalid` and
   `TestCommand_SessionKeyMissing_ReturnsInvalidNamingSecretsFile`. If Open question 1 is
   answered yes later, add `http.drain_delay` (duration, default `0s`, `0..30s`).
2. **`plan/design/module-contract.md` (row 4):** (a) give core definers a way to read
   contributions: `func (s *Set) Contributors(point ExtensionPoint) ([]Contribution,
   error)` for points in `Env.CoreExtensionPoints` (`Invalid` for any other point), since
   `people` and `departments-events` are not modules and hold no `Links`; (b) tighten
   `Factory`: "needs no database read; `bms openapi` and `bms check` build every module
   against a `DB` that refuses every statement (app.md decision 7), so a read at
   construction fails the build step" (also in `add-module` §2 and `go-conventions`);
   (c) record that `app` runs `Build` under 30 s and `Set.Start` under 30 s, and that
   `Set.Stop` shares `http.shutdown_timeout` with the other closers (the "proposed 30 s
   and 15 s" in "Concurrency and lifecycle" and Proposed change 9); mark Proposed change
   9 applied by "`serve`, step by step"; (d) decision 9 gains: "every module ships
   `web/src/modules/<name>/messages/en.json` even without screens, because the admin
   'modules' screen shows its title; `app`'s catalogue test requires it".
3. **`plan/design/db.md` (row 5):** add `func None() DB`: a `DB` with no database behind
   it, for `bms openapi` and `bms check` (app.md decision 7): every `Exec`, `Query`,
   `Row.Scan`, `Rows.Err` and `Transact` fails with `errs.Unavailable` wrapping a new
   sentinel `ErrNoDatabase`; `ReadOnly()` is false; `AfterCommit` outside a transaction
   still runs the hook at once (nothing was written); it must live in `db` because only
   `db` and `postgres/` packages may import pgx (depguard `storage-seam`) and `DB`'s
   methods return pgx types. Mark Proposed change 5 applied by "`serve`, step by step"
   (step 5), "`migrate`, step by step", "`check`, step by step" and the compile-time
   assertion in the test plan.
4. **`plan/design/telemetry.md` (row 15b):** mark Proposed change 5 applied; refinements:
   `Options.Stdout` is `stderr` for every command but `serve` (app.md decision 4);
   `openapi` passes an `Environ` that returns nothing, so a build step never exports;
   `bms check` prints `log: <format> <level>` and `OTLP export: on | off`.
5. **`plan/design/signals.md` (row 10):** mark Proposed change 5 applied with one
   insertion: the runnables (jobs, M4) stop after the HTTP server drained and before
   `signals.Close`, so the order is HTTP → runnables → `signals.Close` → `Set.Stop` →
   `db.Close` → `telemetry.Close` (app.md decision 10).
6. **`plan/design/web-shell.md` (row 17):** mark Proposed change 4 applied: (a) `bms
   openapi` as specified, with `info.version` fixed to `dev` so the committed document
   is stable across commits; (b), (c) as specified (F17, F18); (d)
   `TestMessageCatalogues_EnglishCoversManifests` requires `messages/en.json` for every
   registered module (screens or not) and is skipped only when `web/src` is absent; (e)
   the Makefile targets, with `bin` (not `build`) depending on `web-build`, `check-go`
   as the Go-only subset, and `scripts/e2e.sh` owning the server's start and stop under
   `test-e2e`; (f) confirmed by depguard.
7. **`plan/design/api.md` (row 16):** mark Proposed change 11 applied: the sub-deadline
   is `max(1 s, 2 × shutdown_timeout / 3)`; the drain delay is app.md Open question 1
   (if answered yes, `api` gains `Server.BeginDrain()`: health routes answer `503` while
   the listener stays open); `core.modules.read` is registered through `authz`'s part
   with `corePerms`; `bms check` prints the operation count; the `deliver:hello.greeted`
   assertion runs in app's e2e test once `module-hello.md` defines the write route.
8. **`plan/design/jobs.md` (row 14):** `jobs` is core part 7: its `build` constructs the
   runner before `signals.New` and hands it as `Options.Deferrer`; it then receives the
   `*signals.Dispatcher` for `Redeliver` through a setter (`Runner.SetRedeliverer` or
   equivalent) that `app` calls after `signals.New`; it is a `runnable`: `start` after
   `Set.Start` (modules register jobs in `Start`), `stop` after the HTTP server drained
   and before `signals.Close`; `Deps.Jobs` is scoped per module by `provision`.
9. **`plan/design/identity.md` (row 6):** core part 1: `declare` registers the commands
   `create-superuser` (M1) and `rotate-secret` (later) in app's command table with their
   `usage`, and its migration set; `build` sets `a.authenticator` and the `accounts`
   route set from `cfg.Session.Key`; both commands require the key (app F6) and load
   configuration through `Main` like `serve`.
10. **`plan/design/people.md` (row 8) and `departments-events.md` (row 9):** parts 5 and
    6: `declare` lists their core signals and extension points (`Env.CoreSignals`,
    `Env.CoreExtensionPoints`) and, for `people`, the commands `export <person-id>` and
    `delete <person-id>` (`model.ParseID`; audited by `people`); `link` takes
    `a.set.PersonData()` and reads contributions through `Set.Contributors` (item 2(a)).
11. **`plan/design/authz.md` (row 7):** part 4: `build` receives `a.corePerms`
    (`api.PermissionModulesRead` first, then every part's declarations) and
    `a.plan.Permissions()`, sets `a.permissions` and `a.deps.Authz`.
    **`plan/design/settings.md` (row 15), `audit.md` (row 12), `notify.md` (row 11),
    `files.md` (row 13):** parts 3, 2, 8, 9 as the table "How later core services plug
    in" states; each names its `coreDeps` field and whether `provision` scopes it.
12. **`Makefile` and `scripts/e2e.sh`:** the targets in "The Makefile": add `VERSION`,
    `web-check`, `web-build`, `openapi-check`, `bin`, `run`, `migrate`, `test-e2e`,
    `check-go`; extend `check` and `imports`; add `scripts/e2e.sh`. `.gitignore` already
    ignores `/bin/`.
13. **`plan/03-decisions.md`, D20 (for `plan-keeper`):** add: "The server (`bms serve`)
    logs to stdout; every other command logs to stderr so that stdout carries the
    command's output alone (app.md decision 4; pending the owner's answer to its Open
    question 2)."
14. **`plan/07-abstractions.md`, `app` entry:** replace with: "**Purpose:** run the
    command the operator asked for over the core assembled from its configuration.
    **Owns:** the command table (`serve`, `migrate`, `check`, `openapi`, `version`, `help`;
    later `create-superuser`, `export`, `delete`, `rotate-secret` added by their parts)
    and the exit statuses 0/1/2/3; the assembly (config sections into each package's
    options; telemetry, registry, database, signals, core parts, modules, HTTP shell, in
    that order; `Provision`); the lifecycle (start order; SIGINT/SIGTERM; stop order HTTP
    → runnables → signals → modules → db → telemetry within `http.shutdown_timeout`, HTTP
    under two thirds of it); the module list (`modules.go`); the installer step
    (`secrets.Ensure`, migrations in order); the core-part seam each later core service
    plugs into; the tests only the root can run. **Doesn't do:** anything any assembled
    package does; packaging; configuration reload; a drain delay (open). **Spec:**
    `design/app.md`." Layer rule 5 may add: "`cmd/bms` is a one-liner over `app.Main`."
15. **`plan/09-core-features.md` §1:** "People can" gains `bms version` and `bms openapi`
    (for the web build) beside the four commands; "Done when" gains "every command
    returns one of four documented exit statuses, and `bms check` exits as `bms serve`
    would" and "a second SIGINT or SIGTERM terminates the process at once".
16. **`.claude/skills/add-module/SKILL.md` and `go-conventions`:** the module list line
    is `_ "github.com/zedgama3/bms/internal/modules/<name>"` in `internal/app/modules.go`
    (plus the web line, D31); a `Factory` needs no database read (item 2(b)); every module
    ships `messages/en.json` (item 2(d)).
17. **`docs/operating.md` (when written):** the commands and their exit statuses; `bms
    check` before `bms serve`; signals (`SIGTERM` for an orderly stop, a second signal to
    force; `SIGHUP` does nothing); a systemd unit example with `KillSignal=SIGTERM` and
    `TimeoutStopSec` greater than `BMS_HTTP_SHUTDOWN_TIMEOUT`; that `serve` logs to
    stdout and the other commands to stderr; that `migrate` must run on the writing copy
    and prints the secrets-file notice once; that a build without `web/dist` serves the
    API only.
