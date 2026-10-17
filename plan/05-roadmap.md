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
      `web-shell`, `app` (architect writes, owner approves)
- [ ] Implement the core with the build loop
- [x] Implement the `lib-core` `validate` amendment: `validate.FieldError` gains `Code` and
      `Params`, each check sets its code, the `Code*` constants are added (lib-core.md,
      api.md decision 25). Must land before `httpx`.
- [ ] A "hello" module that exercises every part of the module contract, as the reference for
      module authors
- [ ] CI on GitHub Actions running `make check` (repository: `github.com/zedgama3/bms`)
- [ ] CODE_OF_CONDUCT.md and governance (Q-GOV1, Q-GOV2)

## Phase 1: First module (MVP)

- [ ] Spec and build the first module (`volunteers`, D26)
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
