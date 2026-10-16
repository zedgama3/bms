# Roadmap

No dates yet. The real constraint is the next event a pilot regional wants to use it for
(Q-MVP).

## Phase 0: Foundations

Design comes before code. Specs are tracked in [design/README.md](design/README.md).

- [x] Working name, license, stack, hosting model, extension model (D2–D6)
- [x] Scaffold the repo: plan, agents, skills, `.golangci.yml`, Makefile, dev compose
- [ ] Answer the Blocking open questions for Phase 0 (Q-OFFLINE, Q-MVP, Q-IDP)
- [ ] Port `lib-errs`, `lib-core`, `lib-secret` from Sojourn (D11)
- [ ] **Design specs, foundation and core:** `model`, `config`, `module-contract`, `db`,
      `identity`, `authz`, `people`, `events`, `notify`, `audit`, `files`, `jobs`, `api`,
      `web-shell`, `app` (architect writes, owner approves)
- [ ] Implement the core with the build loop
- [ ] A "hello" module that exercises every part of the module contract, as the reference for
      module authors
- [ ] CI (once a public repo exists, Q-NAME)
- [ ] CODE_OF_CONDUCT.md and governance (Q-GOV1, Q-GOV2)

## Phase 1: First module (MVP)

- [ ] Spec and build the first module (proposed: `volunteers`)
- [ ] Packaging for a volunteer operator: one container image, compose file, backup and
      upgrade docs
- [ ] Pilot at one real event

## Phase 2: More modules

- [ ] `registrations` (camps, vehicles, art, calendar events)
- [ ] `incidents`, after a privacy review, and once Q-OFFLINE is settled
- [ ] `after-reports`

## Phase 3: Many regionals

- [ ] A second regional runs it, with at least one module of its own
- [ ] Module author guide, translation workflow, upgrade policy between versions
