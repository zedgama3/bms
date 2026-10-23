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
  level of each permission, and the starter roles that get them (a "Starter roles" column
  in the Permissions table, mapping to `PermissionDecl.StarterRoles`, keys from `authz.Role*`)
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
                   New needs no database read: `bms openapi` and `bms check` build every
                   module against a DB that refuses every statement (app.md decision 7)
                   Start registers signal handlers and jobs and never writes
                   Views and Groups in the manifest; privacy.Filter in every use case that
                   returns a response type; visibility_<group> catalogue keys in en.json (D44);
                   every non-public field of a response type is omitempty, or omitzero
                   when it is a struct or array (D46)
                   The module list is one line in internal/app/modules.go:
                   _ "github.com/zedgama3/bms/internal/modules/<name>" (plus the web
                   line, D31)
  service.go       use cases; each one checks its permission via authz first (M2: `deps.Authz`;
                   `Allow` site-wide, `AllowIn` for one department's record, `ExtentOf` to
                   filter a list, search, count or export). Declares the
                   storage interface it needs (no SQL or driver types in it)
                   Until identity, authz and settings exist (M0), the service takes three seams
                   (an actor function, a permission checker, a setting source) wired to "no
                   actor", "deny" and "the declared default"; the service fails closed and
                   the wiring, not the code, changes later (`hello` shows it)
                   The actor is `identity.PersonFrom(ctx)` (false inside a signal delivery and for
                   a restricted session): the module imports `core/identity` for it and takes it
                   as a seam (`actorFunc`) so tests use `identity.WithSession`; no `Deps` field
  <record>/        a leaf package for record types shared by the service and the store
                   (`hello/greeting`). The module root imports postgres/ to build the store,
                   so postgres/ cannot import the root; the leaf imports only model and lib
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
                   (`people` calls it; DeletePerson runs inside people's transaction on the
                   ctx it is given; a foreign key to core.person(id) uses ON DELETE RESTRICT)
  <name>_test.go   black-box tests (test-author); calls moduletest.Conformance
                   with WithSeed (or WithoutPersonData) and WithCapability
                   for each required or used capability
web/src/modules/<name>/
  index.ts         exports `ui: ScreenSet` (name, kind "module", routes relative to
                   /<name>/, nav entries, message catalogues); one import line is added to
                   web/src/modules/index.ts (D31; web-shell.md)
  messages/en.json the keys module-contract decision 8 derives from the manifest
                   (module.<name>.title, .description, .permission.<rest>.title, ...) plus
                   the module's own under module.<name>.*; other languages optional.
                   Required even for a module without screens: the admin modules screen
                   shows its title, and app's catalogue test fails without it
  screens/, components/
```

The template to copy is the `hello` reference module (`plan/design/module-hello.md`).

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
- **Audit what the privacy rules say to audit** (`audit.md`; `deps.Audit` is scoped to the
  module, so actions are `<module>.<record>.<verb>`). Every read of `personal` fields about
  someone else and every `sensitive` read calls `deps.Audit.Record` after loading and before
  returning, with `Fields` and `Level`. A change records inside its `Write` transaction.
  Never record inside a `Read` transaction (`Internal`). A `Record` error fails the use case
  (no audit, no action). On a read-only copy a `Record` inside a `Write` transaction fails with `Unavailable` wrapping `db.ErrReadOnly` like the change itself and is never spooled; only a `Record` outside any transaction (a read, a login, a search) spools. Unit tests use `audittest.NewFake(Manifest.Name)`.
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
  get them. `PermissionDecl.StarterRoles` only seeds a starter role once at `bms migrate`.
- **Contributing to the person page** (M2): set `ContributesTo: personpage.Point`
  (`people.person-page`), return a `personpage.Provider` from `Contributions()`, add catalogue
  keys `module.<name>.section.<ident>.title`; the provider checks the viewer's permission and
  audits what it shows. Tests use `Deps.People` with `peopletest.NewFake()`, and `authztest.NewFake()` for `Deps.Authz`.
- **Departments and events** (M3, D37): `Deps.Departments` (`NamesOf`, `Active`; check `Active` before writing a department's record) and `Deps.Events` (`InfoOf`) are unscoped; tests use `departmentstest.NewFake()` and `eventstest.NewFake()`. A foreign key to `core.department(id)` or `core.event(id)` is allowed with `ON DELETE RESTRICT`; never read those tables directly. To contribute to a new event's set-up, set `ContributesTo: eventsetup.Point` (`events.event-setup`) and return an `eventsetup.Provider` from `Contributions()`: idempotent per (event, module), and it checks the caller's permission. A module that takes sign-ups subscribes to `departments.department-archived` and `events.event-closed`.
- **Settings** (M4, D39): `deps.Settings` is a `settings.Reader` scoped to the module (bare keys; `Text` for text and enum, `Secret` for a sealed value) and replaces the "declared default" seam; a module never caches a setting (the reader is a map lookup, and a change applies without a restart). Declare each setting in `Manifest.Settings` (`SettingDecl`: key `[a-z][a-z0-9]*(_[a-z0-9]+)*`, kind, default, options, optional `Check`); titles are `module.<name>.setting.<key>.title|description|option.<opt>`. Tests use `settingstest.NewFake(Manifest.Name, Manifest.Settings)` and `Fake.Set`. Declare retention keys (`<name>.retention*`) only when the spec has the job that enforces them; there are no automatic ones.
- **Jobs** (M4, D43): `deps.Jobs` (a `jobs.Registrar` scoped to the module): `Register` in `Start` with names `<name>.<ident>`, `Enqueue` inside the use case's transaction with record references only, handlers idempotent and running as the system; a dedupe `Key` names the record's own ID (the sign-up, the shift, the upload), never a person ID: keys show on `/jobs` and in logs; with a `Key`, the latest request wins: a repeat moves, reopens or (while the row runs) is held and applied after the run, so a handler may `Enqueue` its own key to run again later; a test asserts the schedule with `jobstest.Fake.Queued()` and drives it with `Fake.RunDue`, never from `Enqueued()` alone; tests use `jobstest.NewFake(Manifest.Name)` and `Fake.Run`.
- **Files** (M4, D44): `deps.Files` is a `files.Store` scoped to the module. An upload is `Stage` before the module's transaction and `Attach` inside its `Write` transaction with the record, with a deferred `Discard`; `Delete` and `DeleteByRecord` run inside the module's `Write` transaction; serve content from the module's own route after its permission check with `files.Open` and `files.Serve`, never through a public route unless the file's level is `public`, and never through a store URL; the level and `Subject` declared at `Attach` come from the module's spec (later from the field's privacy tag); `DeletePerson` deletes every file whose subject is the person; tests use `filestest.NewFake(Manifest.Name)`.
- **Privacy tags** (D44): the manifest carries `Views` (the response types) and `Groups`; every exported field of a response type has a `privacy` tag (`plan/design/privacy-tags.md`); the service calls `privacy.Filter` in every use case that returns a response type and audits what it reports; `visibility_<group>` catalogue keys go in `en.json`.
- **Notifications** (M4, D39, D40): `deps.Notify` is a `notify.Notifier` scoped to the module. `Send` enqueues on the caller's `Write` transaction (the message commits or rolls back with the change) with a template `<name>.<ident>`, a recipient person, an idempotency `Key`, `Vars` (nothing `sensitive`, nothing personal about anyone but the recipient) and an optional `Link` path. Ship templates as `Manifest.Templates` files `<ident>.<lang>.tmpl` (every ident needs `en`; titles `module.<name>.template.<ident>.title`). Ask `deps.Notify.Configured(ctx)` before work whose outcome must not depend on the recipient (a public route that would otherwise answer differently for a known and an unknown address); otherwise call `Send` and treat `Unsupported` as "no e-mail" (log once, continue). Pass a short `TTL` for anything carrying a one-time link. Tests use `notifytest.NewFake(Manifest.Name)` and assert with `Fake.Sent()`.
- **Terminology comes from settings.** A regional that calls them "theme villages" instead of
  "theme camps" changes a setting, not code.
- **The module works with zero optional settings changed.** Defaults are sensible.
- Turning the module off leaves its data in place and hides its routes and screens.

## 4. Done when

- [ ] Spec Approved, then Implemented through the build loop
- [ ] Migrations apply cleanly on an empty database and on one with the previous release
- [ ] Every permission rule and failure mode has a test; `moduletest.Conformance` passes
      (with `WithSeed`, or `WithoutPersonData` when the spec's Data section lists nothing about
      a person, and `WithCapability` for each capability required or used)
- [ ] Export and delete for one person are tested
- [ ] `make check` passes, including `make imports`
- [ ] UI reviewed by `web-reviewer`
- [ ] A short `docs/modules/<name>.md` for regionals: what it does, its settings and roles
