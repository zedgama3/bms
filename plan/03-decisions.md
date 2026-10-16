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

- **Status:** Accepted (2026-10-16) as a working name. The final name is Q-NAME.
- Repository: `github.com/zedgama3/bms` (Go module path `github.com/zedgama3/bms`). If the
  project moves to a GitHub org later, the module path changes with it; do that early.

## D7. Methodology carried over from Sojourn

- **Status:** Accepted (2026-10-16)
- Design specs before code, one purpose per abstraction, error-early rules, single-job
  agents with matched models, building and checking by different agents, and owner control
  of what's published (D16). Agents and skills were adapted from Sojourn's `.claude/`.

## D8. PostgreSQL is the only database

- **Status:** Proposed
- Unlike Sojourn, there's no pluggable storage backend. With many modules, each pluggable
  backend would multiply the work, and PostgreSQL is OSI-licensed and available everywhere.
- Modules write plain SQL against PostgreSQL (no ORM).

## D9. Modules never import each other

- **Status:** Proposed
- Two modules cooperate only through core contracts (for example, the people directory) and
  in-process **domain events** published through the core (for example, "camp registration
  approved"). Enforced by `make imports`.

## D10. Each module owns its own schema and migrations

- **Status:** Proposed
- Each module's tables live in its own PostgreSQL schema (for example `volunteers.shift`),
  with forward-only migrations shipped in the module. Modules may reference core tables
  (people, editions, departments) but never another module's tables.

## D11. Port the foundation libraries from Sojourn

- **Status:** Proposed
- Copy the `lib-errs`, `lib-core` (clock, retry, validate, timeutil) and `lib-secret` specs
  and code from Sojourn, relicensed under AGPL-3.0. The owner holds the copyright on both.
  They go through `go-reviewer` once, to remove anything specific to Sojourn.

## D12. Private details live only in `plan/private/`

- **Status:** Accepted (2026-10-16)
- The repository is public. Anything about a specific regional, a specific person or an
  existing vendor goes in `plan/private/`, which is gitignored. Real personal data never goes
  in fixtures, seeds or tests.

## D13. Docker Compose for development infrastructure

- **Status:** Proposed
- `deploy/compose` runs PostgreSQL, Mailpit, and Dex (profile `oidc`) for development and
  integration tests. Production packaging is a later decision.

## D14. Testing with the standard library and hand-written fakes

- **Status:** Proposed
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
- Reasons: authorization (per department, edition and record) has to live in the app
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
