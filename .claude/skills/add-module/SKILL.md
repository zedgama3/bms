---
name: add-module
description: Checklist for adding or changing a feature module (volunteers, registrations, incidents, after-reports, or a regional's own) — spec, package layout, the module contract, its own schema and migrations, permissions, signals (domain events), settings, UI screens, data export and deletion, tests and docs. Load when designing, implementing or reviewing a module.
---

# Adding a module

The contract is defined by `plan/design/module-contract.md` and is authoritative once
Approved (2026-10-16). This is the checklist that wraps it. Module names are letters and
digits only (`afterreports`): one name is the Go package, the schema, the URL segment and
the message-key prefix.

## 1. Spec first

`architect` writes `plan/design/module-<name>.md` with the `design-spec` template. It must
cover:

- the jobs to be done, by role (participant, lead, admin, ...)
- the tables, in schema `<name>`, with a sensitivity level for each personal-data column
- the permissions it declares, their scopes (department, event, own records), the Reaches
  level of each permission, and the default roles that get them
- a table per published signal: name, when it is published, payload keys and their ID
  types (`signals.Payload` holds record references only, so no personal data can travel
  on a signal); for each subscription, what the handler does, that it runs as the system,
  how it is idempotent on `Signal.ID`, and what happens when it fails
- its settings, with defaults (anything regionals would do differently)
- its screens and who sees them
- how it answers "export my data" and "delete my data" for one person
- how its data carries over to a new event, and what's kept for history
- what it does if a module it reacts to (through signals) isn't enabled

## 2. Package layout

```
internal/modules/<name>/
  module.go        var Manifest = module.Manifest{...}: declares permissions (with Reaches),
                   routes, signals, settings, migrations, jobs, and what it provides,
                   requires, uses and contributes to (D19). Titles and descriptions are
                   catalogue keys derived from the names, not text (decision 8).
                   func New(ctx context.Context, deps module.Deps) (module.Module, error);
                   func init() { module.Register(Manifest, New) }
                   Start registers signal handlers and jobs and never writes
  service.go       use cases; each one checks its permission via authz first. Declares the
                   storage interface it needs (no SQL or driver types in it)
  postgres/        the PostgreSQL implementation of that interface (D8). Only this
                   package imports the driver.
    store.go       implements the interface
    sql/           .sql files embedded with go:embed
    migrations/    forward-only, numbered: 0001_init.sql, ... applied by `bms migrate`; the
                   highest number is the module's schema version, checked at init (D17)
  http.go          handlers: httpx.ReadJSON, call the service, httpx.WriteJSON; on error
                   httpx.Fail(w, r, err) and return (never write an error body yourself).
                   Path IDs with model.ParseID. No business logic. GET and HEAD routes
                   never change state: the cross-origin check does not cover them and
                   browsers prefetch them. Handlers start no goroutines (panic recovery
                   covers the request goroutine only; long work is a job).
  openapi.json     the module's OpenAPI fragment, embedded, returned by OpenAPI(): paths
                   relative to /api/<name> ("/x/{$}" is documented as "/x/", "{p...}" as
                   "{p}"), schema names starting with the module's name, $refs pointing at
                   its own schemas or the base ones (Problem, Field, ID, ...). A route
                   pattern ending in "/" is mounted exactly; use {path...} for a subtree.
                   api refuses to start if a route and the fragment disagree.
  signals.go       the payload key constants, the NewPayload(signals.Ref(...)) calls for
                   each published signal, and the handlers for each subscription
  privacy.go       implements module.PersonData: export and delete for one person
  <name>_test.go   black-box tests (test-author); calls moduletest.Conformance
web/src/modules/<name>/
  index.ts         exports `ui: ScreenSet` (name, kind "module", routes relative to
                   /<name>/, nav entries, message catalogues); one import line is added to
                   web/src/modules/index.ts (D31; web-shell.md)
  messages/en.json the keys module-contract decision 8 derives from the manifest
                   (module.<name>.title, .description, .permission.<rest>.title, ...) plus
                   the module's own under module.<name>.*; other languages optional
  screens/, components/
```

## 3. Rules

- **Never import another module**, in Go or in `web/`. To work with another module (D19):
  - to **show or collect** something another module knows (a section on a person's page,
    data to export), use an **extension point**: the asking side defines the shape in
    `internal/contracts/`, and any module contributes. This is the default.
  - to **call a service** only one module provides, `require` or optionally `use` a
    **capability** from `internal/contracts/`. Prefer optional use, and hide the feature
    when the capability is missing.
  - to **react** when something happens, subscribe to a **signal** (domain event, D24).
  - Declare all of it in the manifest.
  - In the browser the same rule: a module's screens call only `/api/<name>/` and the
    core's routes, and import only `@/api`, `@/i18n`, `@/ui`, `@/registry/types` and
    themselves; another module's data comes through a contract the owning module renders.
    ESLint and `scripts/check-web-module-imports.sh` (`make imports`) enforce it.
- **Every screen works on a phone first** (web-shell.md decision 21; owner, 2026-10-17):
  many participants only have a phone, so a flow that needs a wide screen or a mouse is a
  defect. Screens use the shell's `ErrorState` and `ErrorSummary`, `useT` with typed keys
  and `{term_*}` for terminology, and no `style` prop (CSP).
- **Signals:** publish inside the `Transact` that makes the change (delivery happens after
  commit, so a rolled-back change publishes nothing); subscribe in `Start`, once per name;
  handlers are quick and run as the system, long work is a job. Tests use
  `signalstest.NewFake(Manifest.Name, Manifest.Publishes, Manifest.Subscribes)` as
  `Deps.Signals` and `Fake.Deliver` to drive a handler (`plan/design/signals.md`). Contract calls take batches, and providers check
    permissions for the person asking. A new contract or a change to one is a design
    change: stop and send it to `architect`.
- **Never read or write another module's schema.** Foreign keys go only to your own tables
  and core tables.
- **Permissions are declared, not hard-coded to role names.** A regional decides which roles
  get them.
- **Terminology comes from settings.** A regional that calls them "theme villages" instead of
  "theme camps" changes a setting, not code.
- **The module works with zero optional settings changed.** Defaults are sensible.
- Turning the module off leaves its data in place and hides its routes and screens.

## 4. Done when

- [ ] Spec Approved, then Implemented through the build loop
- [ ] Migrations apply cleanly on an empty database and on one with the previous release
- [ ] Every permission rule and failure mode has a test; `moduletest.Conformance` passes
- [ ] Export and delete for one person are tested
- [ ] `make check` passes, including `make imports`
- [ ] UI reviewed by `web-reviewer`
- [ ] A short `docs/modules/<name>.md` for regionals: what it does, its settings and roles
