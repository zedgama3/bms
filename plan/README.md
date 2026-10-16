# Planning

Planning workspace for **bms** (burn management system, a working name): an open-source,
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
  - Drafted the core feature list (09-core-features.md) for review.
  - Q-LOGIN: username and password only for now. Accepted D19 (modules declare the
    capabilities, extension points and events they provide and use).
- **Next steps:**
  1. The owner reviews [09-core-features.md](09-core-features.md) (Q-MVP).
  2. Port the foundation libraries from Sojourn (D11): `lib-errs`, `lib-core`, `lib-secret`.
  3. `architect` drafts `model` and `module-contract`, the spec everything else hangs off.

## Conventions

- Record decisions in `03-decisions.md` with a status (Accepted, Proposed or Superseded), not
  only in chat.
- When a question is answered, copy the answer into the document it affects and tick the box
  in `06-open-questions.md`.
- Everything in `plan/` except `private/` is public. Write it so any regional can read it.
