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
                 │ (HTTP)      │   │ incidents, after-reports, <yours>        │   (never import each other)
                 └──────┬──────┘   └───────────────────┬─────────────────────┘
                        │                              │
                        ▼                              ▼
     ┌──────────────────────────────────────────────────────────────────────────┐
     │ core contracts: module · db · identity · authz · people · editions ·     │   core
     │ departments · events · notify · audit · files · jobs · settings          │
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
   (acyclic). They never import a module, the api shell, `app` or `config`.
3. A **module** (`internal/modules/<name>/...`) depends on core contracts, `model` and
   `lib`. It **never imports another module**, the api shell, `app` or `config`.
4. The **api shell** mounts the routes it's given. It never imports a module.
5. Only `app` imports modules and core implementations, and turns modules on from
   configuration through the registry.
6. **Storage seam (D8).** Only `postgres/` subpackages and core `db` import the database
   driver. Services depend on a storage interface they declare themselves.

---

## Foundation

### model: shared value types
- **Purpose:** the vocabulary every layer shares (IDs, person reference, edition, department
  reference, time range, money if needed, sensitivity level).
- **Owns:** type definitions and their own validation (`Validate() error`).
- **Doesn't do:** I/O, storage, anything specific to one module.

### lib: internal shared libraries
- **Purpose:** small helpers every layer needs, so behavior is consistent.
- **Owns:** `errs` (error kinds), `clock`, `retry`, `validate`, `timeutil`, `secret`
  (ported from Sojourn, D11), `testkit` (test containers and fakes).
- **Doesn't do:** anything that knows about the domain.

### config: configuration loading
- **Purpose:** load and validate the process configuration, reporting every problem at once.
- **Doesn't do:** module settings that a regional edits at runtime (that's `settings`).

## Core

### module: the module contract and registry
- **Purpose:** define what a module gives the core, and register the modules compiled in.
- **Owns:** the `Module` interface, the registry, the enabled-modules check at start-up
  (for example, a module that subscribes to an event nobody publishes).
- **Doesn't do:** run migrations (`db`), mount routes (api shell), authorize (`authz`).

### db: database access
- **Purpose:** give modules a PostgreSQL connection with transactions and deadlines.
- **Owns:** the pool, transactions, read-only detection (Q-OFFLINE), the migration runner
  (per-schema, forward only) that `bms migrate` calls (D17), the start-up schema-version
  check, translating driver errors into `errs` kinds.
- **Doesn't do:** any module's queries.

### identity: who is this?
- **Purpose:** authenticate a person and manage their session.
- **Owns:** login providers (local, OIDC, optional trusted proxy header; D15), sessions, password or magic-link flow.
- **Doesn't do:** decide what they may do (`authz`), hold their profile (`people`).

### authz: may they do this?
- **Purpose:** decide whether a person may perform an action on a resource.
- **Owns:** roles, grants (optionally scoped to a department or edition), the permissions
  modules declare, the `Authorizer`.
- **Doesn't do:** authentication, audit logging.

### people: the directory of participants
- **Purpose:** hold each person's profile and contact details, with consent.
- **Owns:** profiles (playa name, legal name, contacts, emergency contact), consent records,
  export and deletion of a person's data across modules (through the module contract).
- **Doesn't do:** module-specific data about a person.

### editions and departments
- **Purpose:** the event years, and the departments that exist in each.
- **Doesn't do:** anything about what a department does (that's the modules).

### events: in-process domain events
- **Purpose:** let modules react to each other without importing each other.
- **Owns:** publish and subscribe, delivery inside a transaction or after commit (decided in
  the spec), the catalog of event types and their payloads.
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

### settings: what a regional configures at runtime
- **Purpose:** typed, validated settings per module and for the core (terminology, branding,
  retention periods), editable by admins.

## Delivery

### api shell
- **Purpose:** expose the core's and modules' routes over HTTP.
- **Owns:** router, middleware (session, CSRF, request IDs, limits), the error mapper
  (`errs` kind → HTTP status), the OpenAPI document assembled from module fragments.
- **Doesn't do:** business logic.

### web shell
- **Purpose:** the React app frame: login, navigation, layout, the API client, and the
  registry modules add screens to.

### app: composition root
- **Purpose:** build everything from configuration, start it and stop it cleanly. Provides
  the commands: `serve`, and `migrate` (D17).

## Modules

Each gets its own spec (`plan/design/module-<name>.md`), written with the `add-module`
skill: `volunteers`, `registrations`, `incidents`, `after-reports`.
