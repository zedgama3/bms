# Roadmap

No dates yet. No pilot regional or date is tracked (Q-MVP, answered 2026-10-16: module order
in D26).

## Phase 0: Foundations

Design comes before code. Specs are tracked in [design/README.md](design/README.md).

- [x] Working name, license, stack, hosting model, extension model (D2–D6)
- [x] Scaffold the repo: plan, agents, skills, `.golangci.yml`, Makefile, dev compose
- [x] Answer the Blocking open questions for Phase 0 (Q-MVP answered, D26; Q-OFFLINE is no
      longer Blocking, D27; Q-IDP answered, D18)
- [x] Agree the core feature list and its MVP items (09-core-features.md; owner, 2026-10-16)
- [x] Port `lib-errs`, `lib-core`, `lib-secret` from Sojourn (D11)
- [x] Development observability stack: Grafana, Loki, Tempo, Prometheus, Alloy (D20)
- [ ] **Design specs, foundation and core:** `model`, `config`, `module-contract`, `db`,
      `identity`, `authz`, `people`, `departments-events`, `signals`, `notify`, `audit`, `files`, `jobs`, `api`,
      `web-shell`, `app` (architect writes, owner approves). Update 2026-10-20 (D43): `jobs`
      and `volunteers` (module-volunteers.md) are Approved; `files` is not approved and is
      being redesigned as a file service, not database storage. Update 2026-10-20 (D44):
      `files` and `privacy-tags` are Approved; the `jobs` amendments are approved and being built.
- [ ] Implement the core with the build loop (M0 done 2026-10-17: model, config, telemetry, db, signals, module contract, httpx/api, web shell, app; M1 done 2026-10-18: audit, identity; M2 done 2026-10-18: authz, people, plus the audit screen; M3 done 2026-10-18: departments, events; M4 partly done as of 2026-10-20: e-mail and password reset by e-mail implemented on feat/m1 (settings and notify Implemented; the forgotten-password and notify admin screens built); the D41 reset-limit settings implemented (commit 3ba4c6c); `jobs` Implemented on feat/m1 (commit 42a101d: core area, /api/jobs routes, /jobs screen); still open in M4: `volunteers`, `files` (Approved 2026-10-20, D44), privacy tags (Approved 2026-10-20, D44); the `jobs` follow-ups from D44 are being built; M4 stays open)
- [x] Implement the `lib-core` `validate` amendment: `validate.FieldError` gains `Code` and
      `Params`, each check sets its code, the `Code*` constants are added (lib-core.md,
      api.md decision 25). Must land before `httpx`.
- [x] A "hello" module that exercises every part of the module contract, as the reference for
      module authors
- [ ] CI on GitHub Actions running `make check` (repository: `github.com/zedgama3/bms`) (workflow written 2026-10-17; runs once pushed)
- [x] CODE_OF_CONDUCT.md and governance (Q-GOV1, Q-GOV2 → D40; the reporting address is set to conduct@zg3.net, D43)

## Phase 1: First module (MVP)

- [ ] Spec and build the first module (`volunteers`, D26). Spec Approved 2026-10-20 (D43); not yet built.
- [ ] Privacy tags (`privacy-tags.md`, Approved 2026-10-20, D44) built before the volunteers module's services, in the same milestone; `hello` is retagged with them.
- [ ] Later: identity, notify and people adopt privacy tags and per-group visibility settings (when a regional asks for a people-level choice, privacy-tags Q-PRIV2).
- [ ] Packaging for a volunteer operator: one container image, compose file, backup and
      upgrade docs
- [ ] Pilot at one real event

## Phase 2: More modules

Order after the core, from D26:

- [ ] `incidents` (module 2), after a privacy review, and once Q-OFFLINE is settled
- [ ] `registrations` (module 3): camps, vehicles, art, calendar events
- [ ] `after-reports` (module 4)

## Phase 3: Many regionals

- [ ] A second regional runs it, with at least one module of its own
- [ ] Module author guide, translation workflow, upgrade policy between versions
