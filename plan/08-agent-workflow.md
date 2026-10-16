# Agent Workflow

Code is built by focused agents. Each agent has one job and runs on the cheapest model that
does that job well. Building and checking are always done by different agents.

Definitions live in `.claude/agents/` and `.claude/skills/`. Change them there; this file
explains how they fit together. Adapted from Sojourn (D7).

## Roster

| Agent | Model | Job | Writes? |
|---|---|---|---|
| `architect` | fable | Designs abstractions and modules and writes design specs in `plan/design/` | spec files only |
| `go-implementer` | sonnet | Implements one spec (one package, core contract or module) with its happy-path tests | Go code |
| `test-author` | sonnet | Writes error-path, permission and contract tests **from the spec**, without reading the implementation first | `_test.go` only |
| `sql-author` | sonnet | Writes PostgreSQL schema, migrations and queries, and checks them against the compose database | SQL and Go query files |
| `frontend-implementer` | sonnet | Implements React/TypeScript screens and components against the OpenAPI contract | `web/` only |
| `go-reviewer` | opus | Reviews a Go/SQL change against the spec and the tenets: boundaries, errors, access and privacy, resources, concurrency | nothing |
| `web-reviewer` | opus | Reviews a UI change: spec conformance, every state handled, accessibility, mobile, privacy in the browser | nothing |
| `check-runner` | haiku | Runs one named check command and reports pass or fail with the failing output | nothing |
| `plan-keeper` | haiku | Records decisions and answers in `plan/`, updates status tables | `plan/` only |

## Skills

| Skill | Used by | Holds |
|---|---|---|
| `go-conventions` | every Go agent | the tenets in concrete Go form: layers, error kinds, constructors, contexts, testing patterns |
| `design-spec` | architect, reviewers | the spec template and the checklist a spec must pass |
| `build-loop` | the main session (orchestrator) | the sequence spec → build → test → check → review → fix, and when to stop |
| `burn-domain` | everyone, before naming or designing what burners see | how burns work and their vocabulary; updated whenever the owner explains more |
| `add-module` | architect, implementers, reviewers | checklist for a new feature module: layout, contract, schema, permissions, signals, UI, tests |
| `privacy-data` | architect, implementers, reviewers | how personal and sensitive data is classified, accessed, audited, exported and deleted |
| `postgres-sql` | sql-author, go-implementer, go-reviewer | schema-per-module, migrations, query style, driver error mapping |
| `plan-maintenance` | plan-keeper | how plan files are organized and updated |

## The loop for one abstraction or module

```
architect ──▶ design spec (plan/design/<name>.md), owner approves
                │
                ├──▶ go-implementer / sql-author / frontend-implementer   run in parallel,
                └──▶ test-author  (failure-mode + permission tests)       neither reads the other's work first
                │
                ▼
          check-runner   (make fmt / vet / build / test / lint / imports; pnpm checks)   back to the implementer on failure
                │
                ▼
          go-reviewer and/or web-reviewer   (spec conformance + tenets)   findings go to the implementer to fix
                │
                ▼
          check-runner   (again, after fixes) ──▶ main session commits to the feature branch;
                                             owner reviews, merges and pushes (D16)
```

The main session runs this loop with the `build-loop` skill. Agents never call other agents
and never touch git. Only the main session commits, and only to a local feature branch.

## Why this split

- **Cost:** most runs are checks and focused implementation, so they go to haiku and sonnet.
  Opus and fable are kept for design and review, where judgment matters.
- **Quality:** tests written from the spec by an agent that didn't write the code catch
  "built the wrong thing" as well as "built it wrong".
- **Focus:** each agent's prompt and tools are limited to its job (for example, test-author can
  only write `_test.go` files, and the reviewers and check-runner can't edit anything).
