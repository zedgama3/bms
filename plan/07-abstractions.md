# High-Level Abstractions

This is the map of the system's abstractions: what each one is for, what it owns, and what it
doesn't. It sets the boundaries. A **design spec** in [design/](design/) will fill each one
out (interface, permissions, failure modes, tests) before code is written for it. Spec status
is tracked in [design/README.md](design/README.md).

> First draft (2026-10-16). `architect` refines it; the owner approves changes.

## Layers and dependency rules

```
                 ┌──────────────────────────────────────────────┐
                 │ app (composition root)                        │  knows every module and implementation
                 └──────┬──────────────────────┬────────────────┘
                        ▼                      ▼
                 ┌─────────────┐   ┌─────────────────────────────────────────┐
                 │ api shell   │   │ modules: volunteers, registrations,      │   features
                 │ (HTTP)      │   │ incidents, afterreports, <yours>         │   (never import each other)
                 └──────┬──────┘   └───────────────────┬─────────────────────┘
                        │                              │ share through
                        │                              ▼
                        │          ┌─────────────────────────────────────────┐
                        │          │ contracts: interfaces and data types     │   capabilities and
                        │          │ only, no implementation (D19)            │   extension points
                        │          └───────────────────┬─────────────────────┘
                        ▼                              ▼
     ┌──────────────────────────────────────────────────────────────────────────┐
     │ core contracts: module · db · identity · authz · people · departments ·  │   core
     │ events · signals · notify · audit · files · jobs · settings · telemetry  │
     └──────────────────────────────────┬───────────────────────────────────────┘
                                        │ implemented by
       local, oidc (identity) · email (notify) · disk, s3-compatible (files) · postgres (db)
                                        │
                                        ▼
                    model (shared value types) · lib (errs, clock, validate, secret, ...)  foundation
```

Rules (enforced by depguard and `make imports`):

1. `model` and `lib/*` depend on nothing in the project except `lib`.
2. **Core** packages (`internal/core/...`) depend on `model`, `lib` and other core packages
   (acyclic). They never import a module, the api shell, `app` or `config`. `core/module`
   is the top of the core graph: it imports core contracts; no core package imports it.
3. A **module** (`internal/modules/<name>/...`) depends on core contracts, shared
   `contracts`, `model` and `lib`. It **never imports another module**, the api shell, `app`
   or `config`.
4. The **api shell** mounts the routes it's given. It never imports a module. Handlers in
   any layer reach the shell's error mapper through `lib/httpx.Fail`; `api` is never
   imported.
5. Only `app` imports modules and core implementations, and turns modules on from
   configuration through the registry.
6. **Shared contracts (D19).** `internal/contracts/<name>/` holds interfaces and data types
   only. Contracts import only `model`, `lib` and other contracts.
7. **Storage seam (D8).** Only `postgres/` subpackages and core `db` import the database
   driver. Services depend on a storage interface they declare themselves.
8. `config` imports only `lib`; only `cmd/bms` and `app` import it.
9. **Telemetry seam.** `core/telemetry` is at the bottom of the core graph: it imports `lib`
   only (and the OpenTelemetry SDK, which no other package imports; depguard `otel-seam`);
   every core package, `api`, `app` and every module may import it. Spans and metrics go
   through its `Tracer` and `Meter` interfaces with typed attributes, never through
   OpenTelemetry globals.

---

## Foundation

### model: shared value types
- **Purpose:** the vocabulary every layer shares (typed IDs (`ID[E]`; `PersonID`,
  `AccountID`, `DepartmentID`, `EventID`), sensitivity level, time range, e-mail address).
- **Owns:** type definitions and their own validation (`Validate() error`). No I/O;
  generates IDs with the standard library.
- **Doesn't do:** I/O, storage, anything specific to one module. Money is not defined
  (Q-PAY; D33 puts payments out of scope) and is added only when a consumer appears.
- **Spec:** [design/model.md](design/model.md), Approved.

### lib: internal shared libraries
- **Purpose:** small helpers every layer needs, so behavior is consistent.
- **Owns:** `errs` (error kinds), `clock`, `retry`, `validate`, `timeutil`, `secret`
  (ported from Sojourn, D11), `httpx` (the problem body, `StatusOf`, `Fail`, `ReadJSON`,
  `WriteJSON`, the client-address and scheme ctx accessors; owned by `api.md`), `testkit`
  (test containers and fakes).
- **Doesn't do:** anything that knows about the domain.

### config: configuration loading
- **Purpose:** turn what the operator sets in the environment into one typed, validated
  value the process starts from.
- **Owns:** the catalogue of keys (`Keys()`: dotted name, `BMS_*` variable, type, default,
  required, secret), loading from the environment with `*_FILE` for secrets, strict
  grammars, `Validate` reporting every problem at once by variable name, refusing unknown
  `BMS_*` variables.
- **Doesn't do:** runtime settings (`settings`), `OTEL_*` (`telemetry`), read-only detection
  (`db`), checking that a disabled module is registered (`app`), a configuration file or
  flags.

### contracts: what modules share (D19)
- **Purpose:** the shared shapes modules cooperate through: capability interfaces (one
  provider) and extension points (many contributors), with their data types.
- **Owns:** interfaces and plain data types. Calls take batches; providers check
  permissions for the person asking.
- **Doesn't do:** any implementation, storage, or HTTP.

## Core

### module: the module contract and registry
- **Purpose:** define what a module gives the core, and register the modules compiled in.
- **Owns:** the `Manifest` (static declaration), the `Module` interface and its optional
  interfaces, `Deps` (what the core gives a module), the registry, `Resolve` (graph check,
  build order, graph for the admin screen), `Build` (schema check per D17, verification
  against the manifest, linking), `Set` (start and stop in order), and the mandatory
  `PersonData` (export and deletion of one person's data).
- **Doesn't do:** run migrations (`db`), mount routes (api shell), authorize (`authz`).
- **Spec:** [design/module-contract.md](design/module-contract.md), Approved. Module names
  are letters and digits only (`afterreports`).

### db: database access
- **Purpose:** give modules a PostgreSQL connection with transactions and deadlines.
- **Owns:** the pool and its bounded, retried connect; statements with deadlines, on the
  pool or on the transaction the context carries; `Transact` with after-commit hooks;
  `Wrap` and `*Error` (driver errors classified into `errs` kinds, values scrubbed);
  read-only detection (`pg_is_in_recovery()` or `default_transaction_read_only`, probed on
  a schedule, on `Ping` and on every refused write) exposed as `ReadOnly()` for `/readyz`,
  `jobs` and `notify`; `ErrReadOnly` (`Unavailable`) for a write on a read-only copy; the
  migration runner over ordered sets (the core's, then `Plan.Order()`), one transaction
  per file, recorded with checksums in `core.schema_migration`; `CheckSchema` for
  `module.Build` (D17); `dbtest` (a fake `DB`, a temporary database). The handover between
  copies is still open under Q-OFFLINE (D27).
- **Doesn't do:** any domain table or query; retrying transactions; delivering signals; the
  switchover.

### identity: who is this?
- **Purpose:** authenticate a person and manage their session.
- **Owns:** login providers (local, OIDC, optional trusted proxy header; D15), sessions, password or magic-link flow, the break-glass superuser (D23).
- **Doesn't do:** decide what they may do (`authz`), hold their profile (`people`).

### authz: may they do this?
- **Purpose:** decide whether a person may perform an action on a resource.
- **Owns:** roles, grants (site-wide or scoped to one department, with an optional end date),
  separation-of-duties rules (D23), the permissions modules declare, the `Authorizer`, and
  the `Permission`, `Scope` and `PermissionDecl` types.
- **Doesn't do:** authentication, audit logging, the superuser (`identity`; it bypasses
  `authz` by design and is audited).

### people: the directory of participants
- **Purpose:** hold each person's profile and contact details, with consent.
- **Owns:** profiles (playa name, legal name, contacts, emergency contact), consent records,
  export and deletion of a person's data across modules (through the module contract).
- **Doesn't do:** module-specific data about a person.

### departments and events
- **Purpose:** the regional's standing departments (archivable, with their leadership) and
  its events (one per burn, D24).
- **Doesn't do:** anything about what a department does (that's the modules).

### signals: in-process domain events (D24)
- **Purpose:** let modules react to each other without importing each other.
- **Owns:** the `Name` type; the `Payload` (record references only); the `Bus` (`Publish`
  inside the publisher's transaction, `Subscribe` in `Start`); the `Dispatcher` (delivery
  after commit from `db.AfterCommit`, synchronous, in order, each handler under a deadline,
  failures logged and counted, never rolling back the publisher); per-module scoping to the
  manifest's names; the `Deferrer` seam for `jobs`; `signalstest`.
- **Doesn't do:** cross-process messaging; an outbox (until a consumer needs one); retries
  (`jobs`); the start-up graph check (`module`).

### notify: tell someone something
- **Purpose:** deliver a notification to a person through a channel.
- **Owns:** the notifier contract; e-mail first (SMTP), others later behind the same contract.
- **Doesn't do:** decide who needs telling (modules do).

### audit: who did what, and who saw what
- **Purpose:** append-only record of sensitive reads and of changes.
- **Doesn't do:** authorization, analytics.

### files: attachments
- **Purpose:** store and serve files attached to records, under the record's permissions.
- **Owns:** the storage contract; local disk first, S3-compatible as an option.

### jobs: background work
- **Purpose:** run scheduled and deferred work (reminders, retention) once, with retries.

### telemetry: logs, traces, metrics and client error reports (D20)
- **Purpose:** let a maintainer find what happened in any one request from the reference
  the person saw.
- **Owns:** the logger (JSON or text on stdout, trace IDs on every record); the `Tracer` and
  `Meter` interfaces over the OpenTelemetry SDK, with OTLP/HTTP export when `OTEL_*` names
  a collector and nothing exported (but everything still working) when it does not; the
  privacy policy on everything emitted (typed attributes, key denylist, `Scrub`, caps,
  counted violations); the error reference (`XXXX-XXXX`, the first four bytes of the trace
  ID, which is the request ID: there is no separate one); `TraceIDFrom` for `audit`; the
  client error intake service (`intake`: validation, limits, scrubbing; `api` owns the
  route); `telemetrytest` (in-memory capture that fails a test on any policy violation).
- **Doesn't do:** the request middleware and the `/api/client-errors` route (`api`), the
  browser side (web shell), statement spans and pool gauges (`db`), dashboards, alerts and
  retention (the deployment), audit (`audit`), analytics about people.
- **Spec:** [design/telemetry.md](design/telemetry.md), Approved.

### settings: what a regional configures at runtime
- **Purpose:** typed, validated settings per module and for the core (terminology, branding,
  default language (D28), retention periods (D30; default: keep forever)), editable by admins.

## Delivery

### api shell
- **Purpose:** expose the core's and the modules' routes over HTTP with one request
  pipeline.
- **Owns:** the router (`net/http.ServeMux`, routes mounted under `/api/<name>/`, one
  `/api/` fallback for JSON 404 and 405, no redirects under `/api/`); the middleware chain
  (recovery, the peer check for a misrouted plaintext path, the client address and scheme
  from trusted proxies, security headers, body cap, request deadline on the ctx and the
  connection, draining; per route: the request span and duration histogram with the
  method normalised, the error mapper, the cookie guard, cross-origin protection,
  authentication through `identity`'s `Authenticator`); the problem body and the error
  mapper (`errs` kind and the `db`/`intake` sentinels → status and code, `ref`, one log
  line per error response); `/healthz`, `/readyz`, `GET /api/modules`,
  `GET /api/admin/modules`, `POST /api/client-errors`, `GET /api/openapi.json` at
  absolute paths; OpenAPI assembly from the sets' fragments with a start-up check that
  routes and fragments agree; serving the built web shell.
- **Doesn't do:** business logic, permission checks (the service's), sessions
  (`identity`), permissions (`authz`), TLS (the front end), configuration loading
  (`config`), the shutdown deadline and order (`app`).
- **Spec:** [design/api.md](design/api.md).

### web shell
- **Purpose:** the React app frame: login, navigation, layout, the API client, and the
  build-time registry that imports each module's own folder under `web/` (D31). Catches
  errors and reports them (D20). Every string is translation-ready (D28).

### app: composition root
- **Purpose:** build everything from configuration, start it and stop it cleanly. Provides
  the commands: `serve`, and the installer commands (`migrate`, and creating the first
  admin; D17).

## Modules

Each gets its own spec (`plan/design/module-<name>.md`), written with the `add-module`
skill: `volunteers`, `registrations`, `incidents`, `afterreports`. Module names are letters
and digits only (`after-reports` is `afterreports` in code, schema and URLs).
