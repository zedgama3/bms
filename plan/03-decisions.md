# Decision Log

Statuses: **Accepted** (decided), **Proposed** (a recommendation waiting for sign-off),
**Superseded** (replaced by a later decision).

---

## D1. Build an open-source, community-built burn management system

- **Status:** Accepted (2026-10-16)
- A web application for running a regional burn, built in the open with the burner
  community, so that no regional depends on one maintainer or one vendor.

## D2. License: AGPL-3.0

- **Status:** Accepted (2026-10-16)
- Anyone who runs a modified copy as a service must publish their changes, so improvements
  made by one regional flow back to all of them.

## D3. Stack: Go, Vite + React + TypeScript, PostgreSQL

- **Status:** Accepted (2026-10-16)
- Chosen because it's simple and boring, which makes it easy for people to contribute.
  Same stack as Sojourn, so its conventions and agents carry over.

## D4. One self-hosted deployment per regional

- **Status:** Accepted (2026-10-16)
- No multi-tenancy. Each regional runs its own copy with its own database and
  configuration.

## D5. Features are compile-time modules

- **Status:** Accepted (2026-10-16)
- Each feature (volunteers, incidents, registrations, ...) is a Go package implementing the
  core **module contract**, and adds itself to a registry. `app` imports the modules it
  ships with; configuration turns them on or off. A fork adds a module with one import line.
- No runtime-loaded plugins.
- The contract's details (routes, migrations, permissions, navigation, jobs, events, UI) are
  designed in `plan/design/module-contract.md`.

## D6. Working name: bms

- **Status:** Superseded by D25 (2026-10-16). Was: Accepted (2026-10-16) as a working name.
- Repository: `github.com/zedgama3/bms` (Go module path `github.com/zedgama3/bms`). If the
  project moves to a GitHub org later, the module path changes with it; do that early.

## D7. Methodology carried over from Sojourn

- **Status:** Accepted (2026-10-16)
- Design specs before code, one purpose per abstraction, error-early rules, single-job
  agents with matched models, building and checking by different agents, and owner control
  of what's published (D16). Agents and skills were adapted from Sojourn's `.claude/`.

## D8. PostgreSQL only for now, behind a storage seam; no ORM

- **Status:** Accepted (2026-10-16)
- PostgreSQL is the only database we build for now. It's OSI-licensed, available
  everywhere, and its replication supports the offline plan (Q-OFFLINE).
- No ORM and no query builder: plain SQL.
- **Storage is its own section, so it can be replaced later without touching business
  logic.** In each module and core service:
  - The service declares the storage interface it needs, in its own terms (for example
    `ShiftStore`), with no SQL or driver types in it.
  - The PostgreSQL implementation lives in a `postgres/` subpackage, together with its SQL
    files and migrations.
  - Only `postgres/` packages and the core `db` package may import the database driver
    (enforced by depguard). Services, handlers and events never see SQL.
  - Replacing PostgreSQL later means writing new storage packages, not rewriting modules.
- Behavior tests run against the service with a hand-written fake store (D14); the
  `postgres/` package has its own integration tests.

## D9. Modules never import each other

- **Status:** Accepted (2026-10-16)
- Two modules cooperate only through core contracts (for example, the people directory) and
  in-process **domain events** published through the core (for example, "camp registration
  approved"). Enforced by `make imports`.

## D10. Each module owns its own schema

- **Status:** Accepted (2026-10-16)
- Each module's tables live in its own PostgreSQL schema (for example `volunteers.shift`),
  and the module is responsible for that schema. Modules may reference core tables (people,
  events, departments) but never another module's tables.
- How migrations are shipped and run is D17.

## D11. Port the foundation libraries from Sojourn

- **Status:** Accepted (2026-10-16)
- Copy the `lib-errs`, `lib-core` (clock, retry, validate, timeutil) and `lib-secret` specs
  and code from Sojourn, relicensed under AGPL-3.0. The owner holds the copyright on both.
  They go through `go-reviewer` once, to remove anything specific to Sojourn.

## D12. Private details live only in `plan/private/`

- **Status:** Accepted (2026-10-16)
- The repository is public. Anything about a specific regional, a specific person or an
  existing vendor goes in `plan/private/`, which is gitignored. Real personal data never goes
  in fixtures, seeds or tests.

## D13. Docker Compose for development infrastructure

- **Status:** Accepted (2026-10-16)
- `deploy/compose` runs PostgreSQL, Mailpit, and Dex (profile `oidc`) for development and
  integration tests. Production packaging is a later decision.

## D14. Testing with the standard library and hand-written fakes

- **Status:** Accepted (2026-10-16)
- `testing` only; no testify and no mocking frameworks (enforced by depguard). Integration
  tests sit behind the `integration` build tag.

## D15. The app handles login and permissions itself; no reverse proxy required

- **Status:** Accepted (2026-10-16)
- bms authenticates and authorizes in its own Go code (the core `identity` and `authz`
  abstractions), rather than handing authentication to a reverse proxy.
- Identity providers are pluggable: local accounts, any OIDC provider, and optionally a
  "trusted proxy header" provider for regionals that already run an authenticating proxy.
  The app owns sessions and permissions in every case.
- The binary serves the API and the built web UI itself, so no reverse proxy is required.
  Example configs for optional front ends (Cloudflare Tunnel, nginx, Caddy) ship in
  `deploy/`.
- Reasons: authorization (per department, event and record) has to live in the app
  anyway; participants register themselves, which a proxy could only support with a
  separate identity server to operate; modules declare their own public and private routes
  in one place, so a fork changes one thing; and trusting an identity header is dangerous
  if the app can be reached directly.

## D16. Claude may commit to local feature branches, but never push

- **Status:** Accepted (2026-10-16)
- The main Claude session may create a local branch (`feat/`, `fix/`, `chore/` or `docs/`
  plus a topic) and commit to it once checks are green and review has no must-fix findings.
  Commit messages explain why, and carry the attribution trailer.
- It never commits to `main` or merges into it, never pushes, never tags, and never rewrites
  commits the owner has already seen (no amend, rebase or force).
- Subagents never touch git, so building and checking stay separate from recording.
- The owner reviews the branch, merges it and pushes. Supersedes the "agents never commit"
  part of D7.

## D17. The installer applies migrations; each module checks its schema version

- **Status:** Accepted (2026-10-16)
- **Where they live:** each module's migrations sit in its `postgres/migrations/` folder,
  embedded in the binary. The schema changes in the same commit as the code that needs it,
  and a fork adds a module by adding a folder, with no separate install script to edit.
- **Who runs them:** an installer step, not the app at start-up. The same binary has a
  `bms migrate` command that an operator runs before starting a new version. It applies the
  core's migrations first, then each enabled module's, records what ran, and refuses to run
  on a read-only replica.
- **Each module stores its schema version.** The core keeps one row per module (and one for
  the core itself) with the schema version applied, written by the installer in the same
  transaction as the migration.
- **Each module checks its version when it initializes.** It compares the stored version
  with the version it was built for. If they differ, or the row is missing, it fails with an
  error that names the module, both versions, and says to run `bms migrate`. The app stops;
  nothing is changed. The app never changes a schema itself.
- Why not at start-up: during the event there are two copies and only one may write
  (Q-OFFLINE); start-up migrations would race between instances or fail on the replica;
  and operators should take a backup and choose when the schema changes. Development and
  tests can run `bms migrate` automatically.

## D18. Logins are local to each regional; trust between regionals comes later

- **Status:** Accepted (2026-10-16)
- There is no open, shared burner identity system; each tool has its own. bms supports local
  accounts and any OIDC provider a regional already runs (D15). No shared login is assumed.
- **Later: trust between regionals** (federation). A regional can declare which other
  regionals it trusts, and records such as volunteer credit can be recognized across them.
  For example, a group of regionals in one area might agree that volunteering at any of
  their burns counts at all of them. The mechanism (for example a document published at a
  `/.well-known/` URL, plus signed records) is Q-TRUST.
- **What we do now so it's possible later:** each installation has a stable identity (its
  public URL) and can hold a signing key; each person has a stable ID within an
  installation; and records that might be shared (for example completed shifts and hours)
  are kept as distinct, dated records rather than only as running totals.

## D19. Modules declare what they provide and use: capabilities, extension points, signals

- **Status:** Accepted (2026-10-16)
- **Problem:** features need each other's data (a person's page shows the departments they
  volunteer with and the events they're registered for), but modules never import each
  other (D9) or read each other's tables (D10). Without a planned mechanism, this turns into
  hidden coupling.
- **Shared keys.** People, departments and events belong to the core. Every module refers
  to them by ID, so data from different modules lines up without the modules knowing each
  other.
- **Contracts package.** `internal/contracts/<name>/` holds shared interfaces and data types
  only, with no implementation. Modules may import contracts; contracts import only `model`
  and `lib`. A fork adds a contract by adding a package.
- **Three ways to cooperate, each declared in the module's manifest:**
  1. **Extension points** (many contributors, the consumer defines the shape). The
     consumer asks "who has something to add?" and shows whatever comes back. Examples: the
     sections on a person's page, items on "my dashboard", what to include when exporting or
     deleting a person's data, what to do when a new event is created. Removing a module just
     removes its contribution. **This is the default.**
  2. **Capabilities** (one provider, a named service). A module `provides` a capability
     and others `require` it (start-up fails if it's missing, with a clear message) or
     `use` it optionally (the feature hides itself when it's missing). Example: a gate
     module optionally uses `volunteer-credit` to check early-arrival eligibility.
  3. **Signals** (domain events: something happened; renamed by D24). For reactions, not for
     reading data.
- **The registry checks everything at start-up:** every required capability has exactly one
  provider, every signal subscribed to has a publisher, and an admin screen shows the
  provides/uses graph.
- **Rules for contracts:** calls take batches (a list of person IDs, not one at a time), so
  pages don't make a call per row; the provider applies its own permission checks for the
  person asking, so nothing leaks through a contract; and contract changes are reviewed as
  breaking changes (Q-UPGRADE).
- Cross-module SQL joins and reporting across modules are not allowed through the database;
  reporting is a later extension point.

## D20. Observability: OpenTelemetry out, the Grafana stack as the reference; the UI reports its errors

- **Status:** Accepted (2026-10-16)
- **Why:** maintainers should see problems before people report them, and when someone does
  report one, find exactly what happened.
- **What bms emits.** Structured logs (JSON on stdout; a `text` format exists only for a
  developer's terminal, `BMS_LOG_FORMAT=text`, config.md decision 15; owner, 2026-10-16),
  plus traces, metrics and logs over OpenTelemetry (OTLP) when an endpoint is configured
  with the standard `OTEL_*` variables. bms speaks only OTLP, so a regional can use any
  compatible backend, or none.
- **Reference stack:** Grafana, Loki (logs), Tempo (traces), Prometheus (metrics, including
  rates, errors and durations derived from traces), and Alloy as the collector. Every piece
  is OSI-licensed. Development runs it with `make observe`; an example production setup
  ships in `deploy/`. It's recommended, not required.
- **Every request is traceable.** Each request gets a trace ID. Error responses and error
  screens show a short reference that a participant can quote, and that reference finds the
  trace and its logs.
- **The UI reports its own errors.** The web shell catches uncaught errors, unhandled promise
  rejections, crashed screens (React error boundaries) and failed API calls, and sends them
  in batches to a bms endpoint (`POST /api/client-errors`). The server validates them,
  limits the rate and size, and logs them with `source=client`. Browser API calls carry the
  trace context, so one trace covers the click and the server work.
- **Through bms, not straight to the collector,** so there's no extra public endpoint (it
  works through a tunnel), and the server enforces the privacy rules below.
- **Privacy (privacy-data skill).** Reports and logs carry no form values, request or
  response bodies, query strings, names, e-mail addresses or other personal data. URLs are
  reported as the route pattern (`/people/:id`), and the person only by internal ID. Log
  retention is set (proposed default: 30 days), and Grafana access is limited to the
  regional's maintainers.
- **Proactive alerts:** the example setup includes alert rules for error-rate and
  client-error spikes and for slow requests, sent to the maintainers by e-mail.
- Each copy (on-site and off-site, Q-OFFLINE) reports to its own collector; how the on-site
  copy's telemetry gets back afterwards is part of the switchover design.

## D21. A person logs in with their primary e-mail address

- **Status:** Accepted (2026-10-16). Refines Q-LOGIN.
- The username for a local account is the person's primary e-mail address, unique ignoring
  case. There's no separate username.
- The address is `personal` data: visible to the person and to roles with permission (for
  example admins and volunteer admins), never on lists other participants see. Everywhere
  else people are shown by playa name.
- Sign-up is open by default, with a setting to make it invitation only (Q-CORE1). Password
  reset by e-mail comes with notifications in M4 (Q-CORE2).

## D22. People can see that their personal data was viewed; how much is a setting

- **Status:** Accepted (2026-10-16), feature Later; the audit log records what it needs from
  the start.
- A person can see **that** someone viewed their personal data, never **who**. Each regional
  chooses how much to show, because there's no single right answer:
  - off;
  - that it was viewed, and when;
  - plus which fields (for example name, e-mail, phone);
  - plus the viewer's role or position title (not their name).
- The detailed record of who viewed it is available only to people with a separate permission,
  as an export for an internal process; that export is itself audited.
- So that any setting works later, every audited read records the fields viewed and the
  viewer's role or position title at that moment.

## D23. Separation of duties; a break-glass superuser is the only all-access account

- **Status:** Accepted (2026-10-16)
- **Superuser:** full access to the whole site, for development and emergencies ("break
  glass") only, never normal use. It isn't a role and can't be granted in the app: it's
  created and reset only by command. Every login alerts the maintainers and site admins;
  every action is audited and marked; its sessions are short; the UI shows a banner while
  it's in use.
- **Everyone else has separated duties.** No everyday role holds every permission. Site admin
  work is split into separate jobs (accounts, access, departments, events, settings, audit),
  which a regional assigns as it chooses.
- **Departments run themselves.** Each department has its own admins, who manage the
  department, its people and positions, and grant roles within it, and nowhere else. A
  separate permission archives departments and sets or resets their leadership, so that
  doesn't need the superuser.
- **A regional can forbid combinations.** Separation-of-duties rules are data: "a person
  holding role A may never hold permission B" (for example, board members never read
  incident reports). Rules are checked when granting and on every decision.
- Why: burns differ on who may see what, and some forbid particular combinations outright;
  one all-powerful admin role would make those rules impossible to keep.

## D24. "Event" means a gathering the regional puts on; in-process domain events are called signals

- **Status:** Accepted (2026-10-16); refined the same day (Q-CORE13). Replaces the word
  "edition" used in earlier drafts, which burners don't use and would misunderstand.
- An **event** is a gathering the regional puts on, with its own dates: usually a burn, but
  also, for example, an after-burn decompression party. A regional with a spring and a fall
  burn and a decompression party has three events that year.
- **Work is not an event.** Clearing land before a burn, build, strike, inventory and
  training are activities: they may relate to an event (land prep for the spring burn) or to
  none, and they belong to the module that runs them (for example `volunteers`), not to the
  core's events.
- **Departments are standing**, not tied to an event, and can be archived. Module data
  attaches to a department and, where it applies, an event; some work (for example, winter
  inventory) belongs to no event. Work for an event can fall outside its dates (land prep,
  build, strike).
- So "event" has one meaning in code and UI, the in-process publish/subscribe mechanism
  (formerly `events`, "domain events") is renamed **signals** (`core/signals`; for example the
  signal `volunteers.shift-completed`).

## D25. The name is BMS, for "Burn Management System"

- **Status:** Accepted (2026-10-16). Supersedes D6's "working name" status (Q-NAME, name part).
- The project is called **BMS** (Burn Management System). It's no longer a working name.
- The repository and Go module path stay `github.com/zedgama3/bms` for now. Whether the
  project moves to a GitHub org is still open (Q-NAME).

## D26. Module order after the core: volunteers, incidents, registrations, after-reports

- **Status:** Accepted (2026-10-16). Answers Q-MVP. The owner delegated the choice of order.
- After the core milestones M0–M4 ([09-core-features.md](09-core-features.md)), build the
  modules in this order:
  1. `volunteers`: the owner's stated direction; it exercises departments, events, positions
     and qualifications.
  2. `incidents`: the most sensitive data and the most important offline workflow, so it
     proves `privacy-data` and shapes the offline design early.
  3. `registrations`
  4. `after-reports`
- No pilot regional or date is tracked.

## D27. Offline is not in the MVP; its first workflows are known, its mechanism is deferred

- **Status:** Accepted (2026-10-16). Answers the scope of Q-OFFLINE; Q-OFFLINE stays open for
  the mechanism and is no longer Blocking for Phase 0.
- Offline can come later, after the MVP.
- The workflows most often needed offline are: incident management, volunteer management, and
  looking up a participant's personal and emergency information.
- The direction is kept as the likely approach: an on-site mirror that becomes the **single
  primary** during the event, with the other copy serving reads. The handover details (planned
  switchover, who runs it, how fast, which workflows keep working on site) stay open.
- For design later: an offline emergency-information lookup puts sensitive data on an on-site
  copy, so it has privacy implications to address when it's designed.

## D28. Every string is translation-ready; the default language is a per-regional setting

- **Status:** Accepted (2026-10-16). Answers Q-I18N.
- Languages: English today, Spanish soon, then possibly others.
- Some regionals may default to a language other than English, so the default language is a
  per-regional setting.
- Every string is translation-ready from day one.

## D29. Importing data from other systems is a later feature

- **Status:** Accepted (2026-10-16). Answers Q-IMPORT.
- Useful, but low priority and not in the MVP. Kept as a later feature.
- Imported IDs are already allowed by `model` (decision 5 in
  [design/model.md](design/model.md)).

## D30. Retention is configurable; the default is to keep data

- **Status:** Accepted (2026-10-16). Answers Q-RETAIN.
- Retention periods are configurable by each regional. The default is forever: data is kept
  until a regional sets a period.
- D20's 30-day retention applies to telemetry logs and is not affected.

## D31. Module UI lives in one folder per module under `web/`, registered at build time

- **Status:** Accepted (2026-10-16). Answers Q-WEBMOD.
- Each module has its own folder in `web/`, and a build-time registry imports it, mirroring
  the Go side. A fork adds a module folder and one import.
- `web-shell.md` (row 17 in [design/README.md](design/README.md)) owns the registry.

## D32. Forms are built in code; fields are fixed per module

- **Status:** Accepted (2026-10-16). Answers Q-FORMS.
- Registration and other forms are built in code for now, with fields fixed per module.
- Forms a regional can configure may come later.

## D33. Ticketing and payments are out of scope for now

- **Status:** Accepted (2026-10-16). Answers Q-TICKET and Q-PAY.
- Ticketing (including integration with ticketing vendors) is not in scope at this time.
- Payments (camp dues, fees, grants) are not in scope at this time.
