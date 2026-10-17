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
     │ events · signals · notify · audit · files · jobs · settings              │
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
4. The **api shell** mounts the routes it's given. It never imports a module.
5. Only `app` imports modules and core implementations, and turns modules on from
   configuration through the registry.
6. **Shared contracts (D19).** `internal/contracts/<name>/` holds interfaces and data types
   only. Contracts import only `model`, `lib` and other contracts.
7. **Storage seam (D8).** Only `postgres/` subpackages and core `db` import the database
   driver. Services depend on a storage interface they declare themselves.
8. `config` imports only `lib`; only `cmd/bms` and `app` import it.

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
  (ported from Sojourn, D11), `testkit` (test containers and fakes).
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
- **Owns:** the pool, transactions, read-only detection (for the post-MVP on-site mirror,
  D27; the handover mechanism is still open under Q-OFFLINE), the migration runner
  (per-schema, forward only) that `bms migrate` calls, the table of schema versions per
  module and the check each module calls when it initializes (D17), translating driver errors into `errs` kinds.
- **Doesn't do:** any module's queries.

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
- **Owns:** publish and subscribe, delivery inside a transaction or after commit (decided in
  the spec), the catalog of event types and their payloads, the `Name` type.
- **Doesn't do:** cross-process messaging.

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
- **Purpose:** let maintainers see problems early and trace any one request end to end.
- **Owns:** logger, tracer and meter set-up (OpenTelemetry, OTLP), request and trace IDs, the
  short error reference shown to people, and the intake of error reports from the web UI
  (validation, scrubbing, rate limits).
- **Doesn't do:** dashboards or alerts (those are deployment config), audit (`audit`),
  analytics about people.

### settings: what a regional configures at runtime
- **Purpose:** typed, validated settings per module and for the core (terminology, branding,
  default language (D28), retention periods (D30; default: keep forever)), editable by admins.

## Delivery

### api shell
- **Purpose:** expose the core's and modules' routes over HTTP.
- **Owns:** router, middleware (session, CSRF, request IDs and tracing, limits), the error mapper
  (`errs` kind → HTTP status), the OpenAPI document assembled from module fragments.
- **Doesn't do:** business logic.

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
