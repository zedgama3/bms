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
| 1b | `lib-core.md` | clock, retry, validate, timeutil (port from Sojourn, D11) | Implemented (ported, D11); amendment pending (FieldError codes, api.md decision 25) |
| 1c | `lib-secret.md` | a string that can't be printed (port from Sojourn, D11) | Implemented (ported, D11) |
| 2 | `model.md` | shared value types | Approved |
| 3 | `config.md` | config loading and validation | Approved |
| 4 | `module-contract.md` | the Module interface and registry | Approved |
| 4b | `module-hello.md` | the reference module (M0) | Not started |
| 5 | `db.md` | pool, transactions, per-schema migrations, error mapping | Approved |
| 6 | `identity.md` | login providers and sessions | Not started |
| 7 | `authz.md` | roles, grants, permissions, Authorizer | Not started |
| 8 | `people.md` | profiles, contacts, consent, export and deletion | Not started |
| 9 | `departments-events.md` | departments, leadership, archiving, events (D24) | Not started |
| 10 | `signals.md` | in-process domain events (D24) | Approved |
| 11 | `notify.md` | notifier contract, e-mail | Not started |
| 12 | `audit.md` | append-only audit log | Not started |
| 13 | `files.md` | attachments and storage | Not started |
| 14 | `jobs.md` | scheduled and deferred work | Not started |
| 15 | `settings.md` | runtime settings and terminology | Not started |
| 15b | `telemetry.md` | logs, traces, metrics, client error intake (D20) | Approved |
| 16 | `api.md` | router, middleware, error mapper, OpenAPI assembly | Approved (2026-10-17) |
| 17 | `web-shell.md` | UI frame, API client, module screen registry | Approved (2026-10-17) |
| 18 | `app.md` | composition root and lifecycle | Draft (2026-10-17) |
| 19 | `module-volunteers.md` | volunteers module | Not started |
| 20 | `module-registrations.md` | registrations module | Not started |
| 21 | `module-incidents.md` | incidents module | Not started |
| 22 | `module-after-reports.md` | after-burn reports module | Not started |
