# CLAUDE.md

**bms** (burn management system, a working name) is an open-source, community-built web
application for running a regional burn: volunteers and shifts, incidents, camp, RV, art and
event registration, after-burn reports, and whatever regionals need next. Each regional
self-hosts its own copy and can fork it. Every feature is a **module** that can be turned
on, swapped or added without changing the core.

Stack: Go backend, Vite/React/TypeScript frontend, PostgreSQL. License: AGPL-3.0.

**State:** planning and design (Phase 0). There's no application code yet. Start with
`plan/README.md`.

## Non-negotiables

1. **Read `plan/00-principles.md` before designing or writing code.** In short:
   - **Abstract first.** No code for an abstraction until its spec in `plan/design/` is
     Approved. One purpose per abstraction; list what it does *not* do.
   - **Error early and often.** Every fallible call is checked, wrapped with context and
     classified with `lib/errs`. Constructors validate. Every I/O call has a context deadline.
     Every failure mode in a spec has a test.
   - **Dependencies point inward.** Modules depend only on core contracts, `model` and `lib`.
     **A module never imports another module.** Only `internal/app` imports modules. See the
     layer rules in `plan/07-abstractions.md`.
   - **Privacy by default.** Personal and sensitive data (incidents, medical, legal names,
     contact details) follow the `privacy-data` skill: least access, audited reads, retention.
   - **The core works on OSI-licensed software alone.** Anything else is an optional module.
2. **Keep private details out of the code and public docs.** Anything about a specific
   regional, a specific person or an existing vendor goes only in `plan/private/`, which is
   gitignored. Never put real personal data in fixtures, seeds or tests.
3. **Commit only to local feature branches; never push** (D16). The main session may create a
   branch (`feat/<topic>`, `fix/<topic>`, `chore/<topic>`, `docs/<topic>`) and commit to it,
   once checks pass and review has no must-fix findings. Never commit to `main`, merge into
   it, push, tag, or rewrite commits the owner has already seen. Subagents never touch git.
   The owner reviews the branch, merges and pushes.

## Agents and skills

Use the project agents in `.claude/agents/`. Each has one job and a model matched to it.
Building and checking are always done by different agents. The full roster and loop are in
`plan/08-agent-workflow.md`.

- Design an abstraction, a module or a spec → `architect`
- Implement an approved spec → `go-implementer`, plus `test-author` in parallel; SQL and
  migrations → `sql-author`; UI → `frontend-implementer`
- Run a check (fmt, vet, lint, test, build) → `check-runner`. Don't spend a large model on it.
- Review a Go/SQL change → `go-reviewer`; review a UI change → `web-reviewer`
- Record a decision or answer, or update plan status → `plan-keeper`

Skills: `go-conventions` (before writing any Go), `design-spec`, `build-loop` (how the main
session orchestrates the above), `add-module`, `privacy-data`, `postgres-sql`,
`plan-maintenance`.
