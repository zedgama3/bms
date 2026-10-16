# Principles

These apply to every design and every line of code. Agents load them through `CLAUDE.md` and
the `go-conventions` skill. Where a later document conflicts with this one, this one wins
unless a decision in [03-decisions.md](03-decisions.md) explicitly changes it.

## Product principles

1. **Community-owned.** The code is AGPL-3.0 (D2). No single person or vendor should be a
   point of failure for a regional's operations. Every decision is written down in `plan/`
   so a new contributor can pick up the thread without asking anyone.
2. **Forkable by design.** Every feature is a **module** (D5). A regional can turn a module
   off, replace it, or add its own, without patching the core. Things that differ between
   regionals (names of departments, terminology, branding, policies, retention periods) are
   **configuration or data**, never code.
3. **Privacy and consent first.** These systems hold sensitive information about people:
   legal names, contacts, medical notes, incident reports. Collect only what a feature needs,
   restrict who can see it, record who read it, and delete it on a schedule. See the
   `privacy-data` skill.
4. **Radically inclusive.** The UI is accessible (WCAG 2.2 AA), mobile-first, and usable on a
   slow connection. Text is ready for translation from day one.
5. **Simple and boring.** Easy to contribute to: standard library first, few dependencies,
   plain SQL, clear docs. Easy to run: one service and one PostgreSQL database, on a small
   server, operated by a volunteer.
6. **Free to run, no lock-in.** Every core feature works on OSI-licensed software alone. Any
   external service that could be restricted later (an identity provider, an e-mail or SMS
   provider, file storage, payments) sits behind an interface we own, with at least one
   OSI-licensed or self-hostable implementation that we test.

## Engineering tenets

### 1. Abstract first, then code

- Before writing code for an area, write its **design spec** (`plan/design/`). A spec defines
  what the abstraction is responsible for, its interface, who may use it, how it fails, and
  how it's tested. Code is planned against the spec, not the other way round.
- **Each abstraction has exactly one clear purpose.** If describing what it does needs the
  word "and", it's probably two abstractions.
- Each spec also lists what the abstraction **does not do**, so the boundaries can't drift.
- Code that more than one abstraction needs goes into an **internal library**
  (`internal/lib/...`) or the **core**, so behavior stays consistent and a fix is made in one
  place.
- **Dependencies point inward.** Modules depend on core contracts, never on each other. Two
  modules cooperate only through core contracts and domain events. Only the composition root
  (`internal/app`) knows which modules exist. These rules are enforced by tooling (depguard
  and `make imports`), not by memory.

### 2. Error early and often

- **If something can fail, check it, return an error and test it.** No ignored errors, no
  `_ =` on a fallible call, no panics outside `main` start-up and true invariants.
- **Validate at the boundary.** Constructors validate their inputs and return
  `(T, error)`. Configuration is fully validated at start-up, and every problem is reported
  together. The process won't start half-configured.
- **Errors carry context and a kind.** Wrap with the operation (`fmt.Errorf("op: %w", err)`)
  and classify with the shared `errs` kinds, so callers can decide what to do without
  matching strings.
- **Every I/O call has a deadline** (from `context.Context`). Nothing waits forever.
- **Each failure mode listed in a design spec has a test.** A spec's failure-mode table is
  also the error-path test plan.
- Linters enforce this: errcheck, errorlint, wrapcheck, nilerr, rowserrcheck, sqlclosecheck,
  bodyclose, contextcheck.

### 3. Access is decided in one place

- Every read or write of a record is authorized by the core **authorizer**, from permissions
  the module declares. Handlers don't hand-roll permission checks.
- Reads of data marked sensitive are written to the **audit log**.

### 4. Build and check are separate jobs

- Code is written by one agent and checked by another (see
  [08-agent-workflow.md](08-agent-workflow.md)). Tests for error paths are written from the
  **spec**, not from the implementation, so they test what was intended rather than what was
  built.
- Mechanical checks (format, vet, lint, tests) run on the cheapest suitable model. Judgment
  work (design, review) runs on stronger ones.
