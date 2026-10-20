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
- **Last updated:** 2026-10-20
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
     Done 2026-10-18: `departments-events` is Approved (D37), with the owner's answers to its
     six recommendations; its 13 proposed changes elsewhere are being applied.
     Done 2026-10-18: M3 implemented on feat/m1 (`departments-events` Implemented; core parts
       `departments` (6) and `events` (7)). Commits: 8551724 packages; 63c1297 app wiring;
       bca3d5b screens; 5195624 the D37 follow-ups (m)–(p) and (r)–(v), now Done. Verification:
       all Go checks pass (unit, race and integration tests; lint on three tag sets; import
       rules); `pnpm check` and openapi-check pass; Playwright on a fresh database passes 100
       default, 2 hello-disabled and 6 readonly. Accepted deviations: the signal payload key
       `copied_from` is renamed `copied-from` to fit the signals key grammar (specs updated);
       the audit Ref for an event kind uses a fixed ID derived from sha256 of the key, with
       Reason `key:<key>`; RunSetup with no providers records no `events.setup.ran` entry; most
       failure modes are tested in integration tests rather than in unit tests over an
       in-memory store (as in M2). Not pushed.
     Done 2026-10-19: M3 is recorded as done; the D38 answers are applied in commit c5056d5
     on feat/m1.
     Done 2026-10-19: `settings` and `notify` are Approved (D39), with the owner's answers.
     Next milestone: M4 (see 09-core-features.md). The M4 build is next.
     Done 2026-10-20: M4 partly implemented on feat/m1: `settings` and `notify` Implemented
       (the e-mail outbox and SMTP sender, templates with admin overrides, password reset by
       e-mail, the people deletion-request notice, the superuser sign-in alert, the notify admin
       screens (templates, outbox) and the forgotten-password screens). Still open in M4:
       `volunteers` (module-volunteers.md, Not started); `jobs.md` and `files.md` (Not started;
       the settings logo waits for files, D39). M4 stays open.
     Owner's answers 2026-10-20 (D40): the M4 review questions (Q-AMEND1, Q-RESET-RATE,
       Q-RESET-LIFE, Q-SETTINGS-REVIEW, Q-MAILPIT-AUTH, Q-WEB-REVIEW) approved with
       recommendations; code of conduct Contributor Covenant (Q-GOV1); DCO, no CLA (Q-GOV2);
       semver for the module contract (Q-UPGRADE); publication at github.com/zedgama3/bms, no
       org move planned (Q-NAME); qualifications facts recorded, shape for the `volunteers`
       spec (Q-QUAL).
     Open follow-ups (all waiting on the owner or architect):
     - (a) Confirm D34 as D20's debug-level exception.
     - (b) The M3 part name `departments-events` is not a core area name for `module.IsCoreArea`
       (split into two parts, or amend F28).
       Decided 2026-10-18 (D37): two parts, `departments` (6) and `events` (7), each a reserved
       core area; the spec file stays one; `jobs`, `notify`, `files` become parts 8–10. F28 is
       not amended.
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
       Decided 2026-10-18 (D37): approved, and `audittest.Fake` refuses a Record inside a Read tx.
     - (n) authz: a SoD rule or revoke can leave no effective access admin. Recommendation:
       refuse with `last_access_admin`.
       Decided 2026-10-18 (D37): refuse with `last_access_admin`.
     - (o) authz: should Seed record an audit entry when an upgrade adds a permission to a starter
       role? Recommendation: yes.
       Decided 2026-10-18 (D37): yes.
     - (p) authz/people: should DeletePerson publish `access.grant-revoked` for removed grants?
       Recommendation: yes.
       Decided 2026-10-18 (D37): yes.
     - (q) authz: department stewards can't list grants. Recommendation: give them
       `access.grants.read` scoped to their department.
       Decided 2026-10-18 (D37): yes, scoped to their department.
       Reopened 2026-10-18: still open. `department-steward` is site-scoped, so "access.grants.read
       in their department" can't be expressed. Options: give steward site-wide
       `access.grants.read`, or take another route to the Leadership card. Recommendation:
       site-wide, since stewards already manage every department.
       Decided 2026-10-19 (D38): site-wide `access.grants.read` for department-steward. This
       supersedes the D37(q) scope to their department, which can't be expressed for a
       site-scoped role.
     - (r) people: a language-only `PUT /me` is held to the required fields. Recommendation:
       exempt it.
       Decided 2026-10-18 (D37): exempt.
     - (s) people: should the consent audit record the purpose and direction? Recommendation:
       yes.
       Decided 2026-10-18 (D37): yes.
       Spec amendment pending owner approval (2026-10-18): the consent Reason `granted:<purpose>`
       / `withdrawn:<purpose>`. Recommendation: approve.
       Decided 2026-10-19 (D38): spec amendment approved; the consent Reason is
       `granted:<purpose>` / `withdrawn:<purpose>`.
     - (t) people: clear a walk-on's `contact_email` when an account is linked. Recommendation:
       yes.
       Decided 2026-10-18 (D37): yes.
     - (u) people: audit reads of deletion-request notes. Recommendation: yes.
       Decided 2026-10-18 (D37): yes.
       Spec amendment pending owner approval (2026-10-18): one `people.request.read` entry per
       request, with Subject = the requester. Recommendation: approve.
       Decided 2026-10-19 (D38): spec amendment approved; one `people.request.read` entry per
       returned request, with a note and Subject = the requester.
     - (v) people: data rights for an account holder with no `core.person` row. Recommendation:
       run the identity and authz steps anyway.
       Decided 2026-10-18 (D37): run the identity and authz steps anyway.
     - (w) architect, app spec gaps: commands don't start modules, so `people.person-deleted`
       from `bms delete` reaches no module, and PersonData must work on an unstarted module.
       Migrate's seed must reuse serve's split sequence once `jobs` arrives. app.md vs db.md
       notify set drift. The notify set drift is resolved by notify's db.md proposed change
       (D39).
     - (x) Earlier still-open items: (a) D34 as D20's debug-level exception; (j) notify drift,
       resolved 2026-10-19 by notify's db.md proposed change (D39); (k) the
       password hint hard-codes 12; (l) the common-password list barely applies at a
       12-character minimum; and CI waiting for the first push.
     - (y) M3 review: `events.setup.ran` is lost when the request deadline expires mid-run.
       Recommendation: record with `context.WithoutCancel` and a short deadline.
       Decided 2026-10-19 (D38): record `events.setup.ran` with `context.WithoutCancel` and a
       short deadline, so it survives an expired request deadline.
     - (z) M3 review: the setup metric outcome for a provider returning Timeout. Recommendation:
       map it to `timeout`.
       Decided 2026-10-19 (D38): a provider returning kind Timeout gets metric outcome `timeout`.
     - (aa) M3 review: a late provider success is reported as a timeout. Recommendation: report
       what happened.
       Decided 2026-10-19 (D38): a late provider success is reported as what happened, success
       with its items.
     - (ab) M3 review: a panic is logged twice. Recommendation: log once.
       Decided 2026-10-19 (D38): a provider panic is logged once.
     - (ac) M3 review: `RunSetup` checks permission before input. Recommendation: validate first.
       Decided 2026-10-19 (D38): RunSetup validates input before checking permission.
     - (ad) M3 review: an empty zone gives code `required` where the spec says `malformed`.
       Recommendation: align the spec on `required`.
       Decided 2026-10-19 (D38): the spec aligns on `required` for an empty zone.
     - (ae) M3 review: `departments` maps conflicts by kind, not by constraint name.
       Recommendation: match by name.
       Decided 2026-10-19 (D38): `departments` matches conflicts by constraint name.
     - (af) Web: page titles are literal "Departments" and "Events", since terms have no plural
       or title-case form. Recommendation: add plural and title term keys in settings.
       Decided 2026-10-19 (D38): add plural and title-case term keys. This belongs to the
       settings spec (M4) and is deferred to it. Answered 2026-10-19 (D39): four term forms
       are stored: singular, plural, singular_title, plural_title.
     - (ag) Web: the "See roles here" link has no role filter, because `GrantList` filters by
       role ID. Recommendation: let `GrantList` accept a role key.
       Decided 2026-10-19 (D38): `GrantList` accepts a role key, and "See roles here" filters
       to department-admin.
     - (ah) Web: a renamed builtin kind still shows its catalogue label elsewhere, a
       contradiction in the spec. Recommendation: show the stored name once renamed.
       Decided 2026-10-19 (D38): once a builtin kind is renamed, its stored name is shown
       everywhere.
     - (ai) Web e2e lacks the "grant department-admin to a second person" and "copy set-up at
       phone width" flows. Recommendation: add them when a module contributes set-up.
       Decided 2026-10-19 (D38): add the e2e flows once a module contributes set-up. Deferred
       until then.
     - (aj) Kind names are not unique. Recommendation: allow it.
       Decided 2026-10-19 (D38): kind names may repeat. No change.
     - (ak) Web: whether a builtin event kind has been renamed is decided in the browser,
       against the seeded English names. Recommendation: the server sends `renamed`.
     - (al) Web: the kind-rename field is pre-filled with the translated label, so in another
       language a no-change save counts as a rename. Recommendation: pre-fill with the stored
       name, or skip unchanged saves.
     - (am) `constraintOf` is duplicated in events, people and authz. Recommendation:
       `db.ConstraintOf`.
  3. Local dev database (half-reset): the `authz` and `hello` version-2 rows in
     `core.schema_migration` have to be deleted by the owner, then `make migrate` run again.
     The alternative is `make down && make up`. Still half-reset on 2026-10-18; the owner's
     command is unchanged.
  4. CI on GitHub (`make check`): workflow `.github/workflows/ci.yml` is written (2026-10-17);
     waiting for the owner to push the branch so it runs.

## Conventions

- Record decisions in `03-decisions.md` with a status (Accepted, Proposed or Superseded), not
  only in chat.
- When a question is answered, copy the answer into the document it affects and tick the box
  in `06-open-questions.md`.
- Everything in `plan/` except `private/` is public. Write it so any regional can read it.
