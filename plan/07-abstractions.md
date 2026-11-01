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
     │ events · signals · notify · audit · files · jobs · settings · privacy ·  │
     │ telemetry · demo · auditlog · regional                                   │
     └──────────────────────────────────┬───────────────────────────────────────┘
                                        │ implemented by
       local, oidc (identity) · email (notify) · disk, s3-compatible (objects) · postgres (db)
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
   configuration through the registry. `cmd/bms` is a one-liner over `app.Main`.
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
10. **Web shell.** The Go package `web` (`web/embed.go`) holds the built shell; it imports
    the standard library and `lib/errs` only, and only `app` imports it (depguard `web`).
    In `web/src/`, a module's or core area's folder never imports another's, `core/*` or
    `shell/*`; enforced by ESLint and `scripts/check-web-module-imports.sh` under
    `make imports`.
11. **App wires, it never decides.** `internal/app` calls constructors, converts shapes,
    collects declarations, orders the lifecycle and forwards; it parses no setting and holds
    no rule. Each core package reads its own settings area through a `settings.Reader`.
12. **No hidden two-way dependencies.** If component A takes B at runtime and B takes A,
    split one into a part built below and a part built above. A late-bound adapter in `app`
    (a value that reads a field set after construction) is listed in "Late-bound seams" with
    the component it waits for and why no split is possible;
    `TestSeamTable_MatchesApp` (under `make imports`) fails on one that is not.

## Late-bound seams

A row is one type in `internal/app` (or one top-level function, for a closure that reads the assembly) that reads a field set after it was constructed. "Waits for" is what it resolves when called; "Reason" is the pair that forces it and the migration step that removes it (`plan/design/boundaries.md`). `TestSeamTable_MatchesApp` (rule 12, under `make imports`) fails on an unlisted late-bound type or closure, on a returned or stored capture that is not listed (shapes 5a and 5b), on a stored inline func literal always (it has no name to list; extract it), on a row that names nothing in `app` or is no longer late-bound, and on a missing section or rows beside "None.". "None." is the empty table.

| Type | Waits for | Reason |
|---|---|---|
| `lazyGuard` | a.demo, which the demo part sets last | identity asks demo whether an account is protected, and demo creates accounts through identity (identity ↔ demo); removed in step 10. |
| `lazyFilesPolicy` | a.demo, which the demo part sets last | files takes demo's cap, and demo's reset clears files (files ↔ demo); removed in step 10. |
| `lazyDeferrer` | a.jobs, which the jobs part sets after signals is built | the dispatcher defers through jobs, and the jobs runner redelivers through the dispatcher (signals ↔ jobs); removed in step 11. |
| `demoAccess` | a.authz, which authz's part sets | the demo seed uses a slice of authz in a shape authz does not offer; demo is built last, so this is forwarding, not a runtime pair (F1); removed in step 10. |
| `demoObjects` | a.cfg and a.plan for the buckets to clear | demo's reset needs files to clear every owner's objects, and files holds the demo cap (files ↔ demo); removed in step 10. |
| `demoSchema` | a.migrator, a.plan and a.authz | the reset's migrate step is `bms migrate` re-run, which needs the authz seeder built before demo runs; it holds the assembly only because of that (F1); removed in step 10. |
| `demoChannelFactory` | a.demo, which the demo part sets last | notify takes demo's capture channel, and demo is built after notify (notify ↔ demo); removed in step 10. |
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
- **Example:** `contracts/greeter` (capability `greeter`, extension point `hello.banner`),
  the first contract, shipped with the `hello` reference module
  ([design/module-hello.md](design/module-hello.md)); it has no consumer in M0. Others sit
  beside it: `personpage`, `eventsetup`, `membership` and `demoseed` (the extension point
  `demo.seed`, through which a module adds its own demo data; D53).

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
  pool or on the transaction the context carries; `Transact` with after-commit hooks
  and `TxMode` (the mode of the transaction a context carries, for `audit`);
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
- **Owns:** local accounts keyed by e-mail (D21), the password policy and Argon2id, the signed session token and cookie checked without a write, the `Authenticator` and the principal accessors (`SessionFrom`, `PersonFrom`), per-account revocation, failed-login and password-reset limits, the superuser and `bms create-superuser` (D23), the `accounts` routes and screens, `identity.account-created`, `identity.NewLookup` (`EmailOf` over `core.account` alone: no permission, no audit; notify's `Accounts`); providers (OIDC, trusted header; D15) Later behind the same service.
- **Doesn't do:** decide what they may do (`authz`), hold their profile or the playa name (`people`), invitations until their milestone, e-mail delivery (`notify`). The superuser alert is identity's own (`identity/alert.go`, boundaries step 9): the holders of `accounts.manage` through `Holders` (the `authz.Lookup`) and `Notify`, the alert addresses through `Alerts` (`notify.AlertSender`), all required. The forgotten-password flow (token, routes, screens; D39) is identity's, the e-mail is notify's.

### authz: may they do this?
- **Purpose:** decide whether a person may perform an action on a resource.
- **Owns:** the permission catalogue (declared by the core and modules, with scope and
  `Reaches`); roles (site or department scoped, the base role, the starter roles seeded once
  from `PermissionDecl.StarterRoles`); grants (site-wide or for one department, optional end
  date); separation-of-duties rules (D23) checked at grant time and on every decision; the
  `Authorizer` (`Allow`, `AllowIn`, `ExtentOf` as the batch primitive); `PermissionsOf` for the
  session and `TitlesOf` for audit; the `access` routes and screens; signals
  `access.grant-created|revoked`; and the `Permission`, `Scope` and `PermissionDecl` types.
- **Doesn't do:** authentication, audit logging, creating or marking the superuser
  (`identity`); own-record checks (each service, through `identity.PersonFrom`); whether a
  person or department exists (`people`, `departments-events`); positions and qualifications
  (`volunteers`). `authz` allows the superuser by the flag identity supplies; it bypasses
  `authz` by design and is audited.

### people: the directory of participants
- **Purpose:** hold each person's profile and contact details, with consent.
- **Owns:** profiles (playa name, pronouns, language; legal name, phone, a contact address for
  people without an account), emergency contacts, consents by purpose, the person row behind
  every account (`identity.account-created`) and account-less people with `LinkAccount`, name
  lookup and search, the person page with `people.person-page`, export and deletion across
  identity, authz and every module (`PersonData`) with deletion requests, the `export` and
  `delete` commands, the `people` routes and screens, signals
  `people.person-created|deleted|deletion-requested`, `Directory.LegalNamesOf` (audited, `people.contacts.read`), and the contracts `contracts/personpage` and `contracts/membership` (D44), the installation-wide uniqueness and permanence of playa names (`NameKey`, `playa_name_key`), the once-per-session first-sign-in prompt mark (`name_prompted_at`, D57), `people.NewLookup` (the system reads `NamesOf`, `PlayaNameOf`, `LanguageOf`, `ConsentOf`, `ContactEmailOf`: no permission, no audit; the Service forwards them; notify's `Profiles`).
- **Doesn't do:** module-specific data about a person; the login address (`identity`); who may
  see a profile (`authz`); the person's own view of who read their data (`audit`, Later);
  department membership (a module contributes it through `people.membership`; `volunteers` first); retiring or approving names (Later).

### departments and events
- **Purpose:** the regional's standing departments and its events (one per burn, D24), kept
  as the reference records every module attaches its data to. Two packages, two `app`
  parts (`departments`, `events`), one spec (`departments-events`).
- **Owns:** `departments`: the standing departments (name, description, shared mailbox),
  archiving and restoring, `Directory` (`NamesOf`, `Active`) which is also authz's
  `DepartmentSource`, the `departments` routes and screens, signals
  `departments.department-created|archived|restored`. `events`: the events (name, kind,
  dates as instants plus zone, schedule items), kinds, closing and reopening, `Directory`
  (`InfoOf`), the extension point `events.event-setup` (`contracts/eventsetup`), the
  `events` routes and screens, signals `events.event-created|changed|closed|reopened`.
- **Doesn't do:** anything about what a department does (that's the modules); leadership
  (authz grants of `department-admin`); membership and positions (`volunteers`); work
  around an event (modules); personal data (none held).

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
- **Owns:** the `Notifier` contract (scoped per owner; `Send`, `Configured`), `AddressSender` and `AlertSender` (`Service.Alerts(owner)`: one message to every `notify.alert_addresses` entry, for the superuser alert); templates per language with regional overrides; the outbox (`core.notify_message`) written on the caller's transaction; the sender loop on the writing copy with retries; the SMTP channel (stdlib; Mailpit in development); the `notify` routes and screens; permissions `notify.templates.manage`, `notify.outbox.manage`; settings `notify.*`; the delivery rules (the login address, else the contact address, else undeliverable; the language fallback; the four term forms), read through `Accounts`, `Profiles`, `Regional` and `Terms`, so it is built before identity and people (boundaries step 8). E-mail first, others later behind the same `Channel`.
- **Doesn't do:** decide who needs telling (modules do), subscribe to signals or pick recipients; HTML, attachments, SMS (Later behind `Channel`).

### audit: who did what, and who saw what
- **Purpose:** append-only record of sensitive reads and of changes.
- **Owns:** the `Entry` vocabulary, `Recorder` (scoped per area), the table and its guard, the read-only spool and its importer, the ctx markers `WithSystem` and `WithOperator` and the actor order. Spec: `plan/design/audit.md`.
- **Doesn't do:** authorization, analytics, search, export, retention or the `audit.*` settings (`auditlog`).

### auditlog: reading and expiring the log
- **Purpose:** let the right people read, export and expire what the audit recorder wrote.
- **Owns:** `Search`, `Export` and `CountAbout` behind `audit.search` and `audit.export`; the settings `audit.retention` and `audit.self_view` (it replaces the deleted `auditsettings`); the `audit.purge` job (0 keeps forever, otherwise purge older than now minus the retention, writing copy only); the routes `GET /api/audit/entries` and `GET /api/audit/people/{person}/views`. Split from `audit` because it needs `authz.Lookup`, `settings` and `jobs`, which are built after the recorder. Spec: `plan/design/auditlog.md`.
- **Doesn't do:** write entries, the spool, the table or its guard (`audit`); decide permissions (`authz`); schedule jobs (`jobs`); export and deletion across modules (`datarights`, which calls `Export` and `CountAbout`).

### regional: the regional's public face
- **Purpose:** hold the regional's own profile, brand colours and logo, and the public document the shell boots from.
- **Owns:** the `regional.*` settings area (`name`, `contact_email`, `public_url`, `language`, `timezone`, `color_primary`, `color_primary_dark`) and `Profile`; the public document, `theme.css`, and the logo (`LogoRef`, `MaxLogoBytes`, `SetLogo`, `RemoveLogo`, `OpenLogo`), served under `/api/regional/` (`GET public|theme.css|logo` public; `PUT|DELETE logo` with `settings.manage`); audit actions `regional.logo.changed|removed` (old entries keep `settings.logo.*`). The logo's files owner stays `"settings"`. Split from `settings` so that `settings` no longer imports `files`. Spec: `plan/design/regional.md`.
- **Doesn't do:** store values, the revision, terminology or the admin screens (`settings`); store or serve bytes (`files`); send mail (`notify`, which reads `Profile`); permissions (`settings.manage` is `settings`'); personal data.

### files: attachments
- **Purpose:** keep the bytes a module attaches to one of its records, with the record's permissions deciding who may read them.
- **Owns:** the object-store contract `objects.Store` (a simplified S3: bucket, key, Put/Get/Head/Delete/List, SHA-256 verified at Put; never the database, D43) with the `disk` (default) and `s3` implementations and their conformance suite; the `Store` seam modules get as `Deps.Files`; the metadata (`core.file`, `core.file_tombstone`); the catalogue of accepted kinds (images and PDF), metadata stripping, the safe `Serve`, the audit of content reads, the person-data export and deletion backstop, the reap and sweep jobs, `bms files verify`, the settings `files.max_bytes|quota_bytes|max_per_record` (it declares them as `files.Settings`, replacing the deleted `filesettings`, and reads them through its own `settings.Reader`), config `files.*`. Spec: `plan/design/files.md`.
- **Doesn't do:** permissions (the owning module), routes and screens (the owner; the logo is `regional`'s), retention (the owner's job), running a store over the network (MinIO, Garage, SeaweedFS, Ceph through `s3`), keeping a copy's objects in sync (the operator; later `switchover`), virus scanning, thumbnails.

### jobs: background work
- **Purpose:** run scheduled and deferred work (reminders, retention) once, with retries.
- **Owns:** the `Registrar` contract (`Register`, `Enqueue`; = `Deps.Jobs`, scoped per owner), the table `core.job` (scheduled slots and deferred jobs, record references only), the runner (`SKIP LOCKED` leases with a heartbeat, backoff by `retry.Policy`, writer only), the `signals.Deferrer` implementation and `signals.redeliver`, `jobs.purge`, the status screen behind `jobs.read` / `jobs.manage`. Spec: `plan/design/jobs.md`.
- **Doesn't do:** notify's sender loop (notify decision 2), retention periods (each area's setting), daily local-time schedules (Later), cross-process messaging.

### privacy: who sees which field
- **Purpose:** decide, field by field, which of a record's fields a viewer gets back.
- **Owns:** the `privacy` struct tag, field groups and their `visibility_<group>` settings, the viewer's audiences, `Filter` and what it reports for audit, the `views_tagged` conformance rule. Spec: `plan/design/privacy-tags.md`.
- **Doesn't do:** action permissions and record visibility (`authz`, each service), recording the audit entry (the caller), column sensitivity, retention, export and deletion (each spec, `people`).

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
  route); the usage intake (`intake.Usage`, D54): screen views, declared actions,
  screen-ready time and Web Vitals as bounded metrics by screen template, never per
  person; `telemetrytest` (in-memory capture that fails a test on any policy violation).
- **Doesn't do:** the request middleware and the `/api/client-errors` route (`api`), the
  browser side (web shell), statement spans and pool gauges (`db`), dashboards, alerts and
  retention (the deployment), audit (`audit`), analytics about people: profiles, journeys,
  return visits and visit IDs (nobody).
- **Spec:** [design/telemetry.md](design/telemetry.md), Approved.

### demo: a safe public copy (D53)
- **Purpose:** let strangers use a public copy of bms safely: made-up data in, nothing real out.
- **Owns:** the switch `demo.enabled` (`BMS_DEMO_ENABLED`, off by default) and `demo.public_url`; the seed, which builds one fictional regional through the services as ten demo accounts and asks each module for its data through the extension point `demo.seed` (`contracts/demoseed`); `bms demo seed|reset`; the `Guard` that keeps the shared accounts usable; the mail capture channel and a screen showing each signed-in account only the mail addressed to it; the upload caps; the tables `core.demo_state`, `core.demo_account`, `core.demo_mail`. Spec: `plan/design/demo.md`.
- **Doesn't do:** change permissions, audit, the privacy filter or validation in demo mode, client telemetry (`telemetry`), scheduling the reset (the operator's timer), seeding a module's data (the module), send mail (`notify`).

### settings: what a regional configures at runtime
- **Purpose:** typed, validated settings per module and for the core (terminology, brand colours,
  default language (D28), retention periods (D30; default: keep forever)), editable by admins.
- **Owns:** the declaration types (`Kind`, `Decl`, `Area`), `core.setting` and its revision, terminology (four forms per term per language), the sealed `Secret` kind, scoped `Reader`s, `TermsFor`, `Revise` (the revision lock as a seam for `regional`), the settings screens (including the Regional form and the logo field, which call `regional`'s routes). It declares no area of its own to the core: `regional`, `accounts`, `audit`, `files` and the rest declare theirs and `app` passes them in.
- **Doesn't do:** process configuration (`config`), the meaning of other areas' keys (their specs), enforcing retention (`jobs`), translations (web-shell), signals on change, the regional's profile, public document, `theme.css` and logo (`regional`), storing the logo's bytes (`files`), SVG sanitising (`files`, Later; D47). `settings` no longer imports `files`.

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
  `GET /api/admin/modules`, `POST /api/client-errors`, `POST /api/client-events` (D54),
  `GET /api/openapi.json` at absolute paths; the `bms.api.errors` counter (D54); the
  `X-Robots-Tag: noindex` option for a demo (D53); OpenAPI assembly from the sets' fragments with a start-up check that
  routes and fragments agree; serving the built web shell.
- **Doesn't do:** business logic, permission checks (the service's), sessions
  (`identity`), permissions (`authz`), TLS (the front end), configuration loading
  (`config`), the shutdown deadline and order (`app`).
- **Spec:** [design/api.md](design/api.md).

### web shell
- **Purpose:** host every module's screens in one browser application.
- **Owns:** the frame (navigation from the enabled modules, sign-in state, the read-only
  and superuser banners, the language menu), the typed API client generated from
  `/api/openapi.json` (`traceparent`, one `ApiError`, `X-Read-Only`, `401` → sign-in), the
  build-time screen registry (one folder per module under `web/src/modules/`, one per core
  area under `web/src/core/`, mounted under `/<name>/`, modules shown only when enabled;
  D31), translation (FormatJS/ICU catalogues per set, keys typed, terminology as
  arguments; D28), error display (codes and field codes translated, the reference shown,
  the server's English `reason` only as the last-resort fallback inside a translated
  wrapper), the reporter (`POST /api/client-errors`; D20), the usage collector (`POST /api/client-events`; `trackAction`; D54), the demo banner and the captured-mail screen (D53), the accessibility rules and the
  phone-first rules (a phone is the primary device; a hard requirement), the pinned
  OSI-only toolchain, and the Go package `web` that embeds `dist/` for `app` and exposes
  `ClientManifest()` (the screen and action allowlists; D54).
- **Doesn't do:** any module's or core area's screens (their specs), authorization (the
  server), serving and the CSP (`api`), settings and branding values (`settings`), offline
  caching (Q-OFFLINE).
- **Spec:** [design/web-shell.md](design/web-shell.md).

### app: composition root
- **Purpose:** run the command the operator asked for over the core assembled from its
  configuration.
- **Owns:** the command table (`serve`, `migrate`, `check`, `openapi`, `version`, `help`;
  later `create-superuser`, `export`, `delete`, `rotate-secret`, `files verify`, `demo seed|reset` (D53) added by their parts) and
  the exit statuses 0/1/2/3; the assembly (config sections into each package's options;
  telemetry, registry, database, signals, core parts, modules, HTTP shell, in that order;
  `Provision`); the lifecycle (start order; SIGINT/SIGTERM; stop order HTTP → runnables →
  signals → modules → db → telemetry within `http.shutdown_timeout`, HTTP under two thirds
  of it); the module list (`modules.go`); the installer step (`secrets.Ensure`, migrations
  in order); the core-part seam each later core service plugs into; the tests only the root
  can run.
- **Doesn't do:** anything any assembled package does; packaging; configuration reload; a
  drain delay (not in M0).
- **Spec:** [design/app.md](design/app.md).

## Modules

Each gets its own spec (`plan/design/module-<name>.md`), written with the `add-module`
skill: `volunteers`, `registrations`, `incidents`, `afterreports`. Module names are letters
and digits only (`after-reports` is `afterreports` in code, schema and URLs).

`volunteers`: qualifications and their verified records, positions, membership, shifts and sign-ups (spec `design/module-volunteers.md`).

`hello` is the **reference module** (M0): the smallest module that touches the whole
contract, copied by every later module author and used to calibrate `moduletest.Conformance`.
It is useful to no burner, is enabled by default through Phase 1 and is turned off with
`BMS_MODULES_DISABLED=hello`. Spec: [design/module-hello.md](design/module-hello.md).
