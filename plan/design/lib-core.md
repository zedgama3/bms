# lib-core: clock, retry, validate, timeutil

- **Status:** Implemented (ported from Sojourn, D11, 2026-10-16)
- **Amendment pending implementation (2026-10-17, api.md decision 25):** `validate.FieldError`
  gained `Code` and `Params`, each check sets its code, the `Code*` constants were added
  and `Errors.Addf` takes a code. The ported code does not have this yet; it is an
  implementation follow-up (see "Proposed changes elsewhere") before `httpx` is built on it.
- **Packages:** `internal/lib/clock`, `internal/lib/retry`, `internal/lib/validate`,
  `internal/lib/timeutil` (Go module `github.com/zedgama3/bms`; foundation libraries, not bms modules)
- **Depends on specs:** `lib-errs.md` (Implemented): `errs.E`, `errs.Is`, `errs.KindOf` and the
  kinds Invalid, Unavailable, Timeout, Canceled, Internal. This spec also carries the two items
  lib-errs hands over: the retryable-kinds decision (`retry.Retryable`) and the typed field
  error (`validate.FieldError`).
  **Used by:** every layer; named consumers are listed per method below.
- **Excluded:** `lib/testkit` (separate spec, later).

## Summary

**Purpose.** One shared implementation of time, backoff, input checks and bucket math: four packages `internal/lib/clock`, `internal/lib/retry`, `internal/lib/validate`, `internal/lib/timeutil`. Imports only stdlib and `lib/errs` (`retry` also `clock` and `validate`). No tables, config keys, settings, permissions, routes or signals. `lib/testkit` is a separate, later spec.

**clock.**
- `Clock` interface: `Now()`, `Since(t)`, `After(d) <-chan time.Time`, `NewTimer(d) Timer`, `NewTicker(d) (Ticker, error)` (Invalid if d <= 0). `Timer`: `C()`, `Stop() bool`, `Reset(d) bool` (Go 1.23+ no-stale-value). `Ticker`: `C()`, `Stop()` (idempotent), `Reset(d) error` (Invalid if d <= 0).
- `Real() Clock`; `Wait(ctx, c, d) error` (nil after d; `Canceled`/`Timeout` from ctx; `Invalid` if c nil; always stops its timer).
- `Fake` (in `clock`, not testkit): `NewFake(start) *Fake`, `Advance(d) error` (fires waiters in deadline order, non-blocking sends, Invalid if d < 0), `BlockUntil(ctx, n) error` (exactly n armed waiters; Canceled/Timeout/Invalid).

**retry.**
- `Policy{Initial, Max, Multiplier, MaxAttempts}`; `Default()` = 500ms, 30s, 2, 5; `Validate()` joins `validate.FieldError`s for fields `initial`, `max`, `multiplier`, `max_attempts`; `Delay(attempt, u)` = u * min(Max, Initial * Multiplier^(n-2)), 0 for n < 2; `type Rand func() float64`.
- `Retryable(err) bool`: true only for kinds `Unavailable` and `Timeout`. Callers wrapping writes must check `db.ErrReadOnly` first (it is `Unavailable`).
- `Do(ctx, clk, p, rnd, fn) error`: non-retryable error returned unchanged; exhausted gives `fmt.Errorf("retry: gave up after %d attempts: %w")` keeping last kind; ctx done gives `errs.E("retry.Do", Canceled|Timeout, errors.Join(ctx.Err(), last))`. Pre: clk and fn non-nil, valid policy, and a ctx deadline if MaxAttempts == 0. Never logs; safe for concurrent use.
- `Backoff`: `NewBackoff(p, rnd)`, `Next() (delay, ok)`, `Attempt()`, `Reset()`; not concurrency-safe.

**validate.**
- `FieldError{Field, Code, Params, Reason}`; `Error()` is "<Field>: <Reason>"; `Fields(err) []*FieldError` walks the whole tree. `Reason` never echoes string input (numbers and durations may be echoed). Callers pass the full dotted path or variable name; the package never prefixes.
- Checks, each returning `errs.Invalid` wrapping a `FieldError`: `NonEmpty`, `Range[T cmp.Ordered]` (inclusive), `Positive(duration)`, `OneOf` (empty allowed list is `Internal`), `URL(field, v, schemes...)` (scheme and host required), `HostPort` (numeric port 1..65535), `ListenAddr` (empty host allowed).
- `Errors{Op}` accumulator (zero value usable, not concurrency-safe): `Add(err)`, `Addf(field, code, format, args...)`, `Err()` = `errs.E(Op or "validate", Invalid, errors.Join(...))`; insertion order kept.
- Code constants (stable API strings, pinned by a test): `CodeRequired` "required", `CodeRange` "range" (Params min, max), `CodePositive` "positive", `CodeOneOf` "one_of" (Params options), `CodeURL` "url" (Params schemes), `CodeHostPort` "host_port", `CodeMalformed` "malformed" (reserved for decoders such as `httpx.ReadJSON`).

**timeutil.** `MaxBuckets` = 100000; `CheckBucket(d)` (0 < d and 24h divisible by d), `CheckRange(start, end)` (both set, start before end), `Truncate`, `Align`, `Buckets`, `BucketFor(start, end, maxPoints)` (ladder 1s, 5s, 10s, 15s, 30s, 1m, 5m, 10m, 15m, 30m, 1h, 2h, 3h, 6h, 12h, 24h), `Day(t)`. All results UTC; failures are `Invalid`; DST and local time out of scope.

**Key decisions.** `Fake` lives in `clock` so one contract test covers both clocks; retry loops must be bounded by MaxAttempts or a ctx deadline; per-attempt deadlines are the caller's; `Do` returns non-retryable errors unchanged; validation reports all problems at once, outer kind always `Invalid`; `Validate` helpers take a full path and never prefix. (This spec has no numbered decisions; these are in its Behavior sections.)

**What callers handle.** `Invalid` for every bad input here; `Canceled`/`Timeout` from `Wait`, `BlockUntil` and `Do`; `Backoff.Next` returns `ok == false` (no error) when attempts are exhausted. Minimum Go version 1.24.

**Expects of dependencies.** `lib-errs`: `E`, `Is`, `KindOf`, kinds `Invalid`, `Unavailable`, `Timeout`, `Canceled`, `Internal`.

**Pending amendment (2026-10-17, api.md decision 25).** The spec text already defines `FieldError.Code` and `Params`, the `Code*` constants, per-check codes and params, and the `code` argument of `Errors.Addf`, but the ported code does not have them yet. Implementation follow-up, tests `TestChecks_SetCodeAndParams_Table`, `TestCodes_Pinned`, `TestAddf_SetsCodeNoParams`; must land before `httpx` is built on it. Consumers (`config`, `model`, `telemetry`) are not yet implemented, so nothing else changes.

## Purpose

Give every layer one shared implementation of the primitives that would otherwise be
rewritten per package: time, backoff, input checks, bucket math.

## Responsibilities

- `clock`: the only way production code reads the time or waits for it; a deterministic fake
  so tests never sleep.
- `retry`: one backoff schedule (exponential, capped, full jitter) and one retry loop that
  honors `context.Context`.
- `validate`: a handful of checks that return `errs.Invalid` with the field name, plus an
  accumulator that reports every problem at once.
- `timeutil`: bucket alignment in UTC, bucket choice for a point budget, day boundaries.

## Not responsible for

- Error kinds and wrapping: `lib/errs`.
- Fakes for anything other than the clock, fixtures, containers: `lib/testkit`.
- Deciding *what* to retry and with what budget: the caller (`notify` sender,
  `jobs` runner, a module's service) picks a `retry.Policy`; `retry` only decides whether
  an error *kind* is retryable.
- Per-attempt deadlines: the function passed to `retry.Do` derives its own
  `context.WithTimeout`. `retry` never adds deadlines to attempts.
- Scheduling, tick skipping and overrun handling: the `jobs` scheduler, which uses
  `clock.Ticker` to do it.
- Validation frameworks, struct tags, reflection: not built. Each `model` type and the
  `config` package write plain `Validate()` methods that call these helpers.
- A `TimeRange` type: `model` owns it (`model` may import `lib`; `lib` never imports `model`).
  `timeutil` works on `(start, end time.Time)` pairs.
- Choosing a storage tier (raw versus aggregated) for a query: the module's `postgres` store, using
  `timeutil.BucketFor` as input.
- Anything imported from outside the standard library (see `plan/07-abstractions.md`, layer rule 1). All four packages import only
  stdlib and `lib/errs` (`retry` also imports `lib/clock` and `lib/validate`). The import graph is acyclic:
  `errs <- clock <- retry`, `errs <- validate <- retry`, `errs <- timeutil`. (Amended
  2026-10-15: `Policy.Validate` returns `validate.FieldError`s so the API can list fields.)

## Sub-abstractions

| Name | Purpose (one line) | Notes |
|---|---|---|
| `clock.Clock` | Read the time or wait for it through an injected dependency. | `Real()` wraps `time`; `NewFake` is deterministic. |
| `clock.Fake` | Drive time by hand in tests, firing timers and tickers in deadline order. | Lives in `clock`, not `testkit` (see Behavior). |
| `retry.Policy` | Describe one backoff schedule as a validated value. | Pure; `Delay(attempt, u)` has no state. |
| `retry.Backoff` | Keep the attempt count for a long-running loop: grow on failure, reset on success. | Consumers: `jobs` runner, `notify` sender. |
| `retry.Do` | Run one operation until it succeeds, the error is not retryable, attempts run out, or ctx ends. | Consumers: `notify` sender, `db` (connect at start-up). |
| `validate.FieldError` | Carry one field's problem as a typed error the API can extract without parsing text. | Wrapped by every check; found with `errors.As` or `Fields`. |
| `validate` checks | Return `errs.Invalid` naming the field for one common input rule. | `NonEmpty`, `Range`, `Positive`, `OneOf`, `URL`, `HostPort`, `ListenAddr`. |
| `validate.Errors` | Accumulate problems so a `Validate()` reports all of them in one error. | Zero value usable; `errors.Join` inside. |
| `timeutil` buckets | Align times to UTC buckets, enumerate buckets, pick a bucket for a point budget. | Buckets must divide 24h. |
| `timeutil.Day` | Give the UTC day boundaries for per-day queries and reports. | Infallible. |

## Interface

### clock

```go
package clock

// Clock is the source of time for all production code. Real() returns the system clock;
// NewFake returns a controllable one for tests.
type Clock interface {
    // Now returns the current time. It cannot fail.
    Now() time.Time
    // Since returns Now().Sub(t). It cannot fail.
    Since(t time.Time) time.Duration
    // After returns a channel that receives the time once, after d. A non-positive d
    // fires immediately. It cannot fail. Prefer NewTimer when the caller may need to Stop.
    After(d time.Duration) <-chan time.Time
    // NewTimer returns a timer that fires once after d. A non-positive d fires at once.
    // It cannot fail.
    NewTimer(d time.Duration) Timer
    // NewTicker returns a ticker that fires every d. It fails with errs.Invalid if d <= 0.
    NewTicker(d time.Duration) (Ticker, error)
}

// Timer is a one-shot timer with time.Timer semantics (Go 1.23+: Stop and Reset guarantee
// no stale value is received afterwards).
type Timer interface {
    C() <-chan time.Time
    // Stop prevents the timer from firing. Reports whether it was still pending.
    Stop() bool
    // Reset re-arms the timer to fire after d. Reports whether it was still pending.
    Reset(d time.Duration) bool
}

// Ticker fires repeatedly with time.Ticker semantics: its channel holds one tick, and
// ticks that arrive while it is full are dropped.
type Ticker interface {
    C() <-chan time.Time
    // Stop stops the ticker. Idempotent. The channel is never closed.
    Stop()
    // Reset changes the period and restarts the ticker. Fails with errs.Invalid if d <= 0.
    Reset(d time.Duration) error
}

// Real returns the system clock. Safe for concurrent use. Cannot fail.
func Real() Clock

// Wait blocks for d or until ctx is done, whichever is first. It returns nil after d,
// errs.Canceled if ctx was canceled, errs.Timeout if ctx's deadline passed, and
// errs.Invalid if c is nil. A non-positive d returns nil immediately (after checking ctx).
func Wait(ctx context.Context, c Clock, d time.Duration) error

// Fake is a Clock that moves only when Advance is called. Safe for concurrent use.
type Fake struct { /* unexported */ }

// NewFake returns a Fake whose Now() is start. Cannot fail (a zero start is allowed).
func NewFake(start time.Time) *Fake

// Advance moves Now forward by d, firing every timer and ticker whose deadline is <= the
// new time, in deadline order. Fails with errs.Invalid if d < 0.
func (f *Fake) Advance(d time.Duration) error

// BlockUntil returns once exactly n waiters are armed (unfired timers, running tickers,
// pending After channels), or fails with errs.Canceled / errs.Timeout when ctx ends first,
// or errs.Invalid if n < 0.
func (f *Fake) BlockUntil(ctx context.Context, n int) error
```

Consumers per method:

| Method | Needed by |
|---|---|
| `Now` | `jobs` scheduler (tick timestamps), `identity` sessions (expiry), `audit` (event timestamps), `api` middleware (request duration) |
| `Since` | `jobs` scheduler (job run duration for overrun detection), `api` (request duration for `http.server.request.duration`, telemetry.md). `telemetry` itself uses `Clock` for its own rate limits (export-failure and violation warnings) and `intake` for its token buckets; it has no health endpoint (`/healthz` and `/readyz` are `api`'s). |
| `After` | `signals` and `notify` (a deadline in a `select` with an input channel) |
| `NewTimer` | `retry.Do`, `clock.Wait` (must `Stop` when ctx wins, so no goroutine or fake waiter leaks) |
| `NewTicker` | `jobs` scheduler (one per job interval), `identity` (expired-session sweep) |
| `Wait` | `retry.Do`, `jobs` runner (sleep for `Backoff.Next()` without ignoring shutdown) |
| `Fake.Advance`, `Fake.BlockUntil` | every unit test of the consumers above; `lib/testkit` |

Pre- and post-conditions:

- `NewTicker`: pre `d > 0`; post the ticker is armed and counts as one waiter on `Fake`.
- `Ticker.Reset`: pre `d > 0`; post the next tick is `Now()+d`, and a buffered tick from the
  old period is discarded (matches `time.Ticker.Reset`).
- `Timer.Stop`/`Reset`: post the channel holds no stale value.
- `Wait`: post either `Now()` has advanced by at least `d` (nil), or ctx is done (non-nil).
  The timer is always stopped on return.
- `Advance`: post `Now()` equals the old `Now()+d` exactly; every channel fired carries its
  own deadline, not the final `Now()`.

### retry

```go
package retry

// Rand returns a jitter sample in [0, 1]. Nil means math/rand/v2's Float64 (safe for
// concurrent use). A test passes func() float64 { return 1 } for the undithered schedule.
type Rand func() float64

// Policy describes an exponential backoff with a cap and full jitter.
type Policy struct {
    Initial     time.Duration // base delay before the second attempt; must be > 0
    Max         time.Duration // cap on the base delay; must be >= Initial
    Multiplier  float64       // growth per attempt; must be >= 1
    MaxAttempts int           // total attempts including the first; 0 = unlimited
}

// Default returns Policy{Initial: 500ms, Max: 30s, Multiplier: 2, MaxAttempts: 5}.
func Default() Policy

// Validate fails with errs.Invalid joining one validate.FieldError per field out of range
// (fields "initial", "max", "multiplier", "max_attempts"), so validate.Fields lists them all.
func (p Policy) Validate() error

// Delay returns the wait before attempt n (n >= 2) given jitter sample u:
// u * min(Max, Initial * Multiplier^(n-2)). It returns 0 for n < 2. u outside [0, 1] or
// NaN is treated as 1. Pure; cannot fail once p is valid.
func (p Policy) Delay(attempt int, u float64) time.Duration

// Retryable reports whether err's kind is errs.Unavailable or errs.Timeout. A nil err
// is not retryable. Cannot fail.
func Retryable(err error) bool

// Do calls fn until it returns nil, returns a non-retryable error, MaxAttempts is
// reached, or ctx is done. Waits between attempts come from p and clk.
func Do(ctx context.Context, clk clock.Clock, p Policy, rnd Rand, fn func(ctx context.Context) error) error

// Backoff keeps the failure count for a loop that must "back off after repeated failures,
// reset on success". Not safe for concurrent use: one per goroutine.
type Backoff struct { /* unexported */ }

// NewBackoff fails with errs.Invalid if p is invalid.
func NewBackoff(p Policy, rnd Rand) (*Backoff, error)

// Next records one more failure and returns the delay to wait before trying again.
// ok is false once p.MaxAttempts > 0 and that many failures have been recorded; the
// delay is then 0 and the caller decides whether to give up.
func (b *Backoff) Next() (delay time.Duration, ok bool)

// Attempt returns the number of failures recorded since the last Reset. Cannot fail.
func (b *Backoff) Attempt() int

// Reset clears the failure count after a success. Cannot fail.
func (b *Backoff) Reset()
```

Consumers per method:

| Method | Needed by |
|---|---|
| `Policy.Validate`, `Default` | `db` (its initial-connect schedule, a constant inside `db.connect_timeout`), `notify` sender (its retry settings). Not `config`: it validates its own keys with `validate` and holds no `retry.Policy` (config.md, Proposed change 10). |
| `Policy.Delay` | `Do`, `Backoff`; tests assert the schedule table-style |
| `Retryable` | `Do`; `jobs` runner (decide whether a failure is worth backing off on or is a permanent `Permission` problem to report). Note: `db.ErrReadOnly` is `Unavailable`, so `Retryable` says yes to a write refused on a read-only copy; a loop around a write must check `errors.Is(err, db.ErrReadOnly)` first (db.md decision 9). |
| `Do` | `notify` e-mail sender (SMTP sends), `db.Open` (initial connect, with `db`'s own constant policy under `db.connect_timeout`; db.md "Settings") |
| `Backoff.Next/Attempt/Reset` | `jobs` runner (`Attempt()` is the attribute on its per-run span and log line; telemetry.md Proposed change 6). Not `telemetry`: it has no health endpoint and no retry loop. |

Pre- and post-conditions for `Do`:

- pre: `clk != nil`, `fn != nil`, `p.Validate() == nil`, and if `p.MaxAttempts == 0` then
  `ctx` has a deadline (nothing waits forever).
- `fn` is called with the same `ctx` Do received. Attempt 1 runs immediately.
- post nil: some attempt returned nil.
- post non-retryable: that error is returned **unchanged** (kind preserved, no wrapping).
- post exhausted: `fmt.Errorf("retry: gave up after %d attempts: %w", n, last)`; kind is the
  last error's kind.
- post ctx done (checked before each attempt and during each wait):
  `errs.E("retry.Do", k, errors.Join(ctx.Err(), last))` where `k` is Canceled or Timeout per
  `ctx.Err()`, and `last` may be nil on the first check. `errors.Is(err, context.Canceled)`
  stays true through the join.
- A retryable error from `fn` when `ctx.Err() != nil` is treated as ctx done, not retried.

### validate

```go
package validate

// FieldError is one problem with one input field. Every check in this package returns
// errs.E("validate.<Check>", errs.Invalid, &FieldError{...}), so callers branch with
// errs.Is and the api's mapper (through lib/httpx) extracts fields with errors.As or
// Fields. Reason is safe to show to a user: it never echoes string input (which may be a
// secret or a database URL); it may echo numbers and durations. Code and Params exist so
// the web shell can translate the problem (api.md decision 25, owner 2026-10-17): Code is
// a stable identifier from the list below, Params the rule's parameters as strings,
// never the input.
type FieldError struct {
    Field  string            // the caller's full dotted path: "db.url", "smtp.host"
    Code   string            // "required", "range", "positive", "one_of", "url", "host_port", "malformed"; "" when a caller set none
    Params map[string]string // the rule's parameters: Range {"min","max"}, OneOf {"options": comma-joined}; nil otherwise
    Reason string            // what is wrong, in English: "must be positive, got -1s"
}

// Error returns "<Field>: <Reason>". Cannot fail.
func (e *FieldError) Error() string

// Fields returns every *FieldError in err's tree, depth-first, left to right, descending
// through Unwrap() error and Unwrap() []error. nil or no matches returns an empty slice.
// Cannot fail.
func Fields(err error) []*FieldError

// Each check returns nil or errs.Invalid wrapping a FieldError. field is the caller's full
// dotted path; this package never prefixes. Each check sets the Code named in its comment.

// The codes this package sets, and the one it reserves. A code is a stable API string
// (the web shell translates it under the key field.<code>); a test pins the list.
const (
    CodeRequired  = "required"  // NonEmpty
    CodeRange     = "range"     // Range; Params min, max (formatted as the check echoes them)
    CodePositive  = "positive"  // Positive
    CodeOneOf     = "one_of"    // OneOf; Params options (comma-joined, in the caller's order)
    CodeURL       = "url"       // URL; Params schemes (comma-joined) when schemes were given
    CodeHostPort  = "host_port" // HostPort, ListenAddr
    CodeMalformed = "malformed" // reserved for decoders: httpx.ReadJSON on a body that is not the expected JSON
)

// NonEmpty fails if strings.TrimSpace(value) == "". Code "required".
func NonEmpty(field, value string) error

// Range fails if v < lo or v > hi (inclusive bounds). Works for ints, floats, durations.
// Code "range", Params {"min": lo, "max": hi} formatted with %v.
func Range[T cmp.Ordered](field string, v, lo, hi T) error

// Positive fails if d <= 0. Code "positive".
func Positive(field string, d time.Duration) error

// OneOf fails if value is not exactly one of allowed. Fails with errs.Internal if allowed
// is empty (a caller bug, not bad input). Code "one_of", Params {"options": allowed
// joined with ", "}.
func OneOf(field, value string, allowed ...string) error

// URL fails if value does not parse as an absolute URL with a non-empty host, or if
// schemes is non-empty and the scheme (case-insensitive) is not in it. Code "url";
// Params {"schemes": joined with ", "} when schemes were given.
func URL(field, value string, schemes ...string) error

// HostPort fails unless value is "host:port" or "[v6]:port" with a non-empty host and a
// numeric port in 1..65535. Code "host_port".
func HostPort(field, value string) error

// ListenAddr is HostPort with an empty host allowed (":8080" means all interfaces). Code
// "host_port".
func ListenAddr(field, value string) error

// Errors accumulates problems. The zero value is ready to use. Not safe for concurrent use.
type Errors struct {
    Op string // operation for errs.E; "validate" if empty
    // unexported problems []error
}

// Add records err; a nil err is ignored. Cannot fail.
func (e *Errors) Add(err error)

// Addf records errs.Invalid wrapping &FieldError{Field: field, Code: code, Reason:
// fmt.Sprintf(format, args...)} with no Params. code is one of the constants above or a
// caller's own stable string (a module's "overlap", say); "" is allowed and means the
// web shell shows the reason. Cannot fail.
func (e *Errors) Addf(field, code, format string, args ...any)

// Err returns nil if nothing was added, otherwise
// errs.E(Op, errs.Invalid, errors.Join(problems...)). Cannot fail.
func (e *Errors) Err() error
```

Consumers: `config` (`URL`, `ListenAddr`, `Range`, `OneOf`, `NonEmpty`, `Errors`, `Fields`;
config.md, Proposed change 10);
`model` types' `Validate()` methods (`NonEmpty`, `OneOf`, `Range`, `Errors`); module services
validating requests (`NonEmpty`, `Range`, `Errors`); `notify` settings (`HostPort` for the SMTP
server); `identity` (`URL` for the OIDC issuer);
`retry.Policy.Validate` (`Positive`, `Range`, `Errors`); `httpx` (`FieldError`, `Fields`:
`Fail`'s fallback and the api's mapper build the 400 body's per-field list `{field, code,
params, reason}` without parsing messages, as lib-errs requires; `ReadJSON` builds a
`FieldError` with code `malformed` or `required` for a bad or empty body; api.md); `cmd/bms`/`app`
start-up (prints `Fields(err)` one per line when config is invalid).

### timeutil

```go
package timeutil

// MaxBuckets bounds the slices Buckets returns.
const MaxBuckets = 100_000

// CheckBucket fails with errs.Invalid unless 0 < d and 24h is a whole multiple of d.
func CheckBucket(d time.Duration) error

// CheckRange fails with errs.Invalid if start or end is the zero time, or !start.Before(end).
func CheckRange(start, end time.Time) error

// Truncate returns t rounded down to a bucket boundary, in UTC. Boundaries are aligned to
// midnight UTC, so every valid bucket tiles the day exactly. Fails per CheckBucket.
func Truncate(t time.Time, bucket time.Duration) (time.Time, error)

// Align expands [start, end) outward to bucket boundaries: start rounded down, end rounded
// up (an end already on a boundary is unchanged). Fails per CheckRange and CheckBucket.
func Align(start, end time.Time, bucket time.Duration) (time.Time, time.Time, error)

// Buckets returns the bucket start times covering [start, end): Truncate(start) stepping
// by bucket while < end. Fails per CheckRange and CheckBucket, and with errs.Invalid if
// more than MaxBuckets would be returned.
func Buckets(start, end time.Time, bucket time.Duration) ([]time.Time, error)

// BucketFor returns the smallest bucket on the standard ladder for which
// len(Buckets(start, end, bucket)) <= maxPoints. Fails per CheckRange, with errs.Invalid if
// maxPoints < 1, and with errs.Invalid if even 24h exceeds maxPoints.
func BucketFor(start, end time.Time, maxPoints int) (time.Duration, error)

// Day returns the UTC day containing t as [start, end). Cannot fail: 24h is always a valid
// bucket and the zero time has a day.
func Day(t time.Time) (start, end time.Time)
```

Consumers per function:

| Function | Needed by |
|---|---|
| `CheckBucket`, `CheckRange` | module stores and services validating a report bucket or range |
| `Truncate` | module services (period cut-offs, same bucket last week) |
| `Align` | module stores so SQL predicates cover whole buckets |
| `Buckets` | module reports (dense series with zero-filled gaps for charts) |
| `BucketFor` | `api` handlers and module reports (choose a bucket for the client's width) |
| `Day` | module stores (per-day query bounds), per-day reports |

## Types

- `clock.Clock`, `clock.Timer`, `clock.Ticker` (interfaces), `clock.Fake`.
- `retry.Policy` (value), `retry.Rand` (func), `retry.Backoff`.
- `validate.Errors`.
- `timeutil` defines no types; it uses `time.Time` and `time.Duration` only.

## Behavior

### clock

- `Real()` returns one immutable value wrapping the `time` package. Its `Timer` and `Ticker`
  delegate to `time.Timer`/`time.Ticker`; `NewTicker` and `Ticker.Reset` check `d <= 0`
  before delegating, so the stdlib panic is never reached.
- Requires **Go 1.23+** so `time.Timer` has the no-stale-value guarantee the `Timer`
  interface promises.
- **Where `Fake` lives: in `clock`, not `testkit`.** Reasons: (1) `Fake` must track the
  `Timer`/`Ticker` contract exactly, so one shared table test in `clock` runs against both
  `Real()` and `NewFake` and they cannot drift apart; (2) `testkit` will import `clock` for
  its fixtures, so a `Fake` in `testkit` would make `clock`'s own tests import their importer;
  (3) `Fake` imports nothing test-only (no `testing`), so it costs the binary nothing and
  is dead-code-eliminated.
- **`Fake` semantics, precisely:**
  - State: `now`, and an ordered set of waiters. A waiter is a one-shot (from `After` or
    `NewTimer`) or a periodic (from `NewTicker`), each with a deadline and a creation
    sequence number.
  - `Now()` returns `now`. `Since(t)` is `now.Sub(t)`.
  - `NewTimer(d)` / `After(d)`: channel capacity 1. If `d <= 0` the channel is filled with
    `now` immediately and the timer is not a waiter. Otherwise deadline `now+d`.
  - `NewTicker(d)`: channel capacity 1, deadline `now+d`, period `d`.
  - `Advance(d)`: let `target = now + d`. Repeat: pick the waiter with the smallest deadline
    `<= target` (ties by creation sequence); set `now` to its deadline; deliver that deadline
    on its channel with a **non-blocking send** (a full channel drops the value, as
    `time.Ticker` does); a one-shot is removed, a periodic gets `deadline += period`. When
    no waiter is due, set `now = target`. A ticker with period `p` therefore attempts
    `floor(d/p)` deliveries on `Advance(d)`, but a test that does not drain between them
    receives one. Delivery happens synchronously on the caller's goroutine, so after
    `Advance` returns every due channel has been offered its value.
  - `Timer.Stop`: removes the waiter and drains the channel; reports whether it was armed.
    `Timer.Reset(d)`: `Stop`, then re-arm as `NewTimer(d)` (fires immediately if `d <= 0`).
  - `Ticker.Stop`: removes the waiter, drains the channel. `Ticker.Reset(d)`: validates,
    drains, sets `deadline = now + d`, `period = d`.
  - `BlockUntil(ctx, n)`: returns as soon as the waiter count equals `n`, including when it
    already does. Implemented with a condition variable or a change-notification channel,
    not polling. Exactly-`n` (not at-least) so tests can also assert a timer was stopped
    (`BlockUntil(ctx, 0)`). A test that hands the wrong `n` fails via its ctx deadline, not
    by hanging.
  - Concurrency: all methods are mutex-guarded and safe to call from any goroutine. Two
    concurrent `Advance` calls are serialized; their relative order is unspecified, so
    tests drive time from one goroutine.
- `Wait` creates one `Timer`, selects on it and `ctx.Done()`, and stops the timer on every
  path so the `Fake` waiter count goes back down.

### retry

- `Delay`: with `u = 1` the schedule is `Initial, Initial*M, Initial*M^2, …` capped at `Max`.
  The base is computed in `float64` and stops growing once it reaches `Max`, so no overflow
  for large attempt numbers.
- Full jitter means a delay can be 0. That is acceptable because every loop is bounded by
  `MaxAttempts` or a ctx deadline (`Do`) or by the caller's shutdown ctx (`Backoff`).
- `Do` sequence: check ctx; attempt; if nil return; if `!Retryable(err)` return err; if
  `ctx.Err() != nil` return ctx-done error; if `MaxAttempts > 0 && n == MaxAttempts` return
  exhausted error; `clock.Wait(ctx, clk, p.Delay(n+1, rnd()))`; on Wait error return the
  ctx-done error (joined with last); loop.
- `Do` is safe for concurrent use (no shared state). `Backoff` is not; one per goroutine.
- `Do` never logs. The caller logs the final error once, where it handles it.
- `retry` makes no HTTP, SQL or sleep calls of its own: all waiting is through `clock`.

### validate

- Every problem is a `*FieldError` inside `errs.Invalid`. Tests and the API read `Field`,
  `Code`, `Params` and `Reason` through `errors.As`/`Fields`; nothing parses `Error()`
  text. `Error()` stays `<Field>: <Reason>`; the code is not in it.
- `Code` is set by every check in this package (the constant list above) and by `Addf`'s
  caller; `Params` hold only the rule's parameters (bounds, options, schemes) as strings,
  never the value being checked, so they are as safe to show as `Reason`. A caller that
  builds a `FieldError` by hand may leave `Code` empty; the web shell then shows `Reason`
  (api.md decision 25).
- `Reason` never echoes string input: `NonEmpty`, `OneOf`, `URL`, `HostPort` and
  `ListenAddr` describe the rule ("must be one of: local, oidc") without the value.
  Numbers and durations are echoed: `max_connections: must be between 1 and 1000, got 0`.
- `Fields` walks the whole tree so an `Errors.Err()` with twelve problems yields twelve
  `FieldError`s in insertion order; a `FieldError` wrapped by a caller's `fmt.Errorf("%w")`
  is still found.
- `URL` uses `net/url.ParseRequestURI`-style strictness: scheme and host required, so
  `"localhost:8080"` (parsed as scheme `localhost`) is rejected because it has no host.
- `HostPort`/`ListenAddr` use `net.SplitHostPort` then `strconv.Atoi` on the port; service
  names (`"host:https"`) are rejected (numeric only), since drivers take numbers.
- `Errors` preserves insertion order. `Err()` wraps once, so `errs.Is(err, errs.Invalid)` is
  true and `errors.Is(err, someProblem)` still finds individual problems through the join.
- Problems added to `Errors` keep their own kinds inside the join; the outer kind is always
  `Invalid`, because a `Validate()` result is by definition "bad input".

### timeutil

- All returned times have `Location() == time.UTC`. Inputs in any zone are converted first.
  Monotonic clock readings are stripped by the conversion.
- Alignment uses Unix seconds: `Truncate(t, d)` is `time.Unix(0, (t.UnixNano()/d)*d).UTC()`
  for `t >= epoch`, with floor semantics for `t < epoch`. Because every valid bucket divides
  24h, buckets start at midnight UTC and never straddle a day boundary.
- Standard ladder for `BucketFor`: 1s, 5s, 10s, 15s, 30s, 1m, 5m, 10m, 15m, 30m, 1h, 2h,
  3h, 6h, 12h, 24h. Every entry divides 24h. The ladder is a package-private slice; it is
  documented here so a consumer can map a bucket size to its own storage or display tiers
  (that mapping is the consumer's rule, not this package's).
- `BucketFor` with 2 years and `maxPoints = 300` fails (731 days at 24h) rather than
  silently returning more points than asked. The caller clamps the range or raises the
  budget.
- Local time, DST and calendar months are out of scope; the product's buckets are UTC and
  the UI converts for display.

## Failure modes

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F1 | `Clock.NewTicker` (Real and Fake) | `d <= 0` | Invalid | fix config validation | `TestNewTicker_NonPositive_ReturnsInvalid` |
| F2 | `Ticker.Reset` (Real and Fake) | `d <= 0` | Invalid | fix config validation | `TestTickerReset_NonPositive_ReturnsInvalid` |
| F3 | `clock.Wait` | ctx canceled before `d` | Canceled | stop the loop | `TestWait_ContextCanceled_ReturnsCanceled` |
| F4 | `clock.Wait` | ctx deadline before `d` | Timeout | treat as attempt budget spent | `TestWait_DeadlineExceeded_ReturnsTimeout` |
| F5 | `clock.Wait` | `c == nil` | Invalid | fix wiring | `TestWait_NilClock_ReturnsInvalid` |
| F6 | `Fake.Advance` | `d < 0` | Invalid | fix the test | `TestFakeAdvance_Negative_ReturnsInvalid` |
| F7 | `Fake.BlockUntil` | ctx ends before count == n | Canceled / Timeout | fail the test with the error | `TestFakeBlockUntil_ContextDone_ReturnsKind` |
| F8 | `Fake.BlockUntil` | `n < 0` | Invalid | fix the test | `TestFakeBlockUntil_Negative_ReturnsInvalid` |
| F9 | `Policy.Validate` | `Initial <= 0`, `Max < Initial`, `Multiplier < 1`, or `MaxAttempts < 0` (all reported together) | Invalid | fix config | `TestPolicyValidate_EachField_ReturnsInvalid`, `TestPolicyValidate_ManyProblems_ReportsAll` |
| F10 | `retry.Do` | `p` invalid | Invalid | fix config | `TestDo_InvalidPolicy_ReturnsInvalid` |
| F11 | `retry.Do` | `clk == nil` or `fn == nil` | Invalid | fix wiring | `TestDo_NilClock_ReturnsInvalid`, `TestDo_NilFn_ReturnsInvalid` |
| F12 | `retry.Do` | `MaxAttempts == 0` and ctx has no deadline | Invalid | add a deadline or a budget | `TestDo_UnlimitedWithoutDeadline_ReturnsInvalid` |
| F13 | `retry.Do` | `fn` returns non-retryable kind | that kind, unchanged | handle by kind | `TestDo_NonRetryable_ReturnsUnchangedAfterOneAttempt` |
| F14 | `retry.Do` | `MaxAttempts` retryable failures | last error's kind | log once, record gap | `TestDo_Exhausted_ReturnsLastKindWithAttemptCount` |
| F15 | `retry.Do` | ctx canceled during a wait | Canceled (joined with last error) | shut down | `TestDo_CanceledDuringWait_ReturnsCanceledJoinedWithLast` |
| F16 | `retry.Do` | ctx deadline during a wait, or `fn` returns retryable while `ctx.Err() != nil` | Timeout | give up | `TestDo_DeadlineDuringWait_ReturnsTimeout`, `TestDo_RetryableAfterCtxDone_NotRetried` |
| F17 | `retry.NewBackoff` | `p` invalid | Invalid | fix config | `TestNewBackoff_InvalidPolicy_ReturnsInvalid` |
| F18 | `Backoff.Next` | attempts exhausted | none (`ok == false`, delay 0) | decide to give up or `Reset` | `TestBackoffNext_Exhausted_ReturnsNotOK` |
| F19 | `validate.NonEmpty` | blank or whitespace | Invalid | report | `TestNonEmpty_Blank_ReturnsInvalid` |
| F20 | `validate.Range` | below `lo` or above `hi` | Invalid | report | `TestRange_OutOfBounds_ReturnsInvalid` |
| F21 | `validate.Positive` | `d <= 0` | Invalid | report | `TestPositive_NonPositive_ReturnsInvalid` |
| F22 | `validate.OneOf` | value not allowed | Invalid | report | `TestOneOf_NotAllowed_ReturnsInvalid` |
| F23 | `validate.OneOf` | `allowed` empty | Internal | fix the caller | `TestOneOf_NoAllowed_ReturnsInternal` |
| F24 | `validate.URL` | unparsable, no scheme, no host, or scheme not permitted | Invalid | report | `TestURL_Malformed_ReturnsInvalid`, `TestURL_SchemeNotAllowed_ReturnsInvalid` |
| F25 | `validate.HostPort` | no port, empty host, non-numeric or out-of-range port | Invalid | report | `TestHostPort_Malformed_ReturnsInvalid` |
| F26 | `validate.ListenAddr` | no port, non-numeric or out-of-range port | Invalid | report | `TestListenAddr_BadPort_ReturnsInvalid` |
| F27 | `Errors.Err` | one or more problems added | Invalid (joined) | show every problem, refuse to start | `TestErrorsErr_ManyProblems_JoinsAllAsInvalid` |
| F28 | `timeutil.CheckBucket` (and every function that calls it) | `d <= 0` or `24h % d != 0` | Invalid | reject the request | `TestCheckBucket_NonDivisor_ReturnsInvalid` |
| F29 | `timeutil.CheckRange` (and callers) | zero start or end, or `start >= end` | Invalid | reject the request | `TestCheckRange_ZeroOrInverted_ReturnsInvalid` |
| F30 | `timeutil.Buckets` | would exceed `MaxBuckets` | Invalid | pick a bigger bucket | `TestBuckets_TooMany_ReturnsInvalid` |
| F31 | `timeutil.BucketFor` | `maxPoints < 1` | Invalid | fix the caller | `TestBucketFor_NoBudget_ReturnsInvalid` |
| F32 | `timeutil.BucketFor` | range too long for `maxPoints` even at 24h | Invalid | clamp the range | `TestBucketFor_RangeTooLong_ReturnsInvalid` |

Methods with no failure modes, and why: `Now`, `Since`, `After`, `NewTimer`, `Timer.Stop`,
`Timer.Reset`, `Ticker.Stop`, `Real`, `NewFake` (any start is a valid instant),
`Policy.Delay` (pure arithmetic on a validated policy; out-of-range `u` is defined as 1),
`Default`, `Retryable` (reads a kind), `Backoff.Attempt`, `Backoff.Reset`, `Errors.Add`,
`Errors.Addf`, `FieldError.Error`, `Fields` (a tree walk that returns an empty slice for nil
or unmatched input), `Day` (24h is always a valid bucket). Each still has a success-path
table test.

Deliberately not errors: a `Fake` ticker dropping a tick on a full channel is the specified
`time.Ticker` behavior, tested by `TestFakeTicker_LargeAdvanceUndrained_DeliversOne`.

## Lock-in check

No external technology. `clock.Real()` wraps the Go standard library `time` package; the
interface is proven portable by `clock.Fake`, a second complete implementation in the same
package. `retry.Rand`'s default is `math/rand/v2`; any `func() float64` replaces it.

## Test plan

Unit only, no infrastructure, `go test -race`, no `time.Sleep` anywhere (the one exception is
`Real()`'s own smoke test, which uses sub-millisecond timers with generous bounds and asserts
only ordering, never exact durations).

- `clock`: a shared table test exercising `Timer`/`Ticker` contracts against both `Real()`
  and `NewFake`. Fake-specific tests: `TestFakeAdvance_FiresInDeadlineOrder`,
  `TestFakeAdvance_TiesFireInCreationOrder`, `TestFakeAdvance_TickerFiresRepeatedlyWhenDrained`,
  `TestFakeAdvance_SetsNowExactly`, `TestFakeTimer_StopThenAdvance_NoDelivery`,
  `TestFakeTimer_ResetDrainsStaleValue`, `TestFakeBlockUntil_ReturnsWhenCountReached`,
  `TestFakeBlockUntil_ZeroAfterStop`, `TestWait_ElapsesOnFake`, plus F1–F8.
- `retry`: `TestDelay_Schedule` (table: attempt, u, expected), `TestDelay_CapsAtMax`,
  `TestDelay_LargeAttemptNoOverflow`, `TestDelay_RandOutOfRange_TreatedAsOne`,
  `TestDo_SucceedsFirstTry_NoWait`, `TestDo_RetriesUnavailableThenSucceeds` (uses
  `clock.Fake` with `BlockUntil(ctx, 1)` then `Advance`), `TestBackoff_GrowsThenResets`,
  plus F9–F18.
- `validate`: one table test per check with valid and invalid rows; each invalid row asserts
  `errs.Is(err, errs.Invalid)` and extracts the `*FieldError` with `errors.As` to check
  `Field`, `Code` and `Params`; plus F19–F27, `TestErrorsErr_Empty_ReturnsNil`,
  `TestFields_JoinedAndWrapped_ReturnsAllInOrder`, `TestFields_NilOrUnmatched_ReturnsEmpty`,
  `TestReason_StringChecks_DoNotEchoValue`, `TestChecks_SetCodeAndParams_Table` (one row
  per check: the code, the params, and that no param equals the checked string value),
  `TestCodes_Pinned` (the `Code*` constants are stable API strings),
  `TestAddf_SetsCodeNoParams`.
- `timeutil`: tables with explicit UTC and non-UTC inputs. `TestTruncate_AlignsToMidnightUTC`,
  `TestTruncate_NonUTCInputReturnsUTC`, `TestTruncate_BeforeEpochFloors`,
  `TestAlign_EndOnBoundaryUnchanged`, `TestBuckets_CoversHalfOpenRange`,
  `TestBucketFor_PicksSmallestFitting` (table over the ladder), `TestDay_Boundaries`, plus
  F28–F32.

## Open questions

None. The one dependency that was uncertain while drafting (whether `lib-errs` exports
`Canceled`) is settled by the approved `lib-errs.md`.

## Proposed changes elsewhere

- The minimum Go version is **1.24** (1.23 for the timer no-stale-value semantics
  `clock.Timer` relies on; 1.24 for `t.Context()` in tests, which is how `Fake.BlockUntil`
  tests get a deadline without `time.Sleep`). `go.mod` (1.26) already satisfies this.
- `plan/design/jobs.md` (when written): the scheduler takes `clock.Clock`; the runner takes a
  `retry.Policy` and owns the "give up after N" decision using `Backoff.Attempt()`; its spec
  should state N and whether `MaxAttempts` is 0.
- Report-producing module specs that bucket by time should use `timeutil.CheckBucket` and
  `timeutil.BucketFor`.
- **Implementation follow-up (2026-10-17, from api.md Proposed change 8, decision 25):**
  add `Code` and `Params` to `validate.FieldError`, the `Code*` constants, the code and
  params each check sets, and the `code` parameter of `Errors.Addf` (its only callers today
  are inside `lib`); tests `TestChecks_SetCodeAndParams_Table` (one row per check: the
  code, the params, and that no param equals the checked value) and `TestCodes_Pinned`
  (the constants are stable API strings). `config`, `model` and `telemetry` are not yet
  implemented, so nothing else changes.
