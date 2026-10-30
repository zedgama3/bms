---
name: design-spec
description: Template and acceptance checklist for an abstraction's or module's design spec in plan/design/. Load before writing a spec (architect), before reviewing code against one (go-reviewer, web-reviewer), or when judging whether a spec is ready to approve.
---

# Design specs

A spec defines one abstraction or module from `plan/07-abstractions.md` well enough that one
agent can implement it, another can test it, and a third can review it **without asking the
author anything**.

File: `plan/design/<name>.md` (modules: `module-<name>.md`). Status is tracked in
`plan/design/README.md`.

## Template

```markdown
# <Name>

- **Status:** Draft | In review | Approved | Implemented
- **Package:** internal/<path>   **UI:** web/src/<path> (if any)
- **Depends on specs:** <list>   **Used by:** <consumers>

## Summary
The contract other specs rely on, in at most 80 lines: the purpose; the exported names
(types, interfaces, functions, constants) each with a one-line signature or meaning; the key
decisions, one line each with their number; the errs kinds and sentinels a caller must
handle; routes, signals, permissions, settings and config keys this spec owns; and what it
expects of each spec it depends on. Every later change to the spec updates this section in
the same edit.

## Purpose
One sentence. No "and".

## Responsibilities
- What this abstraction owns (bullets).

## Not responsible for
- What it explicitly does not do, and who does it instead.

## Sub-abstractions
| Name | Purpose (one line) | Notes |

## Interface
```go
// Go signatures with doc comments. ctx first, error last.
```
For each method: which consumer needs it, plus its pre- and post-conditions.

## Types
Value types this abstraction defines (or "uses model.X").

## Data
Tables (schema-qualified), owned by this abstraction. For each personal-data column: its
sensitivity level and retention rule (`privacy-data`).

## Permissions
| Permission | Allows | Scoped by (department / event / own records) | Default roles |

## Signals
Published: name, when, payload (no sensitive fields). Subscribed: name, what it does, and
what happens if handling fails.

## Settings
| Key | Type | Default | Valid range | Who can change it |

## UI (if any)
Screens, who sees each, the actions on them, and their empty, error and forbidden states.

## Behavior
Ordering, concurrency safety, idempotency, retries (who retries: this abstraction or its
caller?), lifecycle (start/stop).

## Failure modes
| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F1 | CreateShift | caller isn't a lead of the department | Permission | show forbidden | TestCreateShift_NotLead_ReturnsPermission |

## Lock-in check
External technology wrapped (if any) · the OSI-licensed or self-hostable implementation that
proves the interface is portable.

## Test plan
Unit (with fakes) · permission tests · integration (build tag, which container) ·
conformance (if a contract).

## Open questions
Only real unknowns, each with a proposed answer.

## Proposed changes elsewhere
Edits this spec needs in 07-abstractions.md or other specs (the architect lists them;
`spec-editor` applies those whose target exists once the owner approves).
```

Sections that don't apply say "None" with a reason, rather than being dropped.

## Amendments

When an amendment is implemented, fold it into the spec body and move its narrative to
`plan/design/history/<spec>.md`.

- Rewrite the Summary, Interface, Data, Permissions, Signals, Settings, UI, Behavior, Failure
  modes and Test plan so they state the current rules directly. Where a later amendment
  overrides an earlier one, keep only the current rule. A short source tag such as "(V3, D57)"
  is fine for traceability.
- The history file starts with a one-line header saying what it is. It holds the amendment
  narratives, the reasons, what the reviews found and the superseded wording, verbatim.
- In the spec, leave one line (an "Amendments" section) pointing to the history file.
- Keep decision numbers, Q-ids, F-numbers and test names: other files cite them.
- No line in a spec is longer than 2000 characters (aim for under 500): use lists for dense
  paragraphs.

## Reading other specs

Read the **Summary** of each spec yours depends on, not the whole file. Open a specific
section (an interface, a decision, a failure mode) only when the summary leaves a question
your design depends on. If a summary is missing or contradicts its spec, say so in your
return message.

## Checklist: a spec is ready to approve when

- [ ] The Summary is present, at most 80 lines, and matches the rest of the spec.
- [ ] The purpose is one sentence with no "and".
- [ ] "Not responsible for" names who owns each excluded concern.
- [ ] Every interface method names its consumer. No method lacks a consumer.
- [ ] Every method has failure modes (or a stated reason it can't fail), each with an `errs`
      kind and a named test.
- [ ] Every operation on data names its permission and scope; every personal-data column has
      a sensitivity level and retention rule.
- [ ] A module spec depends on no other module; cooperation is through core contracts or
      events.
- [ ] Things regionals would do differently are settings, with defaults.
- [ ] Imports follow the layer rules in `go-conventions`.
- [ ] Concurrency and lifecycle are stated (safe for concurrent use? who calls `Close`?).
- [ ] Every setting and config value has a default, a valid range, and a validation error.
- [ ] If an external technology is wrapped, the lock-in check names an OSI-licensed or
      self-hostable implementation.
