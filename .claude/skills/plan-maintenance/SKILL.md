---
name: plan-maintenance
description: How the plan/ folder is organized and how to record decisions, answered questions, spec status and roadmap progress in it. Load before editing anything under plan/.
---

# Maintaining the plan

## Which file holds what

| File | Holds | Who edits |
|---|---|---|
| `README.md` | index, current phase, last updated, next steps | plan-keeper |
| `00-principles.md` | product principles and engineering tenets | owner only |
| `01-context.md` | why the project exists, who uses it | rarely |
| `02-scope.md` | goals, in/out of scope, modules, MVP | plan-keeper, when told |
| `03-decisions.md` | decision log D1..Dn | plan-keeper, when told |
| `04-architecture.md` | runtime shape, data ownership, how modules plug in | architect |
| `05-roadmap.md` | phases and checkboxes | plan-keeper |
| `06-open-questions.md` | open and resolved questions | plan-keeper |
| `07-abstractions.md` | the abstraction map and layer rules | architect (owner approves) |
| `08-agent-workflow.md` | agent roster and loop | owner or main session |
| `design/README.md` | spec status table | architect (Draft), plan-keeper (other statuses) |
| `design/<name>.md` | one spec | architect only |
| `notes/` | research notes | anyone |
| `private/` | **gitignored**: a specific regional, people, vendors | plan-keeper |

## Formats

**Decision** (append to `03-decisions.md`, numbered after the last one):

```markdown
## D<n>. <Short title>

- **Status:** Accepted (YYYY-MM-DD) | Proposed | Superseded by D<m>
- <What was decided, in one or two bullets>
- <Why, only if a reason was given>
```

**Answered question:** in `06-open-questions.md`, move the item to Resolved as
`- [x] **Q-ID:** <question>? <answer>. → D<n> or <file>`, then apply the answer in the
document it affects.

**Spec status:** change only the Status column in `design/README.md`.

**Roadmap:** tick `- [x]`; don't remove items. Add new items under the right phase.

## Rules

- Use dates in absolute form (YYYY-MM-DD), never "today" or "next week".
- Everything except `private/` is public. Write for any regional reading it. Details about a
  specific regional, person or vendor go only in `private/`.
- Never delete history: superseded decisions and resolved questions stay, marked as such.
- Update `README.md` "Last updated" on every change.
