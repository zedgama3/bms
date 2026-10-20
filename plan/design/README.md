# Design Specs

One spec per abstraction or module from [../07-abstractions.md](../07-abstractions.md),
written with the `design-spec` skill (template in `.claude/skills/design-spec/SKILL.md`).
Module specs also follow the `add-module` skill. Code for an abstraction starts only once its
spec is **Approved**.

Statuses: Not started · Draft · In review · Approved · Implemented

The order follows dependencies: foundation first, then the core, then modules.

| # | Spec | Abstraction | Status |
|---|---|---|---|
| 1 | `lib-errs.md` | error kinds and wrapping (port from Sojourn, D11) | Implemented (ported, D11) |
| 1b | `lib-core.md` | clock, retry, validate, timeutil (port from Sojourn, D11) | Implemented (ported, D11; FieldError codes amendment implemented 2026-10-17) |
| 1c | `lib-secret.md` | a string that can't be printed (port from Sojourn, D11) | Implemented (ported, D11) |
| 2 | `model.md` | shared value types | Implemented (2026-10-17) |
| 3 | `config.md` | config loading and validation | Implemented (2026-10-17) |
| 4 | `module-contract.md` | the Module interface and registry | Implemented (2026-10-17) |
| 4b | `module-hello.md` | the reference module (M0) | Implemented (2026-10-17) |
| 5 | `db.md` | pool, transactions, per-schema migrations, error mapping | Implemented (2026-10-17) |
| 6 | `identity.md` | accounts, passwords, sessions, the superuser (M1) | Implemented (2026-10-18) |
| 7 | `authz.md` | roles, grants, permissions, Authorizer | Implemented (2026-10-18) |
| 8 | `people.md` | profiles, contacts, consent, export and deletion | Implemented (2026-10-18) |
| 9 | `departments-events.md` | departments, leadership, archiving, events (D24) | Implemented (2026-10-18) |
| 10 | `signals.md` | in-process domain events (D24) | Implemented (2026-10-17) |
| 11 | `notify.md` | notifier contract, e-mail | Implemented (2026-10-20) |
| 12 | `audit.md` | append-only audit log and its read-only spool | Implemented (2026-10-18) |
| 13 | `files.md` | attachments and storage | Draft (2026-10-20; redesign per D43) |
| 14 | `jobs.md` | scheduled and deferred work | Approved (2026-10-20) |
| 15 | `settings.md` | runtime settings and terminology | Implemented (2026-10-19) |
| 15b | `telemetry.md` | logs, traces, metrics, client error intake (D20) | Implemented (2026-10-17) |
| 15c | `privacy-tags.md` | field-level privacy tags and the visibility filter (D43) | Draft (2026-10-20) |
| 16 | `api.md` | router, middleware, error mapper, OpenAPI assembly | Implemented (2026-10-17) |
| 17 | `web-shell.md` | UI frame, API client, module screen registry | Implemented (2026-10-17) |
| 18 | `app.md` | composition root and lifecycle | Implemented (2026-10-17) |
| 19 | `module-volunteers.md` | volunteers module | Approved (2026-10-20; visibility amendment pending) |
| 20 | `module-registrations.md` | registrations module | Not started |
| 21 | `module-incidents.md` | incidents module | Not started |
| 22 | `module-after-reports.md` | after-burn reports module | Not started |
