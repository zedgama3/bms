---
name: go-conventions
description: The project's Go rules in concrete form — package layers and import rules, the lib/errs error kinds and wrapping, validating constructors, context deadlines, resource closing, logging, and testing patterns (table tests, fakes, injectable clock, integration build tag). Load before writing, testing or reviewing any Go code in this repo.
---

# Go conventions

These turn the tenets in `plan/00-principles.md` into code. If this file and a design spec
disagree, the spec wins for that package. Report the conflict.

## Layers and imports

```
internal/lib/<x>            shared helpers (errs, clock,  imports: stdlib (and each other, acyclic)
                            validate, secret, httpx, ...)
internal/model              shared value types            imports: lib
internal/config             process configuration         imports: lib
internal/core/<c>           core contracts (module, db,   imports: model, lib, other core contracts (acyclic)
                            identity, authz, people, ...)
internal/core/<c>/<impl>    implementations (oidc, email) imports: own contract, model, lib, 3rd-party driver
internal/modules/<m>/...    feature modules               imports: core contracts, model, lib. NEVER another module
internal/api                HTTP shell                    imports: core contracts, model, lib
internal/app                composition root              imports: anything
cmd/bms                     main                          imports: app, config
web                         the built web shell (embed)   imports: stdlib, lib/errs; imported by app only
```

- A module **never** imports another module, `api`, `app` or `config`. Modules cooperate
  through core contracts and signals (domain events, `core/signals`).
- Modules add themselves to the registry in `core/module` with `module.Register(Manifest, New)`
  in `init`, where `module.go` holds `var Manifest = module.Manifest{...}` and
  `func New(ctx context.Context, deps module.Deps) (module.Module, error)`. `app` imports
  the modules it ships with for their side effect, one line each in
  `internal/app/modules.go`: `_ "github.com/zedgama3/bms/internal/modules/<name>"` (plus the
  web line, D31). Module names are letters and digits only
  (`afterreports`).
- depguard in `.golangci.yml` and `make imports` enforce these rules. A failure is a design
  problem, not a lint nuisance.

## Module package layout

See the `add-module` skill. In short: `module.go` (the manifest, `New` and the registry
`init`), `service.go` (use cases, each checking its permission, and declaring the storage
interface it needs), `postgres/` (the only place with SQL and the driver: `store.go`, `sql/`,
`migrations/`; D8), `http.go` (handlers, thin), `signals.go`, `privacy.go` (implements
`module.PersonData`). Record types shared by `service.go` and `postgres/` live in a leaf package
(`<name>/<record>/`, importing only `model` and `lib`): the module root imports `postgres/` to
build the store, so `postgres/` cannot import the root. Until identity, authz and settings
exist (M0), the service takes an actor function, a permission checker and a setting source
wired to "no actor", "deny" and "the declared default", so it fails closed. From M2 the permission checker seam is `deps.Authz` (an `authz.Authorizer`) and each use case checks its permission first in one of three shapes: `Allow` (a site-wide permission), `AllowIn` (a single record belonging to a department), or `ExtentOf` (a list, search, count or export filters by the returned extent: `WHERE department_id = ANY($1)`, or everything when `Site`). `deps.People` (`NamesOf`, `ConsentOf`) is the directory; a foreign key to `core.person(id)` is allowed and uses `ON DELETE RESTRICT`, because `people` calls the module's `DeletePerson` first and, inside people's transaction, a module uses the ctx it was given and never starts a new `Transact(Write)` on another ctx. From M1 the actor is `identity.PersonFrom(ctx)` (false inside a signal delivery and for a restricted session), imported from `core/identity` and taken as a seam (`actorFunc`) so tests use `identity.WithSession`; `Deps` gains no field for it. Audit: every read of `personal` fields about someone else and every `sensitive` read calls `deps.Audit.Record` after loading and before returning; a change records inside its `Write` transaction, never inside a `Read` transaction; actions are `<module>.<record>.<verb>`; a `Record` error fails the use case. The `hello`
module (`plan/design/module-hello.md`) is the template. `Start` registers signal handlers and jobs and never writes, so it
succeeds on a read-only copy. `New` needs no database read, because `bms openapi` and
`bms check` build every module against a `DB` that refuses every statement (app.md decision
7). Every module ships `web/src/modules/<name>/messages/en.json`, screens or not. The manifest carries no human-readable text: titles and
descriptions are catalogue keys derived from the names (module-contract decision 8). Every
module's tests call `moduletest.Conformance` with `WithSeed` (or `WithoutPersonData` when
the spec's Data section lists nothing about a person) and `WithCapability` for each
capability the module requires or uses.

## Errors: `internal/lib/errs`

Kinds, and their HTTP mapping. The table is `lib/httpx.StatusOf`; the api shell's mapper is
the only writer of an error response on a real request; handlers call
`httpx.Fail(w, r, err)` and return, and never write an error body themselves
(`plan/design/api.md`, decision 2):

| Kind | Meaning | HTTP |
|---|---|---|
| `Invalid` | caller gave bad input or config | 400 |
| `Unauthenticated` | no or bad identity | 401 |
| `Permission` | the person lacks the permission for this action or record | 403 |
| `NotFound` | the thing doesn't exist | 404 |
| `Conflict` | state conflict or duplicate | 409 |
| `Unavailable` | dependency down or unreachable; retrying may help | 503 |
| `Timeout` | deadline passed | 504 |
| `Canceled` | the caller gave up (context canceled); never retried | none if the client is gone; 503 on shutdown |
| `Unsupported` | feature or module turned off in this deployment | 501 |
| `Internal` | a bug or broken invariant | 500 |

`Internal` is the zero value and the kind of any error that was never classified. The API is
set by `plan/design/lib-errs.md` (ported from Sojourn, D11): `errs.E(op, kind, err)` (returns nil for a nil err),
`errs.KindOf(err)`, `errs.Is(err, kind)`. Pattern:

```go
// create, with an operation name and a kind
return errs.E("volunteers.postgres.CreateShift", errs.Unavailable, err)

// add context while keeping the kind
if err := r.load(ctx); err != nil {
    return fmt.Errorf("instance %s: load: %w", id, err) // %w, never %v
}

// decide by kind, never by message
if errs.Is(err, errs.Unavailable) { backoff() }
```

Rules:

- **Never** `_ = f()` on a fallible call, and never call `if err != nil { log; continue }`
  without returning or recording the error somewhere the spec names.
- Translate driver errors into kinds **at the storage boundary**: core `db` and each
  `postgres/` package (D8). For example, a unique violation becomes `Conflict` and a network
  error becomes `Unavailable` (see `postgres-sql`). Nothing above the storage boundary sees
  driver error types.
- `panic` only for impossible states in `init`/`main` setup. Library code returns errors.
- Collect multiple validation errors with `errors.Join` so config problems show up all at
  once.

## Constructors validate

```go
func NewService(cfg Config, db db.DB, authz authz.Authorizer, clk clock.Clock) (*Service, error) {
    if err := cfg.Validate(); err != nil {
        return nil, errs.E("volunteers.NewService", errs.Invalid, err)
    }
    if db == nil || authz == nil || clk == nil {
        return nil, errs.E("volunteers.NewService", errs.Invalid, errors.New("nil dependency"))
    }
    ...
}
```

- No exported zero-value-usable structs that hold dependencies. Use `New…` constructors that
  return `(T, error)`.
- `model` types expose `Validate() error`. Call it at boundaries (API input, before store
  writes, after decoding a request).

## Context and deadlines

- `ctx context.Context` is the first parameter of every function that does I/O or may block.
- Every outbound call derives a deadline:
  `ctx, cancel := context.WithTimeout(ctx, cfg.QueryTimeout); defer cancel()`.
- Never store a context in a struct. Never use `context.Background()` below `main`/`app`,
  except in tests.

## Resources

```go
func (r *reader) query(ctx context.Context) (out []Row, err error) {
    const op = "volunteers.postgres.list"
    rows, err := r.db.Query(ctx, q) // r.db is a db.DB; the deadline is derived inside
    if err != nil { return nil, db.Wrap(op, err) }
    defer rows.Close() // releases the connection and the statement deadline db derived
    for rows.Next() { ... }
    if err := rows.Err(); err != nil { return nil, db.Wrap(op, err) }
    return out, nil
}
```

- `rows.Err()` is always checked. Every error that leaves a store method goes through
  `db.Wrap(op, err)`, which classifies driver errors and scrubs values (db.md). pgx's
  `Rows.Close` returns nothing; where a `Close` does return an error (a file, a response
  body), a deferred `Close` joins it into the return value:
  `defer func() { err = errors.Join(err, f.Close()) }()`.
- Every goroutine has a documented exit (context cancellation or channel close) and is
  waited on (`errgroup` or a `sync.WaitGroup`) at shutdown.

## Logging and observability

- `log/slog` only, with structured fields (`slog.String("instance", id)`). Loggers are passed
  in, never global.
- Log an error **once**, where it's handled. Don't log and also return it.
- Never log secrets, passwords, connection strings or full SQL parameter values. Hold them in
  `secret.Secret`, read them once with `Value()`, and compare with `Value()`, never with
  `==`.
- Never log personal data: log person IDs, not names, e-mails or phone numbers. Never log
  fields marked sensitive (`privacy-data`).
- Use the `*Context` methods (`logger.InfoContext(ctx, ...)`) wherever a ctx exists, so the
  record carries the request's `trace_id` and `span_id` (`sloglint` with `context: scope`
  enforces it). The trace ID is the request ID; there is no second one.
- Attribute keys follow the key grammar `[a-z][a-z0-9_.]{0,63}` and are never a denied key
  (the list is in `plan/design/telemetry.md`, "The key denylist": `email`, `legal_name`,
  `phone`, `body`, `sql`, `token`, ...). The telemetry handler rewrites a denied value to
  `[denied]` and counts a violation, which fails any test built on `telemetrytest`.
- Spans come from a `telemetry.Tracer` passed in (`Deps.Tracer` in a module, an
  `Options` field in a core package), with typed attributes: `telemetry.Token` for names,
  routes, operations and status words; `telemetry.ID` for records; `telemetry.Int`,
  `Duration`, `Bool`, `Kind` for the rest; `telemetry.Text` only for an error message (it
  scrubs and caps). Metrics take tokens, numbers and kinds only, never an ID or free text.
  `defer span.End()` right after `Start`; `span.RecordError(err)` where the error is
  handled.
- Never import `go.opentelemetry.io` outside `internal/core/telemetry` (depguard
  `otel-seam`), and never use `otel.Tracer`, `otel.Meter` or `otel.GetTracerProvider`
  (forbidigo).
- A test that must prove nothing personal leaks builds its logger and tracer with
  `telemetrytest.New(t)` and asserts `!c.Contains(marker)`; the capture fails the test on
  any policy violation at cleanup.

## Testing

- Table-driven tests with `t.Run(tc.name, ...)`. Check error kinds with `errs.Is`. Never
  compare error strings.
- Dependencies are interfaces, so tests use **fakes** (hand-written, in `<pkg>test` or
  `lib/testkit`). No mocking frameworks. The core ships them: `dbtest.NewFake()` for
  `Deps.DB`, `telemetrytest.New(t)` for the logger and tracer, and
  `signalstest.NewFake(Manifest.Name, Manifest.Publishes, Manifest.Subscribes)` for
  `Deps.Signals` (it records what a use case published after commit, enforces the
  manifest's names, and `Deliver` runs a handler with the handler's error returned
  unchanged). From M2: `authztest.NewFake()` as `Deps.Authz` (denies everything until
  `Fake.Grant(person, p)`, `GrantIn(person, p, dept)` or `Superuser(person)`) and
  `peopletest.NewFake()` as `Deps.People` (`Fake.Add(id, name)`, `Fake.Consent(id, purpose, granted)`).
- Time comes from `lib/clock`. Tests use a fake clock. **No `time.Sleep` in tests.**
- Tests that need a real database: `//go:build integration`, using containers from
  `lib/testkit` (or the running `deploy/compose` stack). Unit tests must pass with no
  infrastructure.
- Always run `go test -race`.
- Each spec failure mode has a test named so it can be traced back:
  `TestCreateShift_NotDepartmentLead_ReturnsPermission`.
- Test data uses invented people only. Never real names, e-mails or phone numbers.

## Style

- `gofmt` and `goimports`. Package names are short, lowercase, and not `util` or `common`.
- Accept interfaces, return concrete types, except contract constructors that return the
  contract interface for the registry.
- Exported identifiers get doc comments that say what they do *and* how they fail.
