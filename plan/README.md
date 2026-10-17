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
- **Last updated:** 2026-10-16
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
     Next: `signals` (design/README.md row 10, order 6 in 09-core-features.md), then the
     rest of the spec order in [09-core-features.md](09-core-features.md).
  3. CI on GitHub (`make check`), once the owner pushes the branch.

## Conventions

- Record decisions in `03-decisions.md` with a status (Accepted, Proposed or Superseded), not
  only in chat.
- When a question is answered, copy the answer into the document it affects and tick the box
  in `06-open-questions.md`.
- Everything in `plan/` except `private/` is public. Write it so any regional can read it.
