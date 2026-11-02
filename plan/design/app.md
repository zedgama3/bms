# app: the composition root

- **Status:** Implemented (2026-10-17); Approved (2026-10-17)
- **Amended 2026-11-01 (boundaries step 13, `boundaries.md` Proposed change 10):** the body is rewritten to describe `app` as it now is: it wires, it never decides. The lazy adapters, the old part table, the superuser alert, the demo adapters and the per-amendment narratives are gone from it; every superseded text is kept verbatim in `plan/design/history/app.md`, and "Amendments" below points there. Decision numbers, F-numbers and test names are kept.
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
  **Also depends on:** `boundaries.md` (Approved): the tenets "App wires, it never decides" and "No hidden two-way dependencies", the build order, the seam table (`07-abstractions.md`, "Late-bound seams").
  **Used by:** `cmd/bms` (`Main`), the operator (the commands), `check-runner` and CI (the
  Makefile targets), `module-hello.md` (its integration test runs through `Main`), every
  core spec (each plugs in through one *part*, see "The build order").

## Summary

**Purpose:** run the command the operator asked for over the core assembled from its configuration. Composition root: `internal/app`, helper `internal/app/secrets`, binary `cmd/bms` (a one-liner over `app.Main`). App wires, it never decides (boundaries tenet): it calls constructors with values from `config`, from other parts' outputs and from the plan; converts shapes (`module.CoreRoutes`); collects declarations; orders the lifecycle; maps errors to exit statuses.

**Exported (`app`):**
- `Main(ctx, args, environ []string, stdout, stderr io.Writer) int` runs one command in process, never panics on operator input, never calls `os.Exit`.
- `Version() (string, error)`: `-ldflags -X ...internal/app.version`, else `debug.ReadBuildInfo`, else `dev`; a telemetry token of at most 64 bytes.
- Exit statuses (pinned, `TestExitCodes_Pinned`): `ExitOK=0` (also `serve` stopped by signal), `ExitFailure=1`, `ExitUsage=2` (bad config, OTEL variable, module name, registry or routing problem, missing session key, also for `check`), `ExitSchema=3` (schema behind or ahead, changed migration file, lock held).
- `secrets.Ensure(ctx, path, fields) (created []string, err)`: creates the secrets file once (0600, dir 0700, 64 random bytes base64 per field), adds missing fields, never overwrites, atomic replace; `secrets.FieldBytes = 64`.
- Unexported, written against by every core spec: `command{name, summary, usage, run}`, `process`, `assembly`, `coreDeps`, `part{name, declare, build, link, seed}`, `runnable{name, start, stop}`, `closer`, `shutdown(...)`, `provision`, `skipSchema`.

**What app owns:** config (`config.Load` once per command, sections copied into each package's options); telemetry, the database pool and migrator, the signals dispatcher (built over `jobs.NewDeferrer`); the Lookups (built before any part); the parts list and build order; route sets (core areas converted with `module.CoreRoutes`); commands and exit statuses; runnables and the shutdown order; the web shell and client-manifest wiring; the module list (`modules.go`); `provision`; the secrets file; the tests only the root can run.

**What app does not do:** parse a setting (each package reads its own area through a `settings.Reader`); hold a rule a burner or operator could name; implement another package's interface except by forwarding to a built service; import a package's store or delivery channel (depguard `app-no-stores`). Late-bound seams: **None.** (`07-abstractions.md`, "Late-bound seams"; `TestSeamTable_MatchesApp` and `TestSeamTable_IsEmpty`).

**Build order** (see "The build order"; `TestParts_OrderAndNames_Pinned`): telemetry, config, db, migrator; `jobs.NewDeferrer`, then `signals.New`; the Lookups (departments, authz, identity, people); then the parts in `coreParts` order: audit, authz, settings, jobs, auditlog, departments, events, files, regional, notify, identity, people, datarights, demo; then the modules (`Build`) and every part's `link`; then api.

**Commands** (exit statuses in "Commands and exit statuses"): `serve`, `migrate`, `check`, `openapi`, `version`, `help`; from parts `create-superuser` (identity), `export`, `delete` (datarights), `files verify` (files), `demo seed|reset` (demo). `check` probes through `notify.Probe`, `demo.StateOf` and `audit.SpoolCounts`; it opens no store.

**Key decisions:**
- D1 `app.Main` owns command line, config loading and exit status; a second SIGINT/SIGTERM terminates immediately with the OS default (owner, 2026-10-17).
- D2 commands are a table that parts' `declare` appends to; names unique.
- D3 exactly four exit statuses; `check` uses the same numbers as `serve` for everything it can observe without starting; what only `serve` does (`Set.Start` on a real database, the files probe, listening) is reported, not scored (Q-JOBS10, D44).
- D4 `serve` logs to stdout; all other commands log to stderr (owner, 2026-10-17; D20 clarification).
- D5 each command loads config itself; `version`, `help`, `openapi` load none.
- D6 `serve` requires the session key from M0 (owner, 2026-10-17).
- D7 `openapi` and `check` build modules against `db.None()` with the schema check skipped; a factory that reads the database fails.
- D8 missing or stale web build and a missing collector are warnings/nothing, never fatal.
- D9 start order: config, telemetry, registry, database, signals, parts, modules, HTTP shell, `Set.Start`, runnables, `Listen`.
- D10 stop order HTTP, runnables, signals, modules, audit, database, telemetry within `http.shutdown_timeout`; HTTP gets `max(1s, 2*budget/3)`; every closer is called even after failure; shutdown ctx is `WithoutCancel`.
- D11 `serve` starts on a read-only copy; `migrate` refuses one.
- D12 core services plug in as ordered parts with `declare`, `build`, `link`, `seed` and runnables, not a registry; the order is the build order. Amended 2026-11-01: no part is late-bound (`lazyFilesStore`, `lazyAuthorizer` and the other adapters are gone, superseded by the Lookups and build-order changes in `boundaries.md`).
- D13 end-to-end test runs the real assembly in process through `Main`, plus one subprocess signal test.
- D14 `migrate` creates data dir and secrets file before touching the database, only when `cfg.Session.Key` is unset, then reloads config.
- D15 the module list is `internal/app/modules.go` (blank imports) mirrored by `web/src/modules/index.ts`.
- D16 `TestMessageCatalogues_EnglishCoversManifests` lives here and requires `messages/en.json` for every registered module.
- D17 no drain delay in M0 (owner, 2026-10-17).
- D18 debug logging is announced (owner, 2026-10-17); scrubbing stays on at every level.
- D19 accepted deviations (reviewed, 2026-10-17) listed under Behavior "Accepted deviations".
- D57 (2026-10-30) `settingsAreas` builds an area for a module that declares settings or terms; the catalogue tests check module terms (F44).
- D58 (2026-10-30) `people.Options.Session` is `identity.SessionFrom`; `TestMessageCatalogues_EnglishCoversJobs` requires the job titles in the English catalogues (F30a).

**Errors callers handle:** `Main` converts everything to exit statuses (F3 config `Invalid` to 2; F4 OTEL `Invalid` to 2; F5 unknown `BMS_MODULES_DISABLED` name to 2; F6 no session key to 2; F7 `Resolve` failure to 2; F8 `db.Open` `Invalid` to 2, `Unavailable` to 1; F9 `Build`: `Conflict` from `CheckSchema` to 3, `Invalid` to 2, others to 1; F10 `api.New` to 2; F11-F13 start/listen/serve failures to 1 after stopping what started; F19 read-only `migrate` to 1; F20 migrate `Conflict` to 3). `Version` and `secrets.Ensure`: `Invalid` (never echoes content), `Internal`, `Canceled`/`Timeout`. F28 duplicate part/command names are `Internal`.

**Owns (config, files, constants, no permissions/signals/settings):** consumes `config.Config` (all sections); writes `cfg.Secrets.File` (default `<data.dir>/secrets.json`) and creates `cfg.Data.Dir` (0700). Part names are checked with `module.IsCoreArea` (F28). Constants (not keys): `buildTimeout` 30s, `startTimeout` 30s, `listenTimeout` 5s, `migrateTimeout` 30m, `checkTimeout` 30s, `signals.HandlerTimeout` 5s, `openapi` `info.version` `dev`. Log lines: "bms starting", the debug-logging warning (D18), "listening", "stopping", "stopped". Makefile targets: `fmt vet build test lint test-integration imports arch-report web-check web-build openapi-check bin run migrate test-e2e check check-go up observe down ps`.

**Expects of dependencies:** config (`Load`, `Keys`, `SecretsFieldSessionKey`); module-contract (`Registry`, `Resolve`, `Plan`, `Provision`, `Deps`, `SchemaChecker`, `Set`, `ModuleRoutes`, `CoreRoutes`); db (`Open`, `Migrator`, `Pool`, `db.None()`); telemetry (`New`, `Close` last, `intake.New`, `intake.NewUsage`); signals (`New` over a `jobs.Deferrer`); every core package's `New` validates its dependencies and does no I/O (D7), its own settings area is read through a `settings.Reader`, and its probes (`notify.Probe`, `demo.StateOf`, `audit.SpoolCounts`) are what `check` calls; api (`New`, `Listen`, `Serve`, `Shutdown`, `OpenAPI`, `RouteTokens`); web (`web.FS`, `web.Version`, `web.ClientManifest`).

**Open questions:** None. Answered by the owner (2026-10-17): no drain delay in M0 (D17); non-`serve` commands log to stderr (D4); `serve` requires the session key from M0 (D6); a second signal terminates immediately (D1).

**Proposed changes elsewhere:** all applied or recorded (see the section); the references in them to "How later core services plug in" mean the table in `plan/design/history/app.md`.

## Purpose

Run the command the operator asked for over the core assembled from its configuration.

## Responsibilities

- **Commands.** Parse the command line, run one command (`serve`, `migrate`, `check`,
  `openapi`, `version`, `help`; later `create-superuser`, `export`, `delete`,
  `rotate-secret`, `files verify` from their specs), write its output to stdout and its problems to
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
  schema state, API, jobs and UI without starting, and exit with the status `serve` would
  reach before it starts the modules (decision 3).
- **The parts and the build order.** Hold the ordered list of core parts (`declare`, `build`,
  `link`, `seed`, runnables) and the order in which the Lookups, parts, modules and api are
  built, so that each core service arrives as one new file and no change to the commands or
  the lifecycle, and nothing takes anything built after it ("The build order").
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
| Sessions, the superuser, `create-superuser`, `rotate-secret`, the superuser alert | `identity` (`identity.md`). The commands are registered through `app`'s command table by `identity`'s part. |
| Permissions and the `Authorizer`; registering the declared permissions | `authz` (`authz.md`). `app` hands its `Lookup` the core's and the modules' declarations (`api.PermissionModulesRead` among them). |
| Export and deletion of one person across modules; the `export` and `delete` commands | `datarights` (`datarights.md`), over `Set.PersonData()`, through its part. |
| Parsing any setting; any rule; the actor order; the delivery and retention rules; the probes' logic | the package that declares the key or the rule (`boundaries.md`, F1). `app` forwards. |
| Any package's store or delivery channel | the package itself; depguard `app-no-stores` denies the import. |
| Background work; running jobs only on the writing copy; retrying signal deliveries | `jobs` (`jobs.md`), a core part with `start` and `stop`. |
| Any module's behaviour; what a module needs from another | the module's spec; contracts and signals (D19). `app` never calls a module beyond the contract. |
| Packaging: the container image, systemd unit, compose file for production, backups, upgrades | `docs/operating.md` and `deploy/` (Phase 1 roadmap item). This spec fixes what they rely on: the commands, exit statuses, signals and the shutdown budget. |
| Reloading configuration on `SIGHUP` | nobody (config.md "Not responsible for": a restart). `SIGHUP` keeps its default disposition. |
| A drain delay between the stop signal and closing the listener, for probe-based balancers | not in M0 (decision 17; api.md "Concurrency and lifecycle" notes the timing). |
| Cross-process coordination between the on-site and off-site copies | the later `switchover` spec (Q-OFFLINE, D27). Every command here works on a read-only copy except `migrate`, which refuses it. |

## Sub-abstractions

| Name | Purpose (one line) | Notes |
|---|---|---|
| `Main` and the command table | Turn a command line into one command run with one exit status. | `cmd/bms` is a one-liner over it; later specs add commands to the table. |
| `serve` | Build, start, serve, stop. | The only command that listens; the only one whose logs go to stdout. |
| `migrate` | Create the secrets file on a new installation and apply migrations in order. | Refuses a read-only copy; 30 min budget. |
| `check` | Report everything `serve` would check, without starting, and exit as `serve` would up to the point where `serve` starts the modules (decision 3). | Continues past problems and reports them all; a module `Start` failure under `db.None()` is printed, not scored (Q-JOBS10). |
| `openapi` | Print the assembled OpenAPI document with no configuration and no I/O. | For `make openapi-check`; builds the modules against a database that refuses every statement. |
| `version`, `help` | Print the build version; print usage. | No configuration, no I/O. |
| Assembly and `provision` | Build the core in order and give each module its scoped `Deps`. | `assembly` is the one struct every part and every command works on. |
| Parts | The seam each core service plugs into: `declare`, `build`, `link`, `seed`, in `coreParts` order. | One file `core_<name>.go` each; "The build order". |
| Runnables | A service's background work, started after `Set.Start` and stopped before `signals.Close`. | `runnable{name, start, stop}`; see "Runnables". |
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

### The internal shape the core specs hook into

These identifiers are unexported: nothing outside `internal/app` uses them. They are in
the spec because every core spec says where it plugs in, and a reviewer must be able to
check that against something written down.

```go
// command is one entry of the command table. Core parts add theirs in declare (identity:
// create-superuser; datarights: export, delete; files: files; demo: demo). Names are
// "[a-z][a-z0-9-]*".
type command struct {
	name    string
	summary string // one English line for `bms help` (operator text, like log lines)
	usage   string // the argument synopsis, "" when the command takes none: "<person-id>"
	run     func(ctx context.Context, p *process) int // loads config itself (decision 5); never calls os.Exit
}

// process is one invocation of Main: the args, environ and lookup, the writers, the
// clock, the registry (module.Default() in Main, a fresh one in tests), the parts
// (coreParts in Main) and the version.
type process struct{ /* ... */ }

// assembly is the core being built: what the commands fill in and what the parts read and
// extend. One per command run; built on one goroutine before anything serves; not safe for
// concurrent use. It holds config, telemetry, the database (dbc, pool, migrator), the
// dispatcher, the plan and the built set, the ui, the core* declaration slices (signals,
// extension points, migration sets, permissions), routeSets, authenticator,
// deps (coreDeps), runnables and commands, the built Lookups and the
// services a command calls (identity, authz, people, datarights, settings, departments,
// events, notify, regional, files, auditlog, jobs, demo). It holds no store, and no type
// in app holds an *assembly field except assembly's own methods.
type assembly struct{ /* ... */ }

// coreDeps holds the Deps fields the core provides to every module beyond Logger, Tracer,
// Clock, Links, DB and Signals: audit, settings, notify, jobs and files (scoped per
// module by provision), authz, people, departments and events (shared).
type coreDeps struct{ /* ... */ }

// part is how one core service plugs in. Every func is optional. A part never reads another
// part's output except through the assembly fields set by an earlier part or by buildLookups.
type part struct {
	name string // a reserved core area name (module.IsCoreArea); unique
	declare func(a *assembly) error // before Resolve, no I/O: appends to coreSignals, corePoints, coreSets (addCoreSet, in coreSetOrder), corePerms, commands
	build   func(ctx context.Context, a *assembly) error // after Resolve, before module Build, in serve, check, openapi, migrate: constructs the service; no I/O beyond reads; succeeds against db.None()
	link    func(ctx context.Context, a *assembly) error // after module Build, before api.New: the modules' sources and contributions; may add runnables and route sets
	seed    func(ctx context.Context, a *assembly) error // `bms migrate`, after the migrations, on the writing copy (authz: starter roles)
}

// runnable is a core component with background work: started after Set.Start, stopped
// before signals.Close (in reverse registration order).
type runnable struct {
	name  string
	start func(ctx context.Context) error
	stop  func(ctx context.Context) error
}

// closer is one shutdown step. shutdown runs them in order within budget: the first
// (HTTP) under max(1s, 2*budget/3), every other under the shared deadline start+budget;
// each is called even after an earlier failure or an exhausted deadline (every closer is
// idempotent and returns at once on a done ctx); errors are joined and returned (F15,
// F16). ctx is detached from cancellation (context.WithoutCancel).
type closer struct {
	name  string
	close func(ctx context.Context) error
}

func shutdown(ctx context.Context, logger *slog.Logger, clk clock.Clock, budget time.Duration, steps []closer) error

// provision is the module.Provision for this assembly: Logger = tel.Logger().With("module",
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
| `command`, `process` | the six built-in commands; the parts' commands | — | — |
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
control. Commands that act on a person's data (`export`, `delete`) are `datarights.md`'s:
it names the permission the command is audited under and `app` only dispatches them.
`app` registers one permission on behalf of `api`: `core.modules.read`
(`api.PermissionModulesRead`, scope site, reaches internal), gathered first in `corePerms`
and handed to `authz` with the core's other declarations (api.md Proposed change 10).

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
   (the operator's escape hatch when a closer hangs; decision 10; confirmed by the
   owner, 2026-10-17); call
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
   the same conditions, so `bms check; echo $?` predicts `bms serve` **up to the point
   where `serve` starts the modules**: configuration, telemetry, the registry, the
   database, the schema, the build and `api.New` are scored exactly as `serve` scores
   them. What `check` cannot observe the same way is reported but never changes the
   status: a module's `Start` is called under `db.None()` to learn its job registrations,
   and a `Start` that fails there (a read it may legitimately make against a real
   database, or a real fault) is printed in the jobs section as a problem line while
   `serve` would stop on it (jobs Q-JOBS10, owner, 2026-10-20, D44); the files
   public-bucket probe, the runner and listening on the port only run in `serve`. So a
   `0` from `check` means "`serve` will get as far as starting the modules", and the
   lines after `api:` say what it will find there.
4. **`serve` logs to stdout; every other command logs to stderr and writes its output to
   stdout.** D20's "JSON on stdout" is about the server a collector tails. `openapi`
   must emit the document and nothing else on stdout (`make openapi-check` redirects
   it); `check` and `migrate` write a plain-text report that an operator reads in a
   terminal, and interleaving JSON log lines with it would make both unreadable.
   `telemetry.Options.Stdout` is `p.stdout` for `serve` and `p.stderr` for the rest.
   Confirmed (owner, 2026-10-17); D20 gains the clarification (Proposed change 13).
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
   `bms migrate`" instead. Confirmed: from M0 (owner, 2026-10-17).
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
12. **Core services plug in as parts with a fixed order and four optional hooks, not as a
    plugin registry.** The core is a known, finite list; each service needs the plan
    (authz: `Permissions()`), the built set (datarights: `PersonData()`), or neither, and
    some have background work (jobs). Four hooks (`declare`, `build`, `link`, `seed`) and
    the `runnable` cover every case, and an ordered slice in `core.go` with one file per
    part (`core_identity.go`, ...) means adding a service is one file plus one line, and the
    commands and the lifecycle are not touched. An `init`-time registry for core parts was
    rejected: the core is not forkable the way modules are, and an explicit order is what
    the dependencies between parts need. The order is the build order below, a dependency
    order in which nothing takes anything built after it; no part is late-bound (amended
    2026-11-01, boundaries step 13; the earlier wording is in `history/app.md`).
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
    injects `BMS_SESSION_KEY` never gets a file, so config.md F27 cannot trigger. The data
    directory is created only when the file must be written (owner, 2026-10-17), so
    `migrate` also works on a read-only filesystem with `BMS_SESSION_KEY` injected.
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
17. **No drain delay in M0 (owner, 2026-10-17).** `/readyz` answers `503` the instant
    `Shutdown` is called and the listener closes at the same instant, so a probing
    balancer may send a few connections to a closed port during a rollout. A regional's
    deployment is one instance behind a tunnel or reverse proxy on the same host, where a
    restart is a restart. `http.drain_delay` (with `api.Server.BeginDrain()`) is designed
    with the Phase 1 packaging work if a regional runs two instances behind a probing
    balancer. Until then it is under "Not responsible for".

18. **Debug logging is announced (owner, 2026-10-17).** The app never logs e-mail
    addresses; test fixtures may hold them. Debug logging may carry private information if
    needed, but the app must make it obvious when it runs that way. In M0 scrubbing and
    redaction stay on at every level, so nothing is relaxed yet; the warning is the
    contract for the day something is. `serve` logs at warn, right after "bms starting",
    "debug logging is on: logs may contain personal data; do not run this way in
    production" with attribute `log.level=debug`; every other command prints the same text
    to stderr at start; `check` prints `log level: debug (logs may contain personal data)`
    in its configuration section. `/readyz` and the UI are unchanged. Any relaxation of
    scrubbing at debug level needs its own spec change.

### Accepted deviations (implementation review, 2026-10-17)

The reviewers accepted these differences from the text above; they are the behavior.

- `Version` takes build info's version and replaces `+` with `-` (a token has no `+`); with
  no usable version it falls back to `dev`.
- Shutdown writes one record per closer, "`<step>` stopped"; the final lifecycle line is
  the single "stopped" (with `duration_ms`).
- Shutdown deadlines are computed from the injected clock and applied as wall-time
  contexts.
- `check` with a broken `OTEL_*` variable still runs the database, schema and api sections
  (only the log line sources are lost).
- A `Canceled` configuration or secrets step prints "interrupted: rerun the command".
- `config.Load` and `secrets.Ensure` run under `checkTimeout`.
- `secrets.Ensure` creates a new file through a temp file and `link(2)`, so a concurrent
  creator never sees a partial file and never loses its own.
- The `e2ehang` test module lives in `internal/app` (test file with the build tag).
- `make check` needs Node (`web-check`, `openapi-check`); `check-go` is the Go-only
  subset.
- `openapi-check` writes `web/openapi.json` via a temp file, so a failed run leaves the
  committed file intact.

### `serve`, step by step

1. `config.Load(ctx, environ)` (F3). `Version()` (F2). `cfg.Session.Key.IsSet()` or F6.
2. `telemetry.New(ctx, Options{Level: cfg.Log.Level, Format: cfg.Log.Format, Instance:
   cfg.Instance.Name, Version: version, Stdout: p.stdout, Clock: p.clock, Environ:
   p.lookup})` (F4). From here every problem is also logged. Log at info "bms starting"
   with `version`, `instance`, `go_version`, `exporting`, and `slog.Any("config", cfg)`.
   When `cfg.Log.Level` is debug, log at warn "debug logging is on: logs may contain
   personal data; do not run this way in production" with `log.level=debug` (decision 18).
3. Derive `Enabled` (config.md "`modules.disabled`": names from
   `p.registry.Manifests()`, unknown names → F5 under `BMS_MODULES_DISABLED`, all listed).
4. Run every part's `declare` (F28). `p.registry.Resolve(module.Env{Enabled,
   CoreSignals: a.coreSignals, CoreExtensionPoints: a.corePoints})` (F7); log each
   `Warning` at warn with `module` and `message`.
5. `db.Open(ctx, db.Options{URL: cfg.DB.URL, MaxConns, ConnectTimeout, QueryTimeout,
   Tracer: tel.Tracer("db"), Meter: tel.Meter("db")}, p.clock, tel.Logger().With(
   "component", "db"))` (F8). `db.NewMigrator(pool, logger)`. Log `read_only` from
   `pool.ReadOnly()`.
6. `jobs.NewDeferrer(jobs.DeferrerOptions{DB, Clock, Instance})`, then `signals.New(signals.Options{Logger:
   ...With("component", "signals"), Tracer: tel.Tracer("signals"), Meter: tel.Meter("signals"),
   Clock: p.clock, HandlerTimeout: 5 * time.Second, Deferrer: the Deferrer})`. The Deferrer needs
   only the database and the clock, so nothing here waits for the job runner.
7. `checkSchemas` first checks the core sets (exit 3 when one is behind; only then does it also
   name the module sets). Then the Lookups (`buildLookups`: departments, authz, identity,
   people) and every part's `build` in `coreParts` order (F28); the registry's and the parts'
   permission declarations are gathered as `corePerms` with `api.PermissionModulesRead` first.
8. `a.plan.Build(ctx /* buildTimeout */, migrator, a.provision)` (F9 stays with `Plan.Build`).
   Run every part's `link` (datarights takes `set.PersonData()`; people, notify and demo take
   their contributions).
   `web.Version()`: compare with `version`; warn once when missing, invalid or
   different, only when a UI is present (F18).
10. `intake.New(intake.Options{Logger: ...With("component", "intake"), Meter:
    tel.Meter("intake"), Clock: p.clock})`. `api.New(api.Options{Addr, RequestTimeout,
    MaxBodyBytes, TrustedProxies, AllowPlaintext from cfg.HTTP; Logger:
    ...With("component", "api"); Tracer: tel.Tracer("api"); Meter: tel.Meter("api");
    Graph: plan.Graph(); Schema: `schemaStatus.Status`
    (`internal/app/schema.go`, `newSchemaStatus(migrator, sets)`, the sets computed once from
    `setsFor(plan.Order())` after `buildModules`; boundaries.md D1); Authenticator:
    a.authenticator; Permissions: `a.authzLookup`; UI: a.ui})` (F10).
11. `set.Start(ctx /* startTimeout */)` (F11); then each runnable's `start` in order
    (F11; on failure the started runnables are stopped in reverse and `set.Stop` runs).
12. `srv.Listen(ctx /* listenTimeout */)` (F12). Log at info "listening" with `addr` (the
    bound address), `modules` (enabled names), `ui`, `ui_version`.
13. `go srv.Serve()` into a channel. Wait for the first of: `ctx.Done()` (a signal) or
    the channel (F13). Log at info "stopping" with `reason` = `signal` or `serve_error`.
14. `shutdown(ctx, logger, clock, cfg.HTTP.ShutdownTimeout, []closer{http (srv.Shutdown),
    runnables in reverse, signals (disp.Close), modules (set.Stop), audit (spool close), db (pool.Close),
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
2. If `!cfg.Session.Key.IsSet()`: `os.MkdirAll(cfg.Data.Dir, 0o700)` (F23), then
   `secrets.Ensure(ctx, cfg.Secrets.File, []string{config.SecretsFieldSessionKey})` (F22);
   for each created name print config.md decision 7's notice to stderr, framed by a blank
   line before and after: "Created `<path>`. It holds this installation's session key.
   Back it up separately from the database and copy it to every copy of this
   installation; without it, every session is invalid and anything sealed with it is
   unreadable. Keep it readable by the bms user only."; then `config.Load` again (F3).
3. `telemetry.New` with `Stdout: p.stderr` (F4); log the configuration at debug. At debug
   level print the warning of decision 18 to stderr first.
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
   marked `run bms migrate`, then `secrets file: <path> (present | absent)`. When the log
   level is debug print `log level: debug (logs may contain personal data)` here. When
   `cfg.Session.Key` is unset still print "run bms migrate" but record `ExitUsage`, as
   `serve` would (F6); the remaining sections still run.
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
   `ExitFailure` (`Unavailable`, `Timeout`). A disabled module that has never been
   migrated is not a problem (printed as not migrated, disabled; `migrate` never migrates
   a disabled module).
6. The parts' `build`, `Build` with `skipSchema` and `db.None()`, `link`, `api.New` with
   the stub `Database`, an empty `SchemaStatus`, `UI: nil` (decision 7): print `api: <n>
   operations` (counted over the document's `paths`), or the problem with `ExitUsage`
   (a routing or fragment problem is the build's) or `ExitFailure` (F25).
6b. (M4, part 8) The jobs section: call the built set's `Start` under `checkTimeout` and
   `db.None()`, print `jobs` and one line per registered name with its owner and
   schedule (the runner's own, `audit.purge`, files' two, and what each module's `Start`
   registered). A `Start` error is printed first as `modules did not start: <problem>`
   and is **not** a problem for the status: it does not record `ExitFailure` and does
   not count in `result:`; the problem line comes first, then the jobs registered so far, then a note that the modules after the failed one were not started (Q-JOBS10, D44; `TestCheck_ModuleStartFails_StillExit0AndJobsSectionShowsIt`, integration).
   The modules are stopped before the next step.
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
   `nil, nil`; `Authenticator`: nil; `Permissions`: the authz.Lookup, as in `serve`; `UI`: nil; `Version`: `dev` (F10).
4. Write `srv.OpenAPI()` to stdout as indented JSON, followed by one newline; `ExitOK`. (`openapi` loads no configuration and logs at warn, so no debug warning.)

### `version` and `help`

`bms version` prints `bms <version> (<runtime.Version()> <GOOS>/<GOARCH>)` and returns
`ExitOK`; F2 applies. `bms help`, `bms`, `bms -h`, `bms --help` print the usage to stdout
(`ExitOK`): one line `usage: bms <command>`, one line per command in table order (`name`,
padded, then `summary`, then `usage` when present), then "Configuration comes from `BMS_*`
environment variables; run `bms check` to see it." `bms <command> -h` prints that command's
line alone. An unknown command prints `unknown command "<name>"` and the usage to stderr
and returns `ExitUsage` (F1). A command given arguments it does not take, or missing ones
it needs, is F1 too.

### The build order

`serve`, `check`, `openapi` and the operator commands build in this order
(`boundaries.md`, "The build order"; `TestParts_OrderAndNames_Pinned` pins the part order and that
every name is a reserved core area name). Each item names what it takes; nothing takes anything
built later. Rows 0 to 3 happen before any part: the Lookups (departments, authz, identity,
people) are built first by `buildLookups`, because audit, authz's service, settings, identity,
notify, the api shell and every module read them; the parts then run in `coreParts` order:
**audit, authz, settings, jobs, auditlog, departments, events, files, regional, notify,
identity, people, datarights, demo**. The row numbers below are the boundaries.md table's
numbers; the order of rows 12 to 16 is the as-built part order's constraint (notify before
identity and people), not row order.

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
| 20 | api | authenticator (identity), permission checker (authz.Lookup), route sets, schema-status reader |

Rules every part keeps: `declare` does no I/O; `build` does no I/O beyond reads and
succeeds against `db.None()` (decision 7) and on a read-only copy; a part's signals scope
comes from `a.disp.Scope(<area>, publishes, subscribes)`; a route set's `Module` is the
area name, never `core`, `modules` or `admin` (api.md F10); a part that needs a
configuration key proposes it to `config.md` and reads it from `a.cfg`; a package's own
settings area is read by that package through a `settings.Reader` in its `Options`; `Start`
methods that register jobs never write.

### What app wires and what it does not

App wires, it never decides (boundaries tenet). It calls constructors with values from
`config`, from other parts' outputs and from the plan; it converts shapes
(`module.CoreRoutes`); it collects declarations (`settingsAreas`, `declaredPermissions`, the
`link*` functions, `demoSchemas`); it orders the lifecycle; it maps errors to exit statuses
(`statusOf`, `demoStatus`). It parses no setting, implements no interface another package
declares except by forwarding to a built service, imports no package's store or channel
(depguard `app-no-stores`; only `core/db` and the object-store implementations app picks from
configuration), and holds no rule. A test of app proves wiring (a service receives what it was
given), never a rule. Kept in app because it wires: `check.go` (the order, the printing and the
status ranking), `usage.go` (the client manifest into the two intake services' allowlists),
`runDemo`, `demoStatus`, `demoSchema` (the reset's migrate step is `bms migrate` re-run: app's own
command sequence) and `logDemo`.

### Late-bound seams

None. No type in `internal/app` is named `lazy*`, none holds an `*assembly` field except
`assembly` itself, and no closure over the assembly is returned or stored in a package's
`Options`. The seam table is in `plan/07-abstractions.md`, "Late-bound seams", and reads
"None."; `internal/app/seams_test.go` (`TestSeamTable_MatchesApp`, part of `make imports`)
fails on a candidate that is not in the table and on a stale row, and `TestSeamTable_IsEmpty`
asserts that the table is empty and that app has no candidate. A new late-bound adapter needs
its own spec amendment first (the design checklist: "what does this need that is built after
it?").

### Runnables

A `runnable{name, start, stop}` is a service's background work under app's lifecycle: `start`
runs after `Set.Start` (modules register their jobs in `Start`), `stop` runs after the HTTP
server drained and before `signals.Close`, in the reverse of the order they were started
(D10). Registration order: the audit importer, the settings refresher, the notify sender, the
files probe, then the jobs runner, appended last in `link` so it starts last and stops first.
A failed `start` stops those already started (F11). The services' `Run` and `Close` are theirs
(`audit.md`, `settings.md`, `notify.md`, `files.md`, `jobs.md`); app only launches the goroutine
and waits for it to end within the deadline.

One generic `runner` (`internal/app/runner.go`) serves the audit importer, the settings refresher,
the notify sender and the jobs runner: `newRunner(label, run, close, logger)` and `runnable(name)`.
`start` launches `run` under a context detached from start's deadline and cancelled by `stop`;
`stop` calls `close` (if any), then cancels, then waits, and returns `close`'s error or the
context's kind (`Timeout`/`Canceled`); `stop` before `start` returns nil. A `Run` failure is
logged with the error value (all four). The audit importer's `start` still creates the spool
directory first (`auditRunnable`). The files probe stays its own `startFiles`, which calls `files.Probe` (the jobs are registered earlier, at link time, by `files.Start`; boundaries D7). Start order: audit,
settings, notify, the files probe, jobs; stop in the reverse.

`internal/app` is 4,793 production lines (5,673 when `boundaries.md` was written, F1).

### Commands and exit statuses

| Command | From | Exit statuses |
|---|---|---|
| `serve` | app | 0 stopped by signal; 1 runtime failure or a closer failed; 2 input; 3 schema |
| `migrate` | app | 0; 1 (read-only copy, a failing file, interrupted); 2 (config, secrets path); 3 (conflict) |
| `check` | app | the worst status found, `ExitUsage` > `ExitSchema` > `ExitFailure` > `ExitOK` |
| `openapi` | app | 0; 1 (a factory reads the database); 2 (registry or routing problem) |
| `version`, `help` | app | 0; 2 for an unknown command or a bad build version (F1, F2) |
| `create-superuser <email>` | identity | 2 config, key or input; 3 schema; 1 otherwise (including a read-only copy) |
| `export <person-id>`, `delete <person-id>` | datarights | 2 malformed id or config; 3 schema behind; 1 otherwise (`delete` refuses a read-only copy) |
| `files verify` | files | 0 nothing missing or different; 1 something is, or the store cannot be read; 2 config; 3 schema |
| `demo seed`, `demo reset` | demo | 2 `Unsupported`, or `Conflict` `seeded`, `not_empty`, `not_demo`; 3 the migrator's `Conflict`; else 1 |

Every part command builds the parts and the modules the way `serve` does but does not start
the modules; the audit actor of a command is the operator (identity's `WithOperator`, set by
the command), per `audit.md`.

### `bms check`'s probes

`check` opens no store and holds no rule. It calls `notify.Probe` (e-mail configured,
host:port, security; the outbox counts; `smtp: ok` or the kind of the failure; it prints no
address, user name or password and never fails the check), `demo.StateOf` (`demo: off`,
`demo: on (seeded <time>|not seeded|seeding)`, or `demo: on (state unknown: <kind>)`; never a
failure) and `audit.SpoolCounts` (`audit spool: empty` or `N waiting, M failed`; an unreadable
spool path is `ExitUsage`). It prints the jobs the built set registered (D3, F24), the usage-counts and
client-manifest lines (F41, F43), the UI line and `result:`. Printing and the status ranking are
app's; what each probe means is its package's.

### Client telemetry and demo wiring

After `web.FS()`, `app` calls `web.ClientManifest()` (`NotFound` or `Invalid`: one warning and
empty lists; `bms check` prints `client manifest: missing` as a failure in a build that has a UI
and as a note in one that has none, F41). It builds `intake.New` with `Options.Screens` (the
manifest's patterns) and `Options.Routes` (`api.RouteTokens(routes)`), builds `intake.NewUsage`
with `UsageOptions{Enabled: cfg.Telemetry.Usage, Screens, Actions, Modules, Logger, Meter, clock}` and passes it as
`api.Options.Usage`; the two services take the separate meter scopes `intake` and `usage`, and
`openapi` builds both with `Enabled: false` and empty lists (F42). `UsageOptions.Modules` (screen
pattern to the screen set that owns it) is filled from the manifest's `set` field; the `module`
attribute falls back to the pattern's first path segment, or `shell` for `/`; `api.RouteTokens`
excludes `/healthz` and `/readyz`. The demo part is built and linked in every process; with
`cfg.Demo.Enabled` false it declares its tables and the command and does nothing else. When it
is on, `serve` logs `demo mode on` (warn), identity gets `demo.NewGuard`, notify gets
`demo.NewCapture` as its channel, files gets `demo.FileCeiling`, and `api.Options.NoIndex` is
true. `TestDemo_ChangesOnlyTheListed` (the permission catalogue, settings catalogue, signal
names and route list differ from the production assembly only by the `demo` route set) and the
end-to-end demo test live here.

### Areas for modules that declare terms (D57)

`settingsAreas` builds an area for a module that declares settings *or* terms, carrying
`Plan.Settings()`'s `Terms`. The catalogue test gains `TestCatalogue_ModuleTerms_MatchManifestDefaults`
(the four `module.<name>.term.*` keys exist and equal the manifest's `en` defaults) and
`TestCatalogue_TermsUsed_AreDeclared` (every `{term_<x>...}` in a module's `messages/en.json` and
`{{term "<x>"}}` in its templates is in `CoreTerms` or that module's `Terms`); F44. D58:
`people.Options.Session = identity.SessionFrom`, and `TestMessageCatalogues_EnglishCoversJobs`
(F30a) sits beside `TestMessageCatalogues_EnglishCoversManifests`.

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
| `imports` | `scripts/check-module-imports.sh`, `scripts/check-web-module-imports.sh` and the seam test (`TestSeamTable_MatchesApp`) | Go; shell |
| `arch-report` | `scripts/arch-report.sh`: import edges, package sizes, `Options` field counts and the seam test, for the architecture review | Go; shell |
| `web-check` | `pnpm -C web check` | Node LTS, pnpm |
| `web-build` | `BMS_VERSION=$(VERSION) pnpm -C web build` (writes `web/dist/`, including `version.txt`) | Node LTS, pnpm |
| `openapi-check` | `go run ./cmd/bms openapi > web/openapi.json && pnpm -C web generate:api && git diff --exit-code web/openapi.json web/src/api/schema.d.ts` | Go, Node |
| `bin` | `web-build`, then `go build -ldflags "-X github.com/zedgama3/bms/internal/app.version=$(VERSION)" -o bin/bms ./cmd/bms` (the release binary, UI inside) | Go, Node |
| `run` | `set -a; . deploy/compose/.env; set +a; go run ./cmd/bms serve` (development; config.md decision 1) | `.env`, `make up` |
| `migrate` | the same with `migrate` | same |
| `test-e2e` | `bin`, then `scripts/e2e.sh` once per mode (default, `BMS_E2E_MODE=hello-disabled` with `BMS_MODULES_DISABLED=hello`, and `BMS_E2E_MODE=readonly`). Each run loads `.env`, runs `bin/bms migrate`, starts `bin/bms serve` on a free port, waits for `/readyz`, runs `pnpm -C web test:e2e` with `BMS_E2E_URL` and `BMS_E2E_MODE`, stops the server with `SIGTERM`, and fails if its status is not 0. Readonly mode creates a throw-away database through `docker compose exec postgres psql`, migrates it, freezes it with `ALTER DATABASE … SET default_transaction_read_only = on`, and drops it on exit. An unknown mode exits 2 | `make up`, Node, Playwright browsers |
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
`internal/core/telemetry`, `internal/core/telemetry/intake`, `internal/core/signals`, and the
core services it builds: `internal/core/audit`, `auditlog`, `authz`, `settings`, `jobs`,
`departments`, `events`, `files`, `regional`, `notify`, `identity`, `people`, `datarights`,
`demo`, `internal/core/objects` with `objects/disk` and `objects/s3` (the object store is
configuration, not a package's internals), the contracts it collects from
(`internal/contracts/personpage`, `eventsetup`, `membership`, `demoseed`), `internal/api`,
`web`, `internal/app/secrets`, and in `modules.go` every shipped module as a blank import
(`_ ".../internal/modules/hello"`). Never a package's `postgres` store, `core/notify/smtp` or
`core/notify/channel` (depguard `app-no-stores`), never pgx (depguard `storage-seam`: that is
why `db.None()` must live in `db`), never `go.opentelemetry.io`, never `dbtest` or
`telemetrytest` outside test files.
`secrets`: standard library (`context`, `crypto/rand`, `encoding/base64`, `encoding/json`,
`errors`, `fmt`, `io`, `os`, `path/filepath`), `internal/lib/errs`, `internal/config`
(constants only). `cmd/bms`: `context`, `os`, `os/signal`, `syscall`, `internal/app`.
`forbidigo` bans `fmt.Print*`; command output goes through `fmt.Fprint*` on the writers
`Main` was given, which it does not ban.

## Accepted implementation deviations

The M1 and M2 build deviations (2026-10-18) are in `plan/design/history/app.md`; the ones still true are folded into this section's text (the audit closer, the `check` spool line, `export` and `delete` pre-parsing the ID, commands that build modules but do not start them, `api.Permissions` and `core.modules.read`). Demo and telemetry build (2026-10-29): see "Client telemetry and demo wiring".

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
| F15 | `shutdown` | a closer fails or does not finish within its deadline | Timeout or its kind; the next closers still run (each with the shared deadline, even when passed); errors joined; each logged with `name`, `duration_ms`, `error.kind`; `ExitFailure` | read the log; a second signal forces exit | `TestShutdown_ContinuesPastFailuresAndJoins`, `TestShutdown_OrderAndDeadlines` (fake clock; HTTP's deadline is `max(1s, 2b/3)`, the others' is `start+b`; order http, runnables, signals, modules, audit, db, telemetry) |
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
| F28 | `Main` (the parts) | two parts or two commands share a name; a part's name is not a core area name (`module.IsCoreArea`) | Internal (a programming error); `ExitFailure` | fix `core.go` | `TestParts_OrderAndNames_Pinned`, `TestCommands_UniqueNames` |
| F58 | `serve`, `migrate`, `check` | log level is debug | the debug-logging warning (decision 18): a warn record in `serve`, a stderr line in the others, a `log level:` line in `check` | none | `TestServe_DebugLevel_WarnsAfterStarting`, `TestMigrate_DebugLevel_PrintsWarning`, `TestCheck_DebugLevel_PrintsLogLevelLine` |
| F59 | `check` | no session key | `ExitUsage`, still printing "run bms migrate" and running the other sections | run migrate or restore the file | `TestCheck_SessionKeyMissing_Exit2` |
| F60 | `check` | a disabled module with a schema was never migrated | none | — | `TestCheck_DisabledNeverMigrated_NotAProblem` |
| F29 | `provision` | `Tracer` or `Scope` refuses (a name the registry already validated) | the refuser's kind, wrapped with the module name (module-contract F24) | fix app | `TestProvision_ScopeConflict_ReturnsKind` (a dispatcher already holding the scope) |
| F30a | `TestMessageCatalogues_EnglishCoversJobs` (D58, jobs F64) | a job registered by a compiled-in module's or a core owner's `Start` (learned under `db.None()`, the path `bms check` uses) has no `module.<owner>.job.<ident>.title`, or its owner no `module.<owner>.title`, in the union of the shell's `en` catalogue and every `web/src/{modules,core}/*/messages/en.json`; a `Start` that fails under `db.None()` fails the test (it cannot vouch for that module); `zq*` test modules skipped | a test failure naming owner and key; skipped when `web/src` is absent | add the key | the test itself; `TestMessageCatalogues_FixtureMissingJobTitle_Fails` over a fixture |
| F30 | `TestMessageCatalogues_EnglishCoversManifests` | a registered module lacks `web/src/modules/<name>/messages/en.json`, or the file lacks a key module-contract decision 8 derives (`module.<name>.title`, `.description`, `.permission.<rest>.title`/`.description`, `.setting.<key>.title`/`.description`/`.option.<opt>`, `.signal.<rest>.title` for each published name, `.template.<ident>.title` for each `Manifest.Templates` ident (module-hello amendment, approved 2026-10-20, D40)) | a test failure naming module and key (not a runtime path); skipped when `web/src` is absent | add the key | the test itself; `TestMessageCatalogues_FixtureMissingKey_Fails` over a fixture |
| F38 | `demo` command | `Seed`/`Reset` return `Unsupported` or `Conflict` `seeded`/`not_empty`/`not_demo` | as returned | exit 2 | `TestDemoCommand_ExitStatuses` |
| F39 | `demo` command | the migrator's `Conflict` (schema ahead or behind) | `Conflict` | exit 3 | `TestDemoCommand_MigratorConflict_Exit3` |
| F41 | `serve`, `check` | `web.ClientManifest` is `NotFound` or `Invalid` | warning (serve starts); `check` fails in a build with a UI | build the web bundle | `TestServe_ClientManifestMissing_WarnsAndStarts`, `TestCheck_ClientManifestMissing_Fails` |
| F42 | `openapi` | builds `intake` and `Usage` | - | both with `Enabled: false` and empty lists | `TestOpenAPI_UsageBuiltDisabled` |
| F43 | `check` | `telemetry.usage` on or off | - | prints `usage counts: on (<n> screens, <m> actions)` / `off` | `TestCheck_PrintsUsageCounts` |
| F44 (D57) | `settingsAreas`; catalogue test | a module declares terms but no settings; a `module.<name>.term.<key>[.<form>]` entry is missing or differs from the manifest's `en` default; a module message or template names a term that is neither core nor its own | the module still gets an area (its terms appear on `/settings/terms`); the two catalogue failures are test failures, not runtime errors | fix `en.json` or the manifest | `TestSettingsAreas_TermsOnlyModule_HasArea`, `TestCatalogue_ModuleTerms_MatchManifestDefaults`, `TestCatalogue_TermsUsed_AreDeclared` |
| F45 | `TestSeamTable_MatchesApp`, `TestSeamTable_IsEmpty` | a `lazy*` type, a type with an `*assembly` field, a returned or stored capture of the assembly, or a stored func literal appears in `internal/app`, or the table in `07-abstractions.md` is not "None." | a test failure naming the candidate | name the split or remove it (boundaries tenet) | the two tests |

F31 to F37 (`lazyAuthorizer`, `lazyFilesStore`, `auditActor` in a job) and F40 (the demo adapters) are superseded: those adapters no longer exist; the rows are in `plan/design/history/app.md`. The system-actor rule is `audit.md`'s and `jobs.md`'s; the demo wiring is `demo.md`'s plus F40's replacement `TestDemo_ChangesOnlyTheListed`.

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
- Wiring and boundaries (boundaries.md): `TestParts_OrderAndNames_Pinned` (the part order and that every name is a core area name); `TestSeamTable_MatchesApp` and `TestSeamTable_IsEmpty` (F45); the `core_wiring_test` and per-part tests (a service receives what it was given, never a rule); `make arch-report` and depguard `app-no-stores` as review aids.
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
  `GET /api/hello/nope` is `404` with the `not_found` problem body →
  `GET /healthz` `200` → cancel ctx → `Main` returns `ExitOK` → stdout holds the four
  lifecycle lines in order and the seven closers' `stopped` records in the order of
  decision 10, no record carries the markers; the trace of the greeting request has the
  `hello` module's span under the request span. The write route is `POST
  /api/hello/greetings`; in M0 it answers `401` before the handler, so api.md Proposed
  change 11's request-driven `deliver:hello.greeted` assertion moves to M1 (first
  session), while `hello`'s own integration test `TestGreet_EndToEnd_DeliversInSameTrace`
  proves the same trace relationship in M0 (module-hello.md's Proposed change 6, applied
  2026-10-17).
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

## Amendments

The amendments to this spec are folded into the body above. Their narratives (the `lazyAuthorizer`, `lazyFilesStore`, system-actor and demo-adapter amendments of 2026-10-19 to 2026-10-28, the demo and telemetry build deviations of 2026-10-29, the 2026-10-30 notes), the old part table, the M1 and M2 build deviations and the earlier Summary are in `plan/design/history/app.md`.

## Open questions

None. The four questions were answered by the owner on 2026-10-17: the drain delay is
decision 17 (not in M0); the stderr logging of `migrate`, `check` and `openapi` is decision
4; the session key from M0 is decision 6; the second signal is decision 1.

## Proposed changes elsewhere

All items below are applied, or recorded as pending in their own text. References in them to "How later core services plug in" mean the part table in `plan/design/history/app.md`; the current order is "The build order".

1. **`plan/design/config.md` (row 3):** (applied 2026-10-17) in `Load`'s consumer wording and "Who needs each
   member", `cmd/bms` becomes "`app.Main` on `cmd/bms`'s behalf, once per command that
   needs configuration (`serve`, `migrate`, `check`, and later `create-superuser`,
   `export`, `delete`); `version`, `help` and `openapi` load none (app.md decision 5)";
   mark Proposed change 3 applied by this spec (F3, F5, F6, F22, the `check` output in
   "`check`, step by step", `secrets.Ensure` with its tests); F22 and F22b's tests are
   named here as `TestEnabledModules_UnknownDisabledName_ReturnsInvalid` and
   `TestCommand_SessionKeyMissing_ReturnsInvalidNamingSecretsFile`. The `http.drain_delay` part is dropped (decision 17).
2. **`plan/design/module-contract.md` (row 4):** (applied 2026-10-17) (a) give core definers a way to read
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
3. **`plan/design/db.md` (row 5):** (applied 2026-10-17) add `func None() DB`: a `DB` with no database behind
   it, for `bms openapi` and `bms check` (app.md decision 7): every `Exec`, `Query`,
   `Row.Scan`, `Rows.Err` and `Transact` fails with `errs.Unavailable` wrapping a new
   sentinel `ErrNoDatabase`; `ReadOnly()` is false; `AfterCommit` outside a transaction
   still runs the hook at once (nothing was written); it must live in `db` because only
   `db` and `postgres/` packages may import pgx (depguard `storage-seam`) and `DB`'s
   methods return pgx types. Mark Proposed change 5 applied by "`serve`, step by step"
   (step 5), "`migrate`, step by step", "`check`, step by step" and the compile-time
   assertion in the test plan.
4. **`plan/design/telemetry.md` (row 15b):** (applied 2026-10-17) mark Proposed change 5 applied; refinements:
   `Options.Stdout` is `stderr` for every command but `serve` (app.md decision 4);
   `openapi` passes an `Environ` that returns nothing, so a build step never exports;
   `bms check` prints `log: <format> <level>` and `OTLP export: on | off`.
5. **`plan/design/signals.md` (row 10):** (applied 2026-10-17) mark Proposed change 5 applied with one
   insertion: the runnables (jobs, M4) stop after the HTTP server drained and before
   `signals.Close`, so the order is HTTP → runnables → `signals.Close` → `Set.Stop` →
   `db.Close` → `telemetry.Close` (app.md decision 10).
6. **`plan/design/web-shell.md` (row 17):** (applied 2026-10-17) mark Proposed change 4 applied: (a) `bms
   openapi` as specified, with `info.version` fixed to `dev` so the committed document
   is stable across commits; (b), (c) as specified (F17, F18); (d)
   `TestMessageCatalogues_EnglishCoversManifests` requires `messages/en.json` for every
   registered module (screens or not) and is skipped only when `web/src` is absent; (e)
   the Makefile targets, with `bin` (not `build`) depending on `web-build`, `check-go`
   as the Go-only subset, and `scripts/e2e.sh` owning the server's start and stop under
   `test-e2e`; (f) confirmed by depguard.
7. **`plan/design/api.md` (row 16):** (applied 2026-10-17) mark Proposed change 11 applied: the sub-deadline
   is `max(1 s, 2 × shutdown_timeout / 3)`; the drain delay is decision 17 (not in M0; if added later, `api` gains `Server.BeginDrain()`: health routes answer `503` while
   the listener stays open); `core.modules.read` is registered through `authz`'s part
   with `corePerms`; `bms check` prints the operation count; the `deliver:hello.greeted`
   assertion runs in app's e2e test on `POST /api/hello/greetings` once identity gives a session (M1); `module-hello.md` defines the route and proves the M0 trace relationship itself.
8. **`plan/design/jobs.md` (row 14):** (applied 2026-10-20, D43, the table row 8 above; the setter is replaced by `lazyDeferrer{a}`, jobs.md Proposed change 2) `jobs` is core part 8 (D37: was 7): its `build` constructs the
   runner before `signals.New` and hands it as `Options.Deferrer`; it then receives the
   `*signals.Dispatcher` for `Redeliver` through a setter (`Runner.SetRedeliverer` or
   equivalent) that `app` calls after `signals.New`; it is a `runnable`: `start` after
   `Set.Start` (modules register jobs in `Start`), `stop` after the HTTP server drained
   and before `signals.Close`; `Deps.Jobs` is scoped per module by `provision`.
9. **`plan/design/identity.md` (row 6):** (applied 2026-10-18, by identity.md Proposed change 3 and the table row above; `rotate-secret` stays later) core part 2 (after audit): `declare` registers the commands
   `create-superuser` (M1) and `rotate-secret` (later) in app's command table with their
   `usage`, and its migration set; `build` sets `a.authenticator` and the `accounts`
   route set from `cfg.Session.Key`; both commands require the key (app F6) and load
   configuration through `Main` like `serve`.
10. **`plan/design/people.md` (row 8) and `departments-events.md` (row 9):** (people: applied 2026-10-18, the table row 5 above; departments-events: applied 2026-10-18, D37, the table rows 6 and 7 above; the assembly gains `departments *departments.Service` and `events *events.Service`) parts 5,
    6 and 7: `declare` lists their core signals and extension points (`Env.CoreSignals`,
    `Env.CoreExtensionPoints`) and, for `people`, the commands `export <person-id>` and
    `delete <person-id>` (`model.ParseID`; audited by `people`); `link` takes
    `a.set.PersonData()` and reads contributions through `Set.Contributors` (item 2(a)).
11. **`plan/design/authz.md` (row 7):** (applied 2026-10-18, the table row 4 above) part 4: `build` receives `a.corePerms`
    (`api.PermissionModulesRead` first, then every part's declarations) and
    `a.plan.Permissions()`, sets `a.permissions` and `a.deps.Authz`.
    **`audit.md` (row 12):** (applied 2026-10-18, by audit.md Proposed change 1: part 1, built before identity; the table row names its `coreDeps` field and that `provision` scopes it).
    **`plan/design/settings.md` (row 15), `notify.md` (row 11):** (applied 2026-10-19, D39: parts 3 and 9 as the table "How later core services plug in" states; `Settings` and `Notify` are scoped by `provision`)
    **`files.md` (row 13):** (applied 2026-10-20, D44, the table row 10 above) part 10 as that row states.
12. **`Makefile` and `scripts/e2e.sh`:** (pending: Makefile exists, but the new targets name web/, cmd/bms and scripts/e2e.sh, which are implementation created when app is built; adding them now would break make check) the targets in "The Makefile": add `VERSION`,
    `web-check`, `web-build`, `openapi-check`, `bin`, `run`, `migrate`, `test-e2e`,
    `check-go`; extend `check` and `imports`; add `scripts/e2e.sh`. `.gitignore` already
    ignores `/bin/`.
13. **`plan/03-decisions.md`, D20 (for `plan-keeper`):** (applied 2026-10-17) add: "The server (`bms serve`)
    logs to stdout; every other command logs to stderr so that stdout carries the
    command's output alone (app.md decision 4; pending the owner's answer to its Open
    question 2)."
14. **`plan/07-abstractions.md`, `app` entry:** (applied 2026-10-17) replace with: "**Purpose:** run the
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
    package does; packaging; configuration reload; a drain delay (not in M0). **Spec:**
    `design/app.md`." Layer rule 5 may add: "`cmd/bms` is a one-liner over `app.Main`."
15. **`plan/09-core-features.md` §1:** (applied 2026-10-17) "People can" gains `bms version` and `bms openapi`
    (for the web build) beside the four commands; "Done when" gains "every command
    returns one of four documented exit statuses, and `bms check` exits as `bms serve`
    would" and "a second SIGINT or SIGTERM terminates the process at once".
16. **`.claude/skills/add-module/SKILL.md` and `go-conventions`:** (applied 2026-10-17) the module list line
    is `_ "github.com/zedgama3/bms/internal/modules/<name>"` in `internal/app/modules.go`
    (plus the web line, D31); a `Factory` needs no database read (item 2(b)); every module
    ships `messages/en.json` (item 2(d)).
17. **`docs/operating.md`:** (applied 2026-10-30) the commands and their exit statuses; `bms
    check` before `bms serve`; signals (`SIGTERM` for an orderly stop, a second signal to
    force; `SIGHUP` does nothing); a systemd unit example with `KillSignal=SIGTERM` and
    `TimeoutStopSec` greater than `BMS_HTTP_SHUTDOWN_TIMEOUT`; that `serve` logs to
    stdout and the other commands to stderr; that `migrate` must run on the writing copy
    and prints the secrets-file notice once; that a build without `web/dist` serves the
    API only.
18. **`plan/design/settings.md` (row 15):** (applied 2026-10-19, D39, the table row 3 above) part 3 `settings`: `declare` adds `settings.Set` to `coreSets` and `settings.Permissions` to `corePerms`; `build` calls `settings.New` over the core areas and `a.plan.Settings()`, `audit.Scoped("settings")`, the authz actor closure and `a.cfg.Settings.SealingKey`; sets `a.deps.settings`, the `PolicySource` adapters for identity and people, route set `settings`, the runnable `Run`; `provision` sets `Deps.Settings`.
19. **`plan/design/notify.md` (row 11):** (applied 2026-10-19, D39, the table row 9 above) part 9 `notify` as that row states, with the three `bms check` e-mail lines and the part table note "over settings (SMTP) and jobs" becoming "over settings (SMTP); its own runnable (notify decision 2)".
20. **`plan/design/notify.md` and `identity.md`, the superuser alert:** (applied 2026-10-19, D39, "The superuser alert" above) `app` implements `identity.Alerter` in `internal/app/alert.go`: holders plus alert addresses; empty `Key`; reads only `notify.alert_addresses` (implementation, 2026-10-19).
