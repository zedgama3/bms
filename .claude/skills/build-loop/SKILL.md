---
name: build-loop
description: How the main session orchestrates building one abstraction or module with the project agents — architect spec, parallel implementers and test-author, check-runner, go-reviewer and web-reviewer, fix rounds, and when to stop and hand back to the owner. Load when asked to build, implement or continue work on a spec, a package, a module or a roadmap item.
---

# Build loop

The main session is the **orchestrator**. It doesn't write production code itself for
spec-sized work; it sends each job to the agent that owns it and passes results between them.
Agents never call each other.

## 0. Preconditions

- Find the spec in `plan/design/README.md`.
  - **Not started or Draft:** run `architect` to write it, then **stop and ask the owner to
    approve it**. Never approve a spec yourself.
  - **Approved:** continue.
- Make sure the spec's dependency specs are Implemented. If not, build those first, or stop
  and say which ones are missing.

## 1. Build and test in parallel (one message, several agents)

- `go-implementer`: the spec path and the working directory.
- `sql-author`, as well, when the spec adds tables, migrations or non-trivial queries. Give
  it the schema and query files; `go-implementer` owns the Go that calls them.
- `frontend-implementer`, as well, for the spec's UI section, once the API it calls exists.
- `test-author`: the spec path and the package directory.

Tell no agent about another's output. Their independence is the point.

**File ownership, so agents never collide:**
- `test-author` owns `<pkg>_test.go` (black-box, `package <pkg>_test`) and every test named in
  the spec, including the spec's happy-path and permission test names.
- `go-implementer` writes only `example_test.go` and, if it needs white-box tests,
  `internal_test.go` with test names that do **not** appear in the spec.
- `sql-author` owns `postgres/sql/` and `postgres/migrations/` in the package.
- `frontend-implementer` owns `web/`.
- Say this in every prompt.

## 2. Mechanical checks: `check-runner` (haiku), one command per call, in parallel

```
make fmt
make vet
make build
go test -race ./internal/<pkg>/...
golangci-lint run ./internal/<pkg>/...
make imports
```

For `web/`: `pnpm lint`, `pnpm tsc --noEmit`, `pnpm test`, `pnpm build`.

If anything fails, send the failing output to the agent that owns the file (or to
`test-author` if a test doesn't compile because of an exported name, not because the code
breaks the spec). Then run step 2 again.

A test that fails because **the code doesn't match the spec** goes to the implementer. Never
weaken the test.

## 3. Review (opus)

- `go-reviewer` for Go and SQL; `web-reviewer` for `web/`. Run both in parallel when the
  change has both.
- Pass the spec path. Send **must fix** findings to the owning implementer, then repeat
  steps 2–3. Show **consider** findings to the owner; don't act on them automatically.

## 4. Stop conditions

- **Done:** checks green and no reviewer has must-fix findings. Set the spec's Status line and
  its row in `plan/design/README.md` to **Implemented** yourself, and ask `plan-keeper` to
  tick the roadmap item. Then report to the owner: what was
  built, its verification, consider-level findings, and open questions.
- **Commit (D16):** on the feature branch only (create `feat/<topic>` from `main` first if
  you're on `main`). Stage the files by name, never `plan/private/`, and commit with a
  message that says why. **Never push, tag, merge into `main`, or amend/rebase.** Tell the
  owner the branch name and the commit.
- **Stuck:** the same must-fix finding or failing check survives two fix rounds. Stop and
  bring both sides to the owner.
- **Spec problem:** an implementer or reviewer says the spec is wrong or ambiguous, or a
  module needs something from another module that no core contract provides. Stop. Send it
  to `architect` for a revision, which needs the owner's approval again.

## Cost discipline

- Run long commands in the foreground, or in the background and wait for the completion notice.
  Never poll with an `until ... do sleep ...` loop: `pgrep -f "<text>"` matches the loop's own
  command line, and a file that never appears leaves it running. An agent must not end its turn
  while a shell it started is still running.
- Never use an opus or fable agent for a check, a lookup, or a plan edit.
- Re-run only the checks affected by a fix round when it's obvious which ones they are; run
  the full set before declaring done.
- For a trivial change that has no spec (a typo, a one-line config edit), skip the loop. Make
  the change inline and run `check-runner` once.
