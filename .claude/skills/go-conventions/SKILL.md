---
name: go-conventions
description: The project's Go rules in concrete form — package layers and import rules, the lib/errs error kinds and wrapping, validating constructors, context deadlines, resource closing, logging, and testing patterns (table tests, fakes, injectable clock, integration build tag). Load before writing, testing or reviewing any Go code in this repo.
---

# Go conventions

These turn the tenets in `plan/00-principles.md` into code. If this file and a design spec
disagree, the spec wins for that package. Report the conflict.

## Layers and imports

```
internal/lib/<x>            shared helpers                imports: stdlib (and each other, acyclic)
internal/model              shared value types            imports: lib
internal/config             process configuration         imports: lib
internal/core/<c>           core contracts (module, db,   imports: model, lib, other core contracts (acyclic)
                            identity, authz, people, ...)
internal/core/<c>/<impl>    implementations (oidc, email) imports: own contract, model, lib, 3rd-party driver
internal/modules/<m>/...    feature modules               imports: core contracts, model, lib. NEVER another module
internal/api                HTTP shell                    imports: core contracts, model, lib
internal/app                composition root              imports: anything
cmd/bms                     main                          imports: app, config
```

- A module **never** imports another module, `api`, `app` or `config`. Modules cooperate
  through core contracts and signals (domain events, `core/signals`).
- Modules add themselves to the registry in `core/module` (`module.Register(volunteers.New)`).
  `app` imports the modules it ships with for their side effect.
- depguard in `.golangci.yml` and `make imports` enforce these rules. A failure is a design
  problem, not a lint nuisance.

## Module package layout

See the `add-module` skill. In short: `module.go` (the contract), `service.go` (use cases,
each checking its permission, and declaring the storage interface it needs), `postgres/`
(the only place with SQL and the driver: `store.go`, `sql/`, `migrations/`; D8), `http.go`
(handlers, thin), `signals.go`.

## Errors: `internal/lib/errs`

Kinds, and their usual HTTP mapping (done only in `api`'s ErrorMapper):

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
    rows, err := r.db.QueryContext(ctx, q)
    if err != nil { return nil, errs.E(op, kindOf(err), err) }
    defer func() { err = errors.Join(err, rows.Close()) }()
    for rows.Next() { ... }
    if err := rows.Err(); err != nil { return nil, errs.E(op, kindOf(err), err) }
    return out, nil
}
```

- `rows.Err()` is always checked. A deferred `Close` joins its error into the return value.
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

## Testing

- Table-driven tests with `t.Run(tc.name, ...)`. Check error kinds with `errs.Is`. Never
  compare error strings.
- Dependencies are interfaces, so tests use **fakes** (hand-written, in `<pkg>test` or
  `lib/testkit`). No mocking frameworks.
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
