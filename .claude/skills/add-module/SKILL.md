---
name: add-module
description: Checklist for adding or changing a feature module (volunteers, registrations, incidents, after-reports, or a regional's own) — spec, package layout, the module contract, its own schema and migrations, permissions, domain events, settings, UI screens, data export and deletion, tests and docs. Load when designing, implementing or reviewing a module.
---

# Adding a module

The contract is defined by `plan/design/module-contract.md` and is authoritative once
Approved. This is the checklist that wraps it. Until the contract is Approved, treat the
layout below as the intended shape, not as settled.

## 1. Spec first

`architect` writes `plan/design/module-<name>.md` with the `design-spec` template. It must
cover:

- the jobs to be done, by role (participant, lead, admin, ...)
- the tables, in schema `<name>`, with a sensitivity level for each personal-data column
- the permissions it declares, their scopes (department, edition, own records), and the
  default roles that get them
- the events it publishes and subscribes to (payloads carry IDs, not personal data)
- its settings, with defaults (anything regionals would do differently)
- its screens and who sees them
- how it answers "export my data" and "delete my data" for one person
- how its data rolls over to a new edition, and what's kept for history
- what it does if a module it reacts to (through events) isn't enabled

## 2. Package layout

```
internal/modules/<name>/
  module.go        New(deps) (module.Module, error); registry init(); declares permissions,
                   routes, events, settings, migrations, jobs, and what it provides,
                   requires, uses and contributes to (D19)
  service.go       use cases; each one checks its permission via authz first. Declares the
                   storage interface it needs (no SQL or driver types in it)
  postgres/        the PostgreSQL implementation of that interface (D8). Only this
                   package imports the driver.
    store.go       implements the interface
    sql/           .sql files embedded with go:embed
    migrations/    forward-only, numbered: 0001_init.sql, ... applied by `bms migrate`; the
                   highest number is the module's schema version, checked at init (D17)
  http.go          handlers: decode, call service, encode. No business logic.
  events.go        event types published, and subscribers
  privacy.go       export and delete for one person
  <name>_test.go   black-box tests (test-author)
web/src/modules/<name>/
  index.ts         registers routes, navigation and screens with the web shell
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
  - to **react** when something happens, subscribe to a **domain event**.
  - Declare all of it in the manifest. Contract calls take batches, and providers check
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
- [ ] Every permission rule and failure mode has a test
- [ ] Export and delete for one person are tested
- [ ] `make check` passes, including `make imports`
- [ ] UI reviewed by `web-reviewer`
- [ ] A short `docs/modules/<name>.md` for regionals: what it does, its settings and roles
