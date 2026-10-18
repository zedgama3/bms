# Planning

Planning workspace for **BMS** (Burn Management System; D25): an open-source,
community-built web application for running a regional burn. Each regional self-hosts its own
copy and can fork it. Features are modules.

## Documents

| File | Purpose |
|---|---|
| [00-principles.md](00-principles.md) | **Read first.** Product principles and engineering tenets |
| [01-context.md](01-context.md) | Why this exists and who it's for |
| [02-scope.md](02-scope.md) | Goals, what's in and out, the first modules, and the MVP |
| [03-decisions.md](03-decisions.md) | Decision log |
| [04-architecture.md](04-architecture.md) | Runtime shape, data ownership, how modules plug in |
| [05-roadmap.md](05-roadmap.md) | Phases and milestones |
| [06-open-questions.md](06-open-questions.md) | Questions that still need answers |
| [07-abstractions.md](07-abstractions.md) | **Map of abstractions:** purpose and boundaries of each, and the dependency rules |
| [08-agent-workflow.md](08-agent-workflow.md) | Agents, skills, models, and the loop from building to checking |
| [09-core-features.md](09-core-features.md) | What the core lets people do, in build order, and what's MVP |
| [design/](design/) | One design spec per abstraction or module (written before any code) |
| [notes/](notes/) | Research notes |
| `private/` | **Gitignored.** Notes about a specific regional, people or vendors. Never committed. |

## Status

- **Phase:** 0, foundations
- **Last updated:** 2026-10-18
- **Done 2026-10-16:**
  - Repo scaffolded: plan, agents, skills, lint rules, dev compose. Methodology carried over
    from Sojourn (D7).
  - D15 accepted: the app handles login and permissions itself; no reverse proxy required.
  - D16 accepted: Claude may commit to local feature branches, but never pushes.
  - Repository: `github.com/zedgama3/bms`; GitHub CLI installed.
  - Accepted D8 (PostgreSQL only for now, behind a storage seam), D9, D10, D11, D13, D14.
    Accepted D17 (the installer applies migrations; each module checks its schema version)
    and D18 (logins local to each regional; trust between regionals later).
  - Drafted the core feature list (09-core-features.md) for review, then planned it in
    detail: users, milestones M0–M4, what "done" means for each feature, the order of specs,
    and six questions for the owner (Q-CORE1–6), which the owner answered: D21 (log in with
    the primary e-mail address), D22 (people can see that their data was viewed; how much is
    a setting), no sub-teams (positions belong to `volunteers`), and Q-QUAL opened for
    training, certificates and age requirements.
  - Owner's further answers (Q-CORE7–12): optional, multiple emergency contacts; D23
    (separation of duties; a break-glass superuser; departments run themselves); D24 ("event"
    means a gathering the regional puts on, usually a burn, replacing "edition"; departments are standing and archivable; domain
    events renamed signals). Added the `burn-domain` skill, kept current as the owner explains
    how burns work.
  - Ported the foundation libraries from Sojourn (D11).
  - D20: observability with OpenTelemetry; Grafana, Loki, Tempo, Prometheus and Alloy in the
    dev compose (`make observe`); the web UI reports its errors.
  - Q-LOGIN: username and password only for now. Accepted D19 (modules declare the
    capabilities, extension points and signals they provide and use).
  - The owner answered `model`'s two open questions: event start and end are date-times, with
    schedule items such as gate hours inside events left to `departments-events`; e-mail
    uniqueness ignores case only.
  - Owner's answers recorded 2026-10-16: the name is BMS, Burn Management System (D25,
    supersedes D6); module order after the core is volunteers, incidents, registrations,
    after-reports (D26); offline is not MVP, and Q-OFFLINE is no longer Blocking (D27);
    default language per regional, every string translation-ready (D28); importing data is a
    later feature (D29); retention is configurable, default forever (D30); module UI folders
    in `web/` (D31); forms built in code (D32); ticketing and payments out of scope (D33).
    Q-QUAL: department leads verify their department's qualifications (shape still open).
  - Design specs approved by the owner: `model` is Approved, and `module-contract` is Approved
    2026-10-16 (design/README.md).
- **Next steps:**
  1. Done 2026-10-16: the owner agreed [09-core-features.md](09-core-features.md)
     (milestones M0–M4, done criteria, spec order). `architect` drafted `model`, now Approved.
  2. Done 2026-10-16: `config` is Approved (design/README.md row 3).
     Done 2026-10-16: `db` is Approved (design/README.md row 5, order 4 in
     09-core-features.md).
     Done 2026-10-16: `telemetry` is Approved (design/README.md row 15b, order 5 in
     09-core-features.md).
     Done 2026-10-16: `signals` is Approved (design/README.md row 10, order 6 in
     09-core-features.md).
     Done 2026-10-17: `api` is Approved (design/README.md row 16, order 7 in
     09-core-features.md), with the owner's answers to its four open questions (api.md
     decisions 24–27).
     Done 2026-10-17: `web-shell` is Approved (design/README.md row 17, order 8 in
     09-core-features.md), with the owner's answers recorded in the spec (decisions 2, 7, 15,
     17, 18, 21, 22).
     Done 2026-10-17: `app` is Approved (design/README.md row 18, order 9 in
     09-core-features.md), with the owner's answers to its four open questions.
     Done 2026-10-17: `module-hello` is Approved (design/README.md row 4b); every M0 spec is
     now Approved.
     Done 2026-10-17: M0 implementation round — owner approved 21 spec corrections and the
     module-contract moduletest amendment (2026-10-17).
     Done 2026-10-17: M0 implemented on branch feat/m0 (every M0 spec Implemented; `bms migrate`,
     `check` and `serve` verified against the dev database; Playwright smoke passes against the
     real server).
     Done 2026-10-18: `audit` (order 10) and `identity` (order 11) are Approved (D35).
     Done 2026-10-18: M1 implemented on feat/m1.
     Done 2026-10-18: `authz` and `people` are Approved (D36), with the owner's answers to
     their recommendations.
     Done 2026-10-18: M2 implemented on feat/m1 (authz and people Implemented; access, people
     and audit screens built; `bms export` and `bms delete` built; e2e run against the real
     server passes). Not pushed.
     Next milestone: M3, specs `departments-events`.
     Open follow-ups (all waiting on the owner or architect):
     - (a) Confirm D34 as D20's debug-level exception.
     - (b) The M3 part name `departments-events` is not a core area name for `module.IsCoreArea`
       (split into two parts, or amend F28).
     - (c) Done 2026-10-17: `scripts/e2e.sh` has a readonly mode. `BMS_E2E_MODE=readonly`
       migrates a throw-away database, freezes it with `ALTER DATABASE ... SET
       default_transaction_read_only = on` and runs the read-only Playwright scenario.
     - (d) CI workflow `.github/workflows/ci.yml` is written (2026-10-17): a `make check` job and
       an integration-test job with a PostgreSQL 17 service. It runs once the owner pushes the
       branch; waiting for the first push.
     - (e) Done 2026-10-18. audit: entries spooled inside a caller's transaction can outlive a
       rollback. Needs a db seam (for example `db.TxMode(ctx)`) and an audit amendment, both by
       the architect.
       Decided 2026-10-18 (D36): the architect amends `db` and `audit` with a seam (for example
       `db.TxMode(ctx)`), so audit never spools while ctx carries a transaction.
     - (f) Done 2026-10-18. audit F36: an import insert that fails Invalid keeps retrying the
       file forever. Recommendation: set it aside as `.failed`.
       Decided 2026-10-18 (D36): an import insert that fails with Invalid sets the file aside
       as `.failed`.
     - (g) Done 2026-10-18. api: a 409 Conflict carries no `fields`, so sign-up `taken` can't
       name the field. Recommendation: the mapper emits fields for conflict too. The web already
       copes either way.
       Decided 2026-10-18 (D36): the mapper also emits `fields` for a 409 Conflict, so sign-up
       `taken` names the e-mail field.
     - (h) Done 2026-10-18. identity limiter defaults: the per-client-address limit of 10
       failures per 15 minutes may lock out a shared gate uplink. Recommendation: raise the
       address bucket to about 100 and keep the per-account limit at 10.
       Decided 2026-10-18 (D36): the per-client-address bucket rises to about 100 failures per
       15 minutes; the per-account bucket stays at 10.
     - (i) Done 2026-10-18. Two behaviours added beyond the identity spec were pending the
       owner's review: ChangePassword shares the sign-in limiter buckets, and IPv6 addresses are
       keyed by /64.
       Decided 2026-10-18 (D36): both are kept. No longer pending.
     - (m) Approve the D36 (e) amendment to `db.md` and `audit.md` (`db.TxMode`; audit never
       spools inside a transaction). Recommendation: yes, and `audittest.Fake` refuses a Record
       inside a Read tx.
     - (n) authz: a SoD rule or revoke can leave no effective access admin. Recommendation:
       refuse with `last_access_admin`.
     - (o) authz: should Seed record an audit entry when an upgrade adds a permission to a starter
       role? Recommendation: yes.
     - (p) authz/people: should DeletePerson publish `access.grant-revoked` for removed grants?
       Recommendation: yes.
     - (q) authz: department stewards can't list grants. Recommendation: give them
       `access.grants.read` scoped to their department.
     - (r) people: a language-only `PUT /me` is held to the required fields. Recommendation:
       exempt it.
     - (s) people: should the consent audit record the purpose and direction? Recommendation:
       yes.
     - (t) people: clear a walk-on's `contact_email` when an account is linked. Recommendation:
       yes.
     - (u) people: audit reads of deletion-request notes. Recommendation: yes.
     - (v) people: data rights for an account holder with no `core.person` row. Recommendation:
       run the identity and authz steps anyway.
     - (w) architect, app spec gaps: commands don't start modules, so `people.person-deleted`
       from `bms delete` reaches no module, and PersonData must work on an unstarted module.
       Migrate's seed must reuse serve's split sequence once `jobs` arrives. app.md vs db.md
       notify set drift.
     - (x) Earlier still-open items: (a) D34 as D20's debug-level exception; (b) the M3 part
       name `departments-events` vs `IsCoreArea`; (j) notify drift (merge into w); (k) the
       password hint hard-codes 12; (l) the common-password list barely applies at a
       12-character minimum; and CI waiting for the first push.
  3. Local dev database (half-reset): the `authz` and `hello` version-2 rows in
     `core.schema_migration` have to be deleted by the owner, then `make migrate` run again.
     The alternative is `make down && make up`.
  4. CI on GitHub (`make check`): workflow `.github/workflows/ci.yml` is written (2026-10-17);
     waiting for the owner to push the branch so it runs.

## Conventions

- Record decisions in `03-decisions.md` with a status (Accepted, Proposed or Superseded), not
  only in chat.
- When a question is answered, copy the answer into the document it affects and tick the box
  in `06-open-questions.md`.
- Everything in `plan/` except `private/` is public. Write it so any regional can read it.
