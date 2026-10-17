# lib/errs: error kinds and wrapping

- **Status:** Implemented (ported from Sojourn, D11, 2026-10-16). Review amendments carried over
  from the original: F13 clarified, F16 clarified, F17 and F18 added.
- **Package:** `internal/lib/errs` (Go module `github.com/zedgama3/bms`; foundation library, not a bms module)
- **Depends on specs:** none (foundation; standard library only)
- **Used by:** every package that returns or inspects an error. Named consumers below:
  storage and driver boundaries (core `db`, each module's `postgres` store, `identity`
  providers, `notify` e-mail, `files` backends), constructors everywhere, module services,
  `jobs`, `api` (ErrorMapper), `lib/retry`, `lib/validate`, logging, and every test that checks
  an error path.

## Summary

**Purpose.** Give every error in bms one machine-readable kind that callers decide on without reading its message. Package `internal/lib/errs`; standard library only (`context`, `errors`, `strconv`); no config, signals, permissions or routes.

**Exported names.**
- `type Kind uint8`; zero value is `Internal`. Ten constants: `Internal`, `Invalid` (bad input or config), `Unauthenticated`, `Permission`, `NotFound`, `Conflict` (state conflict or duplicate), `Unavailable` (dependency down; retry may help), `Timeout`, `Canceled` (caller gave up; never retry), `Unsupported` (feature or module off in this deployment).
- `func (Kind) String() string`: stable lower_snake_case name (`internal`, `invalid`, `unauthenticated`, `permission`, `not_found`, `conflict`, `unavailable`, `timeout`, `canceled`, `unsupported`); out of range gives `Kind(<n>)`; never panics. These strings are API error codes; changing one is a breaking change.
- `type Error struct { Op string; Kind Kind; Err error }`, with `Error() string` ("<Op>: <Err>", kind not in the text) and `Unwrap() error`. Build with `E`, not a literal.
- `func E(op string, kind Kind, err error) error`: nil if err is nil; out-of-range kind recorded as `Internal`; the outer kind wins over any inner kind.
- `func KindOf(err error) Kind`: depth-first, left-to-right through wrap and join trees; kind of the first `*Error` found (even if out of range or typed-nil, which give `Internal`); else `context.Canceled` gives `Canceled` (wins over deadline), `context.DeadlineExceeded` gives `Timeout`, else `Internal`; `KindOf(nil)` is `Internal`.
- `func Is(err error, kind Kind) bool`: `KindOf(err) == kind`; false for nil err.

**Key decisions (Behavior).**
1. Unkinded errors classify as `Internal`; there is no `Other` kind.
2. `Canceled` is distinct from `Timeout`.
3. Context sentinels map to kinds only when no `*Error` is present; other stdlib sentinels are not recognised (drivers translate their own).
4. Joined errors: first kinded error, depth-first, left to right.
5. One kind per error.
6. Re-kinding allowed, outer wins; to keep the inner kind use `fmt.Errorf("...: %w", err)`; no `Wrap` helper.
7. `E(op, kind, nil)` returns nil; to make a fresh error pass an explicit cause (`errors.New`).
8. No public-message field; the api derives client text from the kind and from `validate` field errors.
9. No retryable helper here; `lib/retry` owns it.
10. Kind not repeated in message text; log it as a structured field.
11. Op format `pkg.Type.Method` or `pkg.Func`; an empty op is tolerated (prefix omitted).
Failure modes F1-F18 are degenerate-input results of total, pure functions (nothing here returns an error about itself).

**What callers handle.** Branch with `errs.Is` / `errs.KindOf`, never on message text. Classify driver and network errors at the boundary package that owns the driver. `Canceled`: stop, do not retry, api writes no response if the client is gone (api.md decision 17; during shutdown it answers 503 `shutting_down`).

**Config, settings, permissions, routes, signals.** None. Concurrency: pure functions; `*Error` immutable after `E`.

**Expects of dependencies.** None (foundation; depends on no spec). It defers to `lib-core` (`lib/retry` decides retryable kinds, `lib/validate` defines the typed field error that `E(..., Invalid, ...)` wraps), to `lib/httpx` (kind to HTTP status table, `Kind.String()` as body `code`; applied in api.md), and to `api` (never emits `err.Error()` to a client).

**Pending amendments.** None. Open questions: None.

## Purpose

Give every error in bms one machine-readable kind that callers can decide on without
reading its message.

## Responsibilities

- Define the closed set of error kinds and their stable string names.
- Attach an operation name and a kind to a cause (`E`), so messages chain outer op to inner
  op to root cause.
- Answer "what kind is this error?" for any error value (`KindOf`, `Is`), walking wrapped and
  joined chains and recognising the two `context` sentinels.
- Interoperate with the standard library: `errors.Is`, `errors.As`, `errors.Join`,
  `fmt.Errorf("%w")` all keep working through an `errs` error.

## Not responsible for

| Concern | Owner |
|---|---|
| Mapping a kind to an HTTP status and a client-safe message | `api` ErrorMapper (`api.md`) |
| Deciding which kinds are worth retrying, and the backoff | `lib/retry` (see "Proposed changes elsewhere") |
| Translating driver, network and OS errors into kinds (`net.Error`, `os.ErrDeadlineExceeded`, SQLSTATE) | the package that owns the driver (core `db`, a module's `postgres` store), at its boundary |
| Structured, user-facing validation detail ("field X must be > 0") | `lib/validate` typed field errors, surfaced by ErrorMapper with `errors.As` |
| Logging errors, or choosing log levels | the package that handles the error, using `log/slog` |
| Stack traces, error codes beyond the kind, telemetry | not provided; out of scope by design (keep the package small) |

## Sub-abstractions

| Name | Purpose (one line) | Notes |
|---|---|---|
| `Kind` | Name the category of a failure so callers can branch on it. | Closed enum. Zero value is `Internal`. `String()` values are stable identifiers. |
| `Error` | Carry an op, a kind and a cause through the call chain. | Immutable once built. Implements `error` and `Unwrap() error`. |
| Classification (`KindOf`, `Is`) | Find the kind of any error value, kinded or not. | Pure functions over the error tree. |

## Interface

```go
// Package errs classifies errors by kind so callers decide by kind, never by message.
// It depends only on the standard library.
// Ported from Sojourn (D11).
package errs

// Kind is the category of a failure. The zero value is Internal, so an unclassified
// error is treated as a bug rather than as something the caller did wrong.
type Kind uint8

const (
	Internal        Kind = iota // a bug or broken invariant; the default when nothing better is known
	Invalid                     // caller gave bad input or configuration
	Unauthenticated             // no or bad identity
	Permission                  // the person lacks the permission for this action or record
	NotFound                    // the thing doesn't exist
	Conflict                    // state conflict or duplicate
	Unavailable                 // dependency down or unreachable; retrying may help
	Timeout                     // a deadline passed before the operation finished
	Canceled                    // the caller gave up (context canceled); retrying is wrong
	Unsupported                 // feature or module turned off in this deployment
)

// String returns the kind's stable lower_snake_case name, for log fields and API error
// codes: "internal", "invalid", "unauthenticated", "permission", "not_found", "conflict",
// "unavailable", "timeout", "canceled", "unsupported". A value outside the defined set
// returns "Kind(<n>)". It never panics.
func (k Kind) String() string

// Error is an error with an operation name, a kind and an optional cause.
// Build it with E rather than a literal; E normalises the kind and drops nil causes.
type Error struct {
	Op   string // where it happened: "pkg.Type.Method" or "pkg.Func", e.g. "volunteers.postgres.CreateShift"
	Kind Kind   // the classification; read it through KindOf, which also handles unkinded errors
	Err  error  // the cause; nil only when built as a literal
}

// Error returns "<Op>: <Err>". If Op is empty the prefix is omitted. If Err is nil the
// kind's name stands in for the cause. Because Err may itself be an *Error, messages chain
// naturally: "volunteers.Service.Enroll: volunteers.postgres.CreateShift: dial tcp ...: connection refused".
// The kind is not repeated in the text; log it as a structured field instead.
func (e *Error) Error() string

// Unwrap returns the cause so errors.Is and errors.As see through this error.
func (e *Error) Unwrap() error

// E wraps err with an operation name and a kind.
//
// It returns nil when err is nil, so `return errs.E(op, kind, f())` is correct on success.
// A kind outside the defined set is recorded as Internal. The returned error's KindOf is
// kind, even when err already carries a different kind (the outer decision wins).
func E(op string, kind Kind, err error) error

// KindOf reports the kind of err. It walks the error tree depth-first, left to right (the
// same order errors.Is uses) and returns the kind of the first *Error it meets. If there is
// none: context.Canceled anywhere in the tree gives Canceled (it wins if DeadlineExceeded is
// also present), context.DeadlineExceeded gives Timeout, and anything else gives Internal.
// A typed-nil or out-of-range *Error is the first *Error and gives Internal. KindOf(nil) is Internal, because asking the
// kind of a nil error is itself a broken invariant; check err != nil first.
func KindOf(err error) Kind

// Is reports whether KindOf(err) == kind. It is false for a nil err regardless of kind,
// so it is safe to call before the nil check. Use it in place of comparing messages.
func Is(err error, kind Kind) bool
```

### Who needs each method

| Member | Consumer | Pre-condition | Post-condition |
|---|---|---|---|
| `Kind` constants | everyone creating or branching on errors | — | one of the ten values |
| `Kind.String` | `api` ErrorMapper (error body `code` field); observability (`slog.String("kind", ...)`) | — | stable string; never panics |
| `Error` (exported type and fields) | observability error logging (`errors.As` to log `op` as a field); tests asserting `Op` without string comparison | — | fields readable; value immutable by convention |
| `Error.Error` | `log/slog`, `fmt`, test failure output | — | non-empty string; never panics |
| `Error.Unwrap` | `errors.Is`/`errors.As` in any caller; `KindOf` | — | returns `Err`, possibly nil |
| `E` | `db` and `postgres` stores (classify a driver error), constructors (`Invalid`), module services (re-kind: a store `NotFound` on a referenced shift becomes `Invalid`), `jobs` handlers (op context) | — | nil iff `err == nil`; otherwise `*Error` with normalised kind |
| `KindOf` | `api` ErrorMapper (status code); `lib/retry` (retry policy); store helpers re-wrapping a kinded error (`errs.E(op, errs.KindOf(err), err)`); observability | — | always one of the ten values |
| `Is` | `jobs` runner and `notify` sender (back off on `Unavailable`, `Timeout`), module services, every error-path test | — | boolean; false for nil |

## Types

Defines `Kind` and `Error` above. Uses nothing from `model` (layer rule 1: `lib/*` depends on
nothing in the project).

## Behavior

**Decisions settled here, with reasons.**

1. **Unkinded errors classify as `Internal`.** The conventions require classification at the
   boundary where the driver is known. An error reaching a kind check without a kind means
   that boundary missed a case; `Internal` (HTTP 500) is the safe answer and makes the gap
   visible in tests. There is no `Other` kind: it would give callers a category that means
   "nobody decided", which is never actionable.
2. **`Canceled` is its own kind, distinct from `Timeout`.** `context.Canceled` means the
   caller stopped wanting the result (shutdown, client disconnect, a sibling in an errgroup
   failed). It is not a dependency fault, so it must not be retried (`Timeout` and
   `Unavailable` may be) and must not be reported as a 504. Keeping it separate lets
   `lib/retry` stop immediately and lets ErrorMapper stay silent when the client is gone.
3. **`context.DeadlineExceeded` → `Timeout`, `context.Canceled` → `Canceled`,** detected with
   `errors.Is` anywhere in the tree, but only when no `*Error` is present. An explicit kind
   set at a boundary is a deliberate decision and outranks the sentinel beneath it. Other
   standard-library sentinels (`os.ErrDeadlineExceeded`, `net.Error.Timeout()`) are not
   recognised here: they only arise from drivers and sockets, which `db` and the stores already translate.
4. **Joined errors: first kinded error in depth-first, left-to-right order.** This is the order
   `errors.Is` and `errors.As` use, so `errs.Is(err, k)` agrees with
   `errors.As(err, &target)`. It also matches how the code joins: `errors.Join(err,
   rows.Close())` puts the primary failure first, so a `Close` error does not mask it. A
   priority scheme (say, `Internal` beats everything) was rejected because it would let a
   secondary cleanup error override the real one and would make `Is` depend on a table nobody
   remembers.
5. **One kind per error.** `Is(err, k)` is exactly `KindOf(err) == k`. An error is never two
   kinds at once, so branch order in callers does not matter.
6. **Re-kinding is allowed and the outer kind wins.** `E(op, kind, inner)` reports `kind` even
   if `inner` has another. Module services need this (store `NotFound` → `Invalid` input). To keep the
   inner kind while adding context, use `fmt.Errorf("shift %s: %w", id, err)`; `KindOf`
   walks through it. No separate `Wrap` helper: `fmt.Errorf` with `%w` already does that job
   and the conventions already teach it.
7. **`E(op, kind, nil)` returns nil.** It makes `E` a wrap-if-failed operation (the
   `pkg/errors.Wrap` precedent), so `return errs.E(op, kind, f())` is correct on success. The
   alternative, a kinded error with no cause, turns a success into a failure whenever a caller
   forgets the nil check, which is the more common mistake. To create a fresh error, pass an
   explicit cause: `errs.E(op, errs.Invalid, errors.New("nil clock"))`.
8. **No public-message field.** What is safe to show an HTTP client is API policy (for
   example, whether to reveal `NotFound` versus `Permission`), and driver messages carry host
   names and SQL. ErrorMapper derives the client text from the kind and from typed field
   errors (`lib/validate`) found with `errors.As`. `Error.Error()` is for logs and tests only.
9. **No retryable helper.** Which kinds merit a retry is policy that differs by caller (the
   `jobs` runner retries `Unavailable` and `Timeout`; an API request retries nothing; nobody
   retries `Canceled`). `lib/retry` owns that decision and expresses it over `KindOf`.
10. **The kind is not in the message text.** Chained messages would read
    "a: unavailable: b: unavailable: dial tcp ...". Loggers add `kind` as a structured field.
11. **Op format** is `pkg.Type.Method` or `pkg.Func` using the package's last path element
    (`volunteers.postgres.CreateShift`, `jobs.NewScheduler`). An empty op is tolerated (prefix
    omitted) rather than rejected, because turning a cosmetic omission into a different error
    at run time would be worse; code review catches it.

**Concurrency:** all functions are pure. An `*Error` is never mutated after `E` returns, so it
is safe to share across goroutines, like any standard-library error value.

**Lifecycle and configuration:** none. No state, no `New`, no `Close`, no settings.

**Imports:** `context`, `errors`, `strconv` only.

## Failure modes

None of these functions can fail at its own job: they are total, pure functions that never
return an error about themselves and never panic. The table therefore lists each degenerate
input and the result the implementation must produce; each row has a test.

| # | Member | Condition | Result (errs kind) | Caller is expected to | Test |
|---|---|---|---|---|---|
| F1 | `E` | `err == nil` | returns `nil` | treat as success | `TestE_NilErr_ReturnsNil` |
| F2 | `E` | kind outside the defined set | `*Error` with kind `Internal` | fix the call | `TestE_OutOfRangeKind_ClassifiesInternal` |
| F3 | `E` | `op == ""` | error with no op prefix; kind kept | fix the call | `TestE_EmptyOp_OmitsPrefix` |
| F4 | `E` | `err` already kinded differently | outer kind wins; inner message and kind still reachable via `Unwrap` | intentional re-kind | `TestE_Rekind_OuterKindWins` |
| F5 | `KindOf` | `err == nil` | `Internal` | check `err != nil` first | `TestKindOf_Nil_ReturnsInternal` |
| F6 | `KindOf` | no `*Error`, no context sentinel | `Internal` | classify at the boundary | `TestKindOf_Unkinded_ReturnsInternal` |
| F7 | `KindOf` | `context.DeadlineExceeded` in tree, no `*Error` | `Timeout` | retry or report 504 | `TestKindOf_DeadlineExceeded_ReturnsTimeout` |
| F8 | `KindOf` | `context.Canceled` in tree, no `*Error` | `Canceled` | stop; do not retry | `TestKindOf_Canceled_ReturnsCanceled` |
| F9 | `KindOf` | `*Error` present above a context sentinel | the `*Error`'s kind | — | `TestKindOf_KindedOutranksContextSentinel` |
| F10 | `KindOf` | `fmt.Errorf("%w")` around a kinded error | inner kind | — | `TestKindOf_ThroughFmtErrorf_PreservesKind` |
| F11 | `KindOf` | `errors.Join` with several kinded members | kind of the first in depth-first, left-to-right order | — | `TestKindOf_Joined_FirstKindedDepthFirst` |
| F12 | `KindOf` | `errors.Join` with no kinded member but a context sentinel | `Timeout`/`Canceled` per F7/F8 | — | `TestKindOf_Joined_Unkinded_UsesSentinel` |
| F13 | `KindOf` | literal `&Error{Kind: Kind(200)}`; the first `*Error` decides even when out of range, so a later kinded error in a join is not consulted | `Internal` | build with `E` | `TestKindOf_OutOfRangeLiteral_ReturnsInternal` |
| F14 | `Is` | `err == nil` | `false` for every kind | — | `TestIs_Nil_False` |
| F15 | `Kind.String` | value outside the defined set | `"Kind(<n>)"`; no panic | — | `TestKindString_OutOfRange` |
| F16 | `Error.Error` | `Err == nil` (literal) | `"<Op>: <Kind.String()>"` (so `"Kind(<n>)"` when out of range), or just the kind name when `Op` is also empty | build with `E` | `TestErrorString_NilErr_UsesKindName` |
| F17 | `KindOf` | both `context.Canceled` and `context.DeadlineExceeded` in the tree, no `*Error` | `Canceled` (cancellation wins: retrying abandoned work is never right; not retrying a timeout is only conservative) | stop; do not retry | `TestKindOf_BothSentinels_CanceledWins` |
| F18 | `KindOf` | a typed-nil `*Error` is the first `*Error` in the tree | `Internal` (the first `*Error` decides, as in F13) | fix the call site | `TestKindOf_TypedNilError_ReturnsInternal` |

## Lock-in check

No external technology. Standard library only (`context`, `errors`, `strconv`), satisfying
layer rule 1 (07-abstractions.md).

## Test plan

Unit tests only, in `internal/lib/errs/errs_test.go`, table-driven, no infrastructure, no
fakes needed. Run with `go test -race`.

Behaviour (happy path):

- `TestE_WrapsCauseWithOpAndKind`: `E` returns `*Error` with the given op, kind, cause;
  `errors.Is(got, cause)` is true; `errors.As(got, &target)` succeeds.
- `TestE_Chained_MessageIsOuterOpThenInner`: `E(outer, k, E(inner, k, cause))` reads
  `"outer: inner: cause"`.
- `TestErrorsAs_FindsErrorThroughFmtErrorf`: `fmt.Errorf("ctx: %w", E(...))` still yields the
  `*Error` with `errors.As`.
- `TestIs_AgreesWithKindOf`: table over all ten kinds; `Is(E(op, k, cause), k)` is true and
  false for every other kind.
- `TestKindString_AllKinds_Stable`: table pinning each constant to its exact string. This
  test is the contract that the strings are stable API codes; changing one is a breaking
  change to the API spec.
- `TestKindOf_RealContextDeadline`: `context.WithTimeout(ctx, 0)`, read `<-ctx.Done()`, then
  `KindOf(ctx.Err()) == Timeout` (no `time.Sleep`; a zero timeout is already expired).
- `TestKindOf_RealContextCancel`: cancel a context, `KindOf(ctx.Err()) == Canceled`.
- `ExampleE`, `ExampleIs`: runnable doc examples showing the storage-boundary and
  decide-by-kind patterns.

Failure modes: F1 through F18, one test each, named in the table.

Lint: the package must pass `errorlint`, `errcheck`, `wrapcheck` and `depguard`
(stdlib-only).

## Open questions

None. The one question of the first draft was answered when `api.md` was approved:

1. **HTTP treatment of `Canceled`.** Answered as proposed (api.md decision 17, owner
   2026-10-17): when the request context is the thing canceled, the api logs at debug and
   writes no response (the client is gone); when a server shutdown cancels work for a
   still-connected client, it answers `503` with code `shutting_down` and
   `Connection: close`. Not a 504 and not a 500.

## Proposed changes elsewhere

1. **`plan/design/api.md` (row 16)** (applied 2026-10-17: the kind-to-status table is
   `lib/httpx.StatusOf`, the body is `httpx.Problem` with `Kind.String()` as `code`, and
   the api's mapper never emits `err.Error()`; validation detail travels as
   `validate.Fields(err)`): ErrorMapper owns the client-visible
   message. It maps `KindOf(err)` to status, uses `Kind.String()` as the body's `code`, never
   emits `err.Error()` to a client, and surfaces validation detail only through
   `lib/validate` typed field errors found with `errors.As`.
2. **`lib/retry` and `lib/validate`:** specified in [lib-core.md](lib-core.md). `retry` decides
   retryable kinds over `errs.KindOf` (default: retry `Unavailable` and `Timeout`; never
   `Canceled`; everything else fails fast). `validate` defines the typed field error that
   `E(..., errs.Invalid, ...)` wraps and ErrorMapper extracts; `errs` deliberately carries no
   user-facing message.
