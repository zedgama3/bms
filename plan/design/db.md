# db: database access

- **Status:** Implemented (2026-10-17); Approved (2026-10-16)
- Amended 2026-10-17 (owner): `Write` begins with plain `BEGIN` so the session's `default_transaction_read_only` applies (server answers 25006); a NULL uuid scans into `*model.ID[E]`; accepted implementation deviations recorded in Behavior, "Accepted implementation deviations".
- **Amended 2026-10-18 (owner approval of audit/identity):** Proposed change 10 applied for `audit` and `identity` (`postgres.Migrations`, `audit.Set`, `identity.Set`, schema `core`); audit's guard deviates from "insert and select only": `UPDATE` and `TRUNCATE` are revoked from the application role and a trigger refuses `UPDATE`, `DELETE` and `TRUNCATE`, with `DELETE` allowed only inside `core.audit_purge` through a transaction-local flag, because the installation has one role and a plain grant cannot tell the retention job from the app (audit decision 9).
- **Amended 2026-10-18 (owner approval of authz/people):** Proposed change 10 applied for `authz` and `people` (`postgres.Migrations`, `authz.Set`, `people.Set`, schema `core`, set names `authz` and `people` already in the fixed order); people's migration `0001` reads `core.account` once to backfill a person row per existing account, the one read across sets (authz.md Proposed change 16, people.md Proposed change 10). The transaction seam is not changed here.
- **Package:** `internal/core/db` (Go module `github.com/zedgama3/bms`), plus the test
  helper package `internal/core/db/dbtest`. **UI:** none of its own; the "this copy is
  read-only" banner and `/readyz` are `web-shell.md`'s and `api.md`'s, fed by `ReadOnly()`
  and `Ping`.
- **Depends on specs:** `lib-errs.md` (Implemented): `errs.E`, `errs.Is`, `errs.KindOf`,
  every kind but `Unauthenticated`, `Permission` and `Unsupported`. `lib-core.md`
  (Implemented): `clock.Clock`, `retry.Do`, `retry.Policy`, `validate.Errors`,
  `validate.FieldError`, `validate.Range`, `validate.URL`. `lib-secret.md` (Implemented):
  `secret.Secret`, `Value`, `IsSet`. `model.md` (Approved): `model.ID[E]` and `model.Email`
  are scanned by underlying type (Proposed change 4 there; proven in the test plan).
  `config.md` (Approved): `Options` mirrors `config.DB` (its decision 12 and Proposed change
  4). `module-contract.md` (Approved): `SchemaChecker` (its Proposed change 4), `Deps.DB`,
  `Manifest.Migrations`, `Plan.Order()`. `telemetry.md` (Approved, 2026-10-16; its Proposed
  change 2 answered this spec's Proposed change 12 the same day): `telemetry.Tracer`,
  `telemetry.Meter`, `telemetry.Span`, `WithKind`, `WithAttrs`, `Token`, `Int`, `Meter.Gauge`,
  `Sample`, `WithUnit` (statement spans and pool gauges).
  **Used by:** `app` (`Open`, `Close`, `NewMigrator`, the `migrate` and `check` commands,
  `Deps.DB`), `api` (`Ping` and `ReadOnly` for `/readyz`; the `ErrReadOnly` mapping),
  `core/module` (`SchemaChecker`, structurally), every core package with tables
  (`identity`, `people`, `departments-events`, `authz`, `audit`, `settings`, `jobs`,
  `files`: each has a `postgres/` package over `DB` and a migration `Set`), every module's
  `postgres/` package (`DB`, `Transact`, `Wrap`, `*Error`), `signals` (`AfterCommit`),
  `moduletest` and module tests (`dbtest`).

## Summary

Purpose: give every `postgres/` package one bounded, classified way to reach PostgreSQL: the
pool, statements with deadlines, transactions, error classification, read-only detection and
the migration runner. Only `db` and `postgres/` packages import pgx (depguard `storage-seam`).
Owns one table, `core.schema_migration` (no personal data, kept forever).

Exported names (package `db`):
- `Options{URL secret.Secret, MaxConns 1..1000, ConnectTimeout 1s..10m, QueryTimeout
  100ms..10m, Tracer, Meter (both optional)}` with `Validate` (fields `db.url`, `db.max_conns`,
  `db.connect_timeout`, `db.query_timeout`); `MinServerVersion = 160000`.
- `Open(ctx, Options, clock.Clock, *slog.Logger) (*Pool, error)`; `*Pool` implements `DB` and
  adds `Ping(ctx) error`, `Close(ctx) error` (idempotent), `Stats() Stats`.
- `Querier{Exec, Query, QueryRow}` returning pgx types, errors already classified; `DB` =
  `Querier` + `Transact(ctx, Mode, func(ctx) error) error` + `ReadOnly() bool`; `Mode`
  (`Read`, `Write`; zero invalid).
- `None() DB` and `ErrNoDatabase`: a `DB` that refuses every statement with `Unavailable` wrapping the sentinel (`ReadOnly()` false, `AfterCommit` outside a transaction runs at once), for `bms openapi` and `bms check` (app.md decision 7).
- `AfterCommit(ctx, fn) error`: runs fn after the ctx's transaction commits, at once when
  there is none.
- `Wrap(op, err) error` (idempotent classifier, nil in nil out), `*Error{Code, Schema, Table,
  Column, Constraint}` (names only), `ErrReadOnly` and `ErrNoDatabase` sentinels.
- Migrations: `Set{Name, Schema, Files fs.FS}` (+`Validate`), `Version(fs.FS) (int, error)`,
  `NewMigrator(*Pool, logger)`, `Migrate(ctx, []Set) ([]Applied, error)`, `Status(ctx, []Set)
  ([]Status, error)`, `CheckSchema(ctx, module, fs.FS) error` (implements
  `module.SchemaChecker` structurally).
- `dbtest`: `Fake` (`NewFake`, `SetReadOnly`, `Calls`, `Call{SQL, Args, InTx}`; no rows,
  `QueryRow.Scan` is NotFound, Transact runs hooks after fn), `TempDB(t) *db.Pool`
  (integration tag, needs `BMS_TEST_DB_DSN`, `CREATEDB`).

Key decisions: 1 db speaks pgx, the storage seam is each service's interface (D8); 2
`Deps.DB` is an interface (`*Pool`, `dbtest.Fake`); 3 every error leaving db is classified,
`Wrap` is the one mapping; 4 server errors scrubbed to SQLSTATE and names, never
message/detail/hint; 5 the transaction travels in the ctx (fn's ctx must not cross
goroutines); 6 nested `Transact` joins the outer, no savepoints; 7 read-only = in recovery or
`default_transaction_read_only` (owner, 2026-10-16); 8 state cached, refreshed by a 15 s
watcher, `Ping`, and every `25006`; 9 refused write is `Unavailable` wrapping `ErrReadOnly`;
10 one transaction per migration file with its row (owner, 2026-10-16); 11 no
non-transactional migrations yet; 12 SHA-256 checksums recorded and checked; 13 migrator
creates each set's schema; 14 pgx `PG*` variables and pgpass honoured for what the URL omits;
15 `Open` retries only Unavailable/Timeout; 16 every statement gets `min(ctx deadline,
QueryTimeout)`, `Migrate` excepted; 17 `Migrate` takes a session advisory lock, fails fast;
18 missing `core.schema_migration` means nothing applied; 19 `TempDB` is a database not a
schema; 20 PostgreSQL 16 minimum (owner, 2026-10-16).

Errs kinds and sentinels callers handle: `Invalid` (bad options/URL/credentials/old server,
FK/not-null/check/data errors, malformed migrations), `Conflict` (unique violation with
`*Error.Constraint`, concurrent `Migrate`, schema version or checksum mismatch), `NotFound`
(`QueryRow` no rows), `Unavailable` (connection loss, serialization/deadlock, closed pool,
read-only), `Timeout`/`Canceled`, `Internal` (bad SQL `42xxx`, unknown). Check
`errors.Is(err, db.ErrReadOnly)` before the kind: `retry.Retryable` would retry it. `db` never
retries a statement or transaction. Stores re-kind `23503` to `Conflict` for "in use".

Config and surfaces owned: reads no env itself; `app` copies `config.DB` (`db.url`,
`db.max_conns`, `db.connect_timeout`, `db.query_timeout`) into `Options`. No settings,
permissions, signals or routes; supplies `ReadOnly`/`Ping` for `/readyz` and the banner and
`Status` for the admin modules screen. Constants: connect retry 500ms to 5s doubling, probe
interval 15 s, probe timeout 5 s, advisory lock key from `bms.migrate`, `application_name`
`bms`. Telemetry: a client span `db.<OPERATION>` per statement (no text or args), gauges
`db.client.connection.count|max|pending_requests` from `Stats`.

Migration file rules: `NNNN_name.sql` at the set root, contiguous from `0001`, highest is the
version, never edited after release; core sets use schema `core`, a module set uses its name.

Expects of dependencies: lib-errs (kinds), lib-core (`clock`, `retry.Do`, `validate`),
lib-secret (`Secret`), model (`ID[E]`, `Email` scanned by underlying type), config (`Options`
mirrors `config.DB` bounds), module-contract (`SchemaChecker`, `Deps.DB`, `Manifest.Migrations`,
`Plan.Order()`), telemetry (`Tracer`, `Meter`, spans, gauges). Scanning: a NULL uuid column scans into `*model.ID[E]` (nil = none). Expects of consumers: `app` opens
once, calls `Close` after `Set.Stop` and the HTTP server; core packages export `Set`s.

Accepted deviations (2026-10-17, see Behavior): `Write` is plain `BEGIN`; the probe also reads `pg_db_role_setting` and the pool is Reset on a state change; a `25006` in a Read tx owned by the pool is `Internal` and flips nothing; a closed pool is `Unavailable`; `Write` nested in `Read` and `AfterCommit` after an ended tx are `Invalid`; `Migrate` returns the files applied so far with the error; `Wrap` keeps only `%T` for unknown errors.

Pending proposed changes (targets not yet written): 10 core specs with tables export a migration `Set` (applied for `audit` and `identity`, 2026-10-18), 11
`docs/operating.md`, 13 jobs/notify read `ReadOnly()` and stop on `ErrReadOnly`.

## Purpose

Give every `postgres/` package one bounded, classified way to reach the installation's
PostgreSQL database.

## Responsibilities

- Open the **pool** from validated options, with a bounded, retried initial connect, and
  close it cleanly.
- Run **statements** on the pool or on the enclosing **transaction**, each with a deadline,
  and run a function inside a transaction with after-commit hooks.
- **Classify** every driver error into an `errs` kind at the boundary, without letting a
  parameter value or a row value into the error text.
- Know whether this copy is **read-only** (D27, 09 ground rules), refuse writes fast when it
  is, and expose the state for `/readyz`.
- Apply **migrations**: each set's forward-only, numbered files in order, the core's sets
  first, then the enabled modules' (D17); record every file that ran in
  `core.schema_migration`; refuse to run on a read-only database or beside another run.
- **Check schema versions** for `module.Build` (D17): the version recorded for a set equals
  the version the binary was built with, and no applied file was changed.
- Give tests a **fake `DB`** that needs no database and a **temporary real database** for
  integration tests.

## Not responsible for

| Concern | Owner |
|---|---|
| Any table other than `core.schema_migration`; any module's or core service's queries, schema design, indexes, pagination | the owning spec's `postgres/` package (D8). `db` owns no domain table. |
| The storage interface a service declares, and keeping SQL out of services and handlers | each service (D8); the `postgres-sql` and `add-module` skills; `go-reviewer`. |
| Deciding whether a foreign-key violation is a bad reference or "in use" | the store that ran the statement: `db` maps `23503` to `Invalid` by default and the store re-kinds to `Conflict` with `errs.E` where it knows better (`postgres-sql`). |
| Turning a constraint name into a field error for the API | the store (`*Error.Constraint` gives it the name). |
| Loading `db.url` and the other `db.*` keys from the environment | `config` (`config.md`); `app` copies `config.DB` into `Options`. |
| Deciding whether a write is allowed for the person asking | `authz`. `db` knows no actor. |
| Auditing reads or writes | `audit`. |
| Delivering signals after commit; the outbox or buffer that holds them until then | `signals` (`signals.md`). `db` provides only the hook, `AfterCommit`. |
| Retrying a failed transaction (serialization failure, deadlock) | the caller; nobody in the MVP (decision 9). `db` returns `Unavailable` and the api answers 503. |
| Running jobs only on the writing copy | `jobs` (`jobs.md`) reads `ReadOnly()`. |
| The switchover between the on-site and off-site copies (Q-OFFLINE); promoting or demoting a PostgreSQL server | a later `switchover` spec and the operator. `db` only reports what it finds. |
| Backups, replication set-up, the `bms` database role and its privileges, TLS to the server | the operator docs (`docs/operating.md`, Proposed change 11). |
| The tracer and meter themselves, the privacy policy on attributes, OTLP export | `telemetry` (`telemetry.md`). This spec only *uses* them: a span per statement (the pgx `QueryTracer` adapter) and the pool gauges from `Pool.Stats`, both inside `db` because only `db` may import pgx (depguard `storage-seam`). Statement text and arguments never reach a span. |
| Where `BMS_TEST_DB_DSN` comes from (a container or the compose stack) | `lib/testkit` when it is specified; until then the running `deploy/compose` stack. |
| Cross-process messaging over `LISTEN`/`NOTIFY` | nobody (D24: signals are in-process). |

## Sub-abstractions

| Name | Purpose (one line) | Notes |
|---|---|---|
| `Options`, `Open`, `Pool` | Open one pool from validated options with a bounded, retried connect, and close it. | `Pool` implements `DB`. `Close` stops the read-only watcher. |
| `Querier`, `Transact`, `AfterCommit` | Run each statement with a deadline on the pool, or on the transaction the context carries. | One code path for a store method, in or out of a transaction. |
| `Wrap`, `*Error`, `ErrReadOnly` | Turn any error that comes back from the driver into an `errs` kind without echoing values. | The only exported mapping function; `*Error` is the scrubbed leaf. |
| Read-only state | Know whether this copy may write, cheaply, and keep it current. | Probed at `Open`, on `Ping`, on a schedule and on every `25006`. |
| `Set`, `Version`, `Migrator` | Apply each set's numbered files in order, record them, and check versions for `module.Build`. | `Migrator` satisfies `module.SchemaChecker` structurally. |
| `dbtest` | A `DB` that needs no database, and a throw-away real database for integration tests. | `Fake` for modules' unit tests and `moduletest`; `TempDB` behind the `integration` tag. |

## Interface

```go
// Package db is the one place the process reaches PostgreSQL: the pool, statements with
// deadlines, transactions, driver-error classification, read-only detection and the
// migration runner (D8, D17). It imports pgx; nothing above the storage seam does.
package db

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/secret"
)

// ---- Options and Pool ----

// Options is what Open needs. app copies config.DB into it (config.md decision 12), so db
// is built in tests with no environment at all.
type Options struct {
	// URL is the PostgreSQL connection URL (postgres:// or postgresql:// with a host). It
	// is read once, with Value(), inside Open, and never appears in an error or a log.
	URL secret.Secret
	// MaxConns is the pool's upper bound. 1..1000.
	MaxConns int
	// ConnectTimeout bounds the initial connect in Open, retries included. 1s..10m.
	ConnectTimeout time.Duration
	// QueryTimeout is the longest one statement may run when the caller's context allows
	// more (Behavior: "Deadlines"). 100ms..10m.
	QueryTimeout time.Duration
	// Tracer, when set, gives every statement a client span (Behavior: "Open", step 2).
	// nil means no statement spans. app passes tel.Tracer("db") (telemetry.md).
	Tracer telemetry.Tracer
	// Meter, when set, registers the pool gauges (Behavior: "Open", step 5). nil means no
	// gauges. app passes tel.Meter("db").
	Meter telemetry.Meter
}

// Validate fails with errs.Invalid joining one validate.FieldError per problem, fields
// "db.url", "db.max_conns", "db.connect_timeout", "db.query_timeout": URL unset or not an
// absolute postgres/postgresql URL with a host (never echoed), a number or duration out
// of range (the same bounds as config.md's catalogue). Tracer and Meter are not checked:
// nil is a valid value for each (tests and `bms migrate` run without them).
func (o Options) Validate() error

// MinServerVersion is the oldest PostgreSQL server Open accepts, as server_version_num.
// PostgreSQL 16 (decision 20).
const MinServerVersion = 160000

// Open validates opts, parses the URL, connects with retries until ConnectTimeout, checks
// the server version and probes the read-only state, then starts the read-only watcher.
// It fails with errs.Invalid for bad options, a URL the driver rejects, bad credentials, a
// missing database or an old server (none retried); errs.Unavailable when the server could
// not be reached within ConnectTimeout; errs.Canceled or errs.Timeout when ctx ends first.
// The caller owns the Pool and must Close it.
func Open(ctx context.Context, opts Options, clk clock.Clock, logger *slog.Logger) (*Pool, error)

// Pool is the process's connection pool. It implements DB. Safe for concurrent use from
// Open until Close returns.
type Pool struct{ /* unexported: pgxpool, opts, clock, logger, tracer, readOnly atomic, watcher */ }

// Stats is a snapshot of the pool, copied from pgxpool.Stat so no caller sees pgx types.
type Stats struct {
	Acquired     int           // connections in use
	Idle         int           // open connections not in use
	Total        int           // Acquired + Idle + connections being opened
	Max          int           // Options.MaxConns
	Pending      int           // callers waiting to acquire
	WaitDuration time.Duration // total time callers have spent waiting since Open
}

// Stats returns the pool's current counters. Cannot fail: it reads pgxpool's own counters
// and after Close returns zeros. Safe for concurrent use.
// Consumers: the gauges this package registers when Options.Meter is set (telemetry.md
// metrics table: db.client.connection.count{state}, .max, .pending_requests); app's
// `bms check` may print it.
func (p *Pool) Stats() Stats

// Querier runs one statement. DB and the transaction a context carries both implement it.
// Each call derives a deadline (Behavior: "Deadlines") and returns errors already
// classified by Wrap: Rows.Err, Rows.Scan and Row.Scan likewise. Values returned are pgx
// types, so pgx.CollectRows and friends work; the postgres-sql skill says how to use them.
type Querier interface {
	// Exec runs a statement that returns no rows. Fails with the kind Wrap gives the
	// driver's error (F10–F20).
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	// Query runs a statement and returns its rows; the caller must Close them (rowserrcheck,
	// sqlclosecheck). Fails as Exec does; Rows.Err and Rows.Scan fail the same way.
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	// QueryRow runs a statement expected to return one row. Errors surface from Row.Scan,
	// classified: errs.NotFound when there is no row (F18), otherwise as Exec.
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// DB is what a postgres/ package needs from the database and what Deps.DB carries
// (module-contract). *Pool and dbtest.Fake implement it.
type DB interface {
	Querier

	// Transact runs fn inside one transaction and commits when fn returns nil. The ctx
	// passed to fn carries the transaction: every Querier call on this DB with that ctx
	// (or a child of it) runs on the transaction, so a store method has one code path.
	// A nested Transact with a ctx that already carries a transaction joins it (no
	// savepoint; decision 6). Mode Write fails at once with errs.Unavailable wrapping
	// ErrReadOnly when ReadOnly() is true (F22). fn's error is returned unchanged after a
	// rollback (F23); a panic in fn is rolled back and re-raised (F24). After a successful
	// commit the AfterCommit hooks run in registration order, then Transact returns nil.
	// Fails with errs.Invalid for an invalid mode or nil fn (F21); with Wrap's kind when
	// begin or commit fails (F25); with errs.Canceled or errs.Timeout when ctx ends (F26).
	// fn must not use its ctx from another goroutine: a transaction is one connection.
	Transact(ctx context.Context, mode Mode, fn func(ctx context.Context) error) error

	// ReadOnly reports whether this copy may not write: the server is in recovery (a
	// replica) or its default transaction mode is read-only (decision 7). It is a cached
	// value refreshed by the watcher, by Ping and by every read-only write error, so it
	// cannot fail and costs no round trip.
	ReadOnly() bool
}

// Mode says what a transaction will do, so a read can run on a replica and a write can
// fail before touching the server. The zero value is invalid.
type Mode uint8

const (
	Read  Mode = iota + 1 // BEGIN READ ONLY; allowed on a read-only copy
	Write                 // plain BEGIN, so the session's default_transaction_read_only applies and the server answers 25006; refused on a read-only copy
)

// String returns "read", "write" or "Mode(<n>)". Cannot fail.
func (m Mode) String() string

// AfterCommit registers fn to run once the transaction carried by ctx has committed
// (Transact runs the hooks after Commit returned nil, in order, each with a context
// derived from Transact's own ctx, not the transaction's). When ctx carries no
// transaction, fn runs immediately, because an autocommit write is already durable.
// Hooks are for work that must not run if the transaction rolls back: signals delivers
// published signals from one. A hook cannot fail the transaction (it is already
// committed); it handles its own errors. db passes the caller's ctx unchanged; signals
// detaches the hook's ctx from the request's cancellation itself (signals.md decision 6).
// Fails with errs.Invalid if fn is nil (F27).
func AfterCommit(ctx context.Context, fn func(ctx context.Context)) error

// Ping checks that the server answers and refreshes the read-only state in the same round
// trip, for /readyz. Fails with errs.Unavailable when the server does not answer (F28),
// errs.Canceled or errs.Timeout when ctx ends (F29).
func (p *Pool) Ping(ctx context.Context) error

// Close stops the read-only watcher, waits for it, and closes the pool, which waits for
// acquired connections to be released. Idempotent: a second Close returns nil. Fails with
// errs.Timeout (or errs.Canceled) if ctx ends before the watcher stopped or the pool
// closed (F30); it returns then, never later, even if a late caller still holds a
// connection (a handler that outlived api.Shutdown's deadline, api.md "Concurrency and
// lifecycle"): the pool keeps closing in the background, the held connection is closed
// when released, and every call on the Pool after Close returned fails with
// errs.Unavailable (F20), which is what the late caller sees. app calls it once at
// shutdown, after Set.Stop and the HTTP server.
func (p *Pool) Close(ctx context.Context) error

// ---- Errors ----

// ErrReadOnly is wrapped by every error for a write refused because this copy is
// read-only. Its kind is errs.Unavailable; callers that must tell the two apart check
// errors.Is(err, db.ErrReadOnly) before the kind (the api does, to answer "this copy is
// read-only"; a retry loop around a write must, because retry.Retryable would retry it).
var ErrReadOnly = errors.New("this copy of the database is read-only")

// ErrNoDatabase is wrapped by every error from the DB that None returns. Its kind is
// errs.Unavailable. A module factory that reads the database at construction fails the
// build step of `bms openapi` and `bms check` with it (app.md decision 7).
var ErrNoDatabase = errors.New("no database behind this DB")

// None returns a DB with no database behind it, for `bms openapi` and `bms check`, which
// build every module without connecting (app.md decision 7). Every Exec, Query,
// Row.Scan, Rows.Err and Transact fails with errs.Unavailable wrapping ErrNoDatabase;
// ReadOnly() is false; AfterCommit outside a transaction still runs its hook at once
// (nothing was written). It lives in db because only db and postgres/ packages may import
// pgx (depguard storage-seam) and DB's methods return pgx types. Cannot fail.
func None() DB

// Error is a PostgreSQL server error as db reports it: the SQLSTATE and the names the
// server attached, never the message's detail, hint or context, which can carry row and
// parameter values (decision 4). It is the leaf of the chain: the driver's error is not
// kept. Find it with errors.As to act on a constraint name.
type Error struct {
	Code       string // SQLSTATE, e.g. "23505"
	Schema     string // schema name, if the server gave one
	Table      string // table name, if given
	Column     string // column name, if given
	Constraint string // constraint name, if given
}

// Error returns "pg <Code> <condition name> [on <Schema>.<Table>[.<Column>]] [constraint
// <Constraint>]", e.g. "pg 23505 unique_violation on volunteers.shift constraint
// shift_slot_key". Cannot fail; never contains a value from the statement or a row.
func (e *Error) Error() string

// Wrap classifies err and wraps it with op: errs.E(op, kind, scrubbed), where the kind
// follows the table in Behavior "Error mapping" and a server error is replaced by an
// *Error. It returns nil for a nil err. An err that already carries an *errs.Error keeps
// its kind (the storage package only adds its op). Every error that leaves a store method
// goes through Wrap, including those from pgx helpers that never passed through Querier
// (pgx.CollectOneRow returning pgx.ErrNoRows). Cannot fail.
func Wrap(op string, err error) error

// ---- Migrations ----

// Set is one sequence of migrations: the core package's or the module's
// postgres/migrations directory. Module sets are built by app from Manifest.Name and
// Manifest.Migrations; core sets are exported by each core package with tables.
type Set struct {
	// Name identifies the sequence in core.schema_migration: a module name, or a core
	// package's name (people, identity, audit, ...). "[a-z][a-z0-9]{1,31}".
	Name string
	// Schema is the PostgreSQL schema the set owns, created with CREATE SCHEMA IF NOT
	// EXISTS before its first file runs: "core" for a core set, Name for a module (D10).
	// Nothing else is accepted.
	Schema string
	// Files holds NNNN_name.sql at its root (Behavior: "Migration files").
	Files fs.FS
}

// Validate fails with errs.Invalid joining a validate.FieldError per problem: Name or
// Schema fails its grammar, Schema is neither "core" nor Name, Files is nil (F32). It does
// not read Files; Version does.
func (s Set) Validate() error

// Version parses files and returns the set's version: the highest file number. It fails
// with errs.Invalid when a name is not NNNN_name.sql, numbers repeat, have a gap or do
// not start at 0001, a file cannot be read, or is empty (F33–F35). Consumers: Migrator,
// `bms check`, tests.
func Version(files fs.FS) (int, error)

// Migrator applies and checks migration sets on one Pool. Safe for concurrent use; Migrate
// serialises itself across processes with an advisory lock.
type Migrator struct{ /* unexported: pool, logger, own bootstrap set */ }

// NewMigrator fails with errs.Invalid if pool or logger is nil (F51).
func NewMigrator(pool *Pool, logger *slog.Logger) (*Migrator, error)

// Migrate applies, in order, every pending file of its own bootstrap set and then of each
// set in sets, one transaction per file with the core.schema_migration row written in the
// same transaction (decision 10). It first validates everything (F32–F35, F41), refuses a
// read-only database (F36) and a concurrent run (F37), and compares every applied row with
// its file (F38, F39) so a problem anywhere stops the run before anything is applied.
// Each applied file is logged at info with set, version, name and duration, and returned.
// Nothing pending returns an empty slice and nil. A failing file is rolled back and
// reported with its set, version and name; earlier files stay applied (F40). ctx bounds
// the whole run and every statement in it; no QueryTimeout applies (F42).
// Consumer: app's `bms migrate`, which passes the core sets then Plan.Order() (D17).
func (m *Migrator) Migrate(ctx context.Context, sets []Set) ([]Applied, error)

// Applied is one migration file Migrate ran.
type Applied struct {
	Set      string
	Version  int
	Name     string
	Duration time.Duration
}

// Status reports, for the bootstrap set and each set in sets, the version the binary was
// built with, the version the database holds (0 when none, including when the table does
// not exist yet) and the problem CheckSchema would report, if any. It never changes the
// database. Fails with errs.Invalid for an invalid set (F41) and errs.Unavailable,
// errs.Timeout or errs.Canceled when the database cannot be read (F44). Consumers: app's
// `bms check`; api's GET /api/admin/modules (versions per module, module-contract Node).
func (m *Migrator) Status(ctx context.Context, sets []Set) ([]Status, error)

// Status is one set's state.
type Status struct {
	Set     string
	Built   int   // Version(files)
	Applied int   // highest version recorded; 0 when none
	Err     error // nil when the set is current; else what CheckSchema returns
}

// CheckSchema implements module.SchemaChecker (structurally; app asserts it). It fails
// with errs.Conflict when no version is recorded for module (including on an empty
// database), when the recorded version differs from Version(migrations) in either
// direction, or when an applied file's checksum or name differs from the file in
// migrations; the message names the module and both versions and says to run
// `bms migrate` (recorded lower) or to upgrade the binary (recorded higher) (F45–F47).
// It fails with errs.Invalid when module or migrations is malformed (F48, F50) and with
// errs.Unavailable, errs.Timeout or errs.Canceled when the database cannot be read
// (F49). It never writes.
func (m *Migrator) CheckSchema(ctx context.Context, module string, migrations fs.FS) error
```

```go
// Package dbtest gives tests a DB. Fake needs no database and is what moduletest and a
// module's unit tests hand to a Factory; TempDB is a real, throw-away database for
// integration tests of postgres/ packages and of db itself.
package dbtest

// Fake implements db.DB with no database behind it: Exec records the call and returns an
// empty CommandTag; Query returns rows with no row; QueryRow's Scan fails with
// errs.NotFound; Transact runs fn (Write fails first when read-only) and then the
// AfterCommit hooks; ReadOnly reports what SetReadOnly set. Safe for concurrent use.
type Fake struct{ /* unexported */ }

// NewFake returns a writable Fake. Cannot fail.
func NewFake() *Fake

// SetReadOnly makes every Exec and every Write Transact fail with errs.Unavailable
// wrapping db.ErrReadOnly (F52), as a Pool does on a replica. Cannot fail.
func (f *Fake) SetReadOnly(ro bool)

// Calls returns every statement run so far, in order, as a copy. Cannot fail.
func (f *Fake) Calls() []Call

// Call is one recorded statement.
type Call struct {
	SQL  string
	Args []any
	InTx bool
}

// TempDB creates a database with a random name through BMS_TEST_DB_DSN, opens a db.Pool
// on it with default options and a fake-free real clock, and drops it in t.Cleanup. It
// calls t.Skip when BMS_TEST_DB_DSN is unset and t.Fatal when the database cannot be
// created. Integration build tag only. Cannot return an error: failures stop the test.
func TempDB(t testing.TB) *db.Pool
```

### Who needs each member

| Member | Consumer(s) | Pre-condition | Post-condition |
|---|---|---|---|
| `Options`, `Validate` | `app` (from `config.DB`); `Open`; tests building a `Pool` without an environment | — | the catalogue's bounds, by dotted key |
| `MinServerVersion` | `Open`; the operator docs test | — | one number |
| `Open` | `app` (every command that touches the database: `serve`, `migrate`, `check`, `create-superuser`, `export`, `delete`) | valid options; a clock and a logger | a working pool with the read-only state known |
| `Pool` as `DB` | `app` (`Provision` sets `Deps.DB`); every core `postgres/` package | opened | one code path in or out of a transaction |
| `Querier` (`Exec`, `Query`, `QueryRow`) | every core and module `postgres/` store | — | deadline applied; errors classified |
| `Transact`, `Mode` | every store that writes more than one row (postgres-sql "Transactions"); `people` (export and deletion across modules, one transaction); the `hello` module | — | committed, or rolled back with the cause |
| `AfterCommit` | `signals` (deliver after commit, 09 §1); `jobs` (enqueue on commit) | inside or outside a transaction | runs iff committed |
| `ReadOnly` | `api` (`/readyz`, the banner), `jobs` (run only on the writer), `notify` (send only on the writer), `Transact` (fail fast) | — | cached, current within one watcher interval or one `Ping` |
| `Ping` | `api` (`/readyz`), `app` (`bms check`) | — | reachable; state refreshed |
| `Close` | `app` at shutdown | — | watcher stopped, pool closed |
| `Stats` | the pool gauges `db` registers on `Options.Meter` (telemetry.md); `app` (`bms check`) | opened | a plain snapshot, zeros after `Close` |
| `ErrReadOnly` | `api` (error mapper: "this copy is read-only"), `moduletest` (`delete_read_only`), `dbtest.Fake` | — | `errors.Is` works through any wrapping |
| `Error` | stores mapping a constraint to a field (`identity`: duplicate e-mail → field `email`) | `errors.As` | names only, never values |
| `Wrap` | every store method's return path; `db` itself | — | an `*errs.Error` with the mapped kind |
| `Set`, `Validate` | `app` (builds sets from manifests and core packages); `Migrate`, `Status` | — | one grammar for names and schemas |
| `Version` | `Migrator`; `bms check`; a module's tests ("my migrations parse") | files at the root | the highest number |
| `Migrator`, `NewMigrator` | `app` | an open pool | — |
| `Migrate`, `Applied` | `app`'s `migrate` command | not read-only | every pending file applied and recorded, or nothing new |
| `Status` | `app`'s `check`; `api`'s `GET /api/admin/modules` | — | one row per set, no write |
| `CheckSchema` | `module.Build` (through `app`, as `SchemaChecker`); `app`'s `check` | — | nil iff the set is current and unchanged |
| `dbtest.Fake`, `SetReadOnly`, `Calls` | `moduletest.Conformance` (`start_read_only`, `delete_read_only`, `export_unknown_person_is_empty`); modules' unit tests of `New` | — | no I/O |
| `dbtest.TempDB` | `db`'s own integration tests; every `postgres/` package's integration tests | `BMS_TEST_DB_DSN` set; the role may `CREATE DATABASE` | an isolated database, dropped after |

## Types

Defines `Options`, `Pool`, `Stats`, `Querier`, `DB`, `Mode`, `Error`, `Set`, `Migrator`,
`Applied`, `Status`, and in `dbtest` `Fake`, `Call`. Uses `secret.Secret`, `clock.Clock`,
`retry.Policy`, `validate.Errors`, `validate.FieldError`, `slog.Logger`, `fs.FS`,
`telemetry.Tracer`, `telemetry.Meter` (and, inside the package, `telemetry.Span`,
`telemetry.Attr`, `telemetry.Sample`), and from pgx `pgx.Rows`, `pgx.Row`,
`pgconn.CommandTag` (and, inside the package only, `pgx.QueryTracer`, `pgxpool.Stat`).
Uses nothing from `model`: IDs and
addresses pass through pgx by underlying type (model.md "At the storage seam"), which the
integration test proves.

## Data

One table, owned by this spec and created by its bootstrap set (`db`, version `0001`,
schema `core`), which `Migrate` always applies first:

```sql
-- postgres/migrations/0001_schema_migration.sql (set "db")
CREATE SCHEMA IF NOT EXISTS core;

CREATE TABLE IF NOT EXISTS core.schema_migration (
    set_name    text        NOT NULL,
    version     integer     NOT NULL CHECK (version > 0),
    name        text        NOT NULL,          -- the file name without number and suffix
    checksum    text        NOT NULL,          -- SHA-256 of the file's bytes, lower-case hex
    duration_ms integer     NOT NULL CHECK (duration_ms >= 0),
    applied_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (set_name, version)
);
```

A set's version is `max(version)` for its `set_name`; D17's "one row per module with the
version applied" is this table read that way, with the history D17 also asks for ("records
what ran") in the same rows. Rows are never updated or deleted, so there is no `updated_at`
(an exception to the `postgres-sql` rule, which this spec may make); `applied_at` is the
row's `created_at`. The `db` set itself is recorded like any other, so the bootstrap file
is applied once.

No column holds personal data. Retention: forever; the rows are the installation's
history and are needed by every later `bms migrate` (D30 does not apply). The table is
read by `CheckSchema` at every start and by `GET /api/admin/modules` behind `core.modules.read`.

Files: none. `db` reads nothing from disk (migrations are embedded `fs.FS` values) and
writes nothing.

Logs: `Open` logs one line at info with host, port, database, user, server version and the
read-only state; never the password or the URL. The watcher logs at warn each time the
read-only state changes and each time a probe fails (once per transition, not per tick).
`Migrate` logs one line per applied file. Statement text and arguments are never logged
(go-conventions): a store that wants to log a failure logs the `*errs.Error`, whose text
holds names, not values (decision 4).

## Permissions

None. `db` performs no operation on behalf of a person; every store names the permission
for its own operation and checks it in the service before calling `db` (09 ground rules).
The commands that use `Migrator` run on the host as the operator. The PostgreSQL role in
`db.url` is the installation's one role; what it may do is the operator's grant (the
`audit` spec makes its table append-only: `UPDATE` and `TRUNCATE` are revoked in audit's
migration and a trigger refuses `UPDATE`, `DELETE` and `TRUNCATE` outside `core.audit_purge`, audit decision 9). `GET /api/admin/modules`, which shows `Status`, is behind `core.modules.read`
(module-contract "Permissions").

## Signals

None published, none subscribed. `AfterCommit` is the hook `signals` uses to deliver
after commit (Proposed change 3); `db` does not know what a signal is.

## Settings

None. Nothing here differs between regionals: the pool, deadlines and URL are process
configuration (`config.md`: `db.url`, `db.max_conns`, `db.connect_timeout`,
`db.query_timeout`, each with a default and a range), because they are needed to reach the
database at all. Constants inside this package, pinned by tests, with the reason each is
not a key:

| Constant | Value | Why not configurable |
|---|---|---|
| connect retry policy | `retry.Policy{Initial: 500ms, Max: 5s, Multiplier: 2, MaxAttempts: 0}` under `ConnectTimeout` | `ConnectTimeout` is the knob an operator understands; the schedule inside it changes nothing an operator could want (config.md Proposed change 4). |
| read-only probe interval | 15 s | fast enough for a banner after a switchover, cheap enough (one `SELECT` per 15 s); `Ping` refreshes on demand. If the switchover design needs faster, it is a key then. |
| probe statement timeout | 5 s | a probe that hangs must not block the watcher; shorter than any sane `QueryTimeout` floor. |
| advisory lock key | a fixed `int64` derived from the string `bms.migrate` | one installation, one database, one lock. |
| `application_name` | `bms` | shows in `pg_stat_activity`; not something to vary. |

## UI

None of its own. What this spec provides for screens elsewhere: `ReadOnly()` and `Ping`
for `/readyz` ("database reachable, writable or read-only", 09 §1) and for the web shell's
"this copy is read-only" banner (`api.md`, `web-shell.md`); `ErrReadOnly` so a write on a
read-only copy becomes a clear "this copy is read-only" response rather than an error page
(09 ground rules); `Status` for the admin "modules" screen's built-for and applied versions
(module-contract "UI").

## Behavior

### Decisions settled here, with reasons

1. **`db` speaks pgx; the storage seam is the service's interface, not `db`.** D8 puts the
   replaceable boundary at the storage interface each service declares, with `postgres/`
   packages behind it, and lets only those packages and `db` import the driver. A `db`
   that re-wrapped every pgx type would be a second seam with no consumer, and would cut
   stores off from `pgx.CollectRows`, named-type scanning and the rest. So `Querier`
   returns `pgx.Rows`, `pgx.Row` and `pgconn.CommandTag`, and `db` adds what pgx lacks:
   deadlines, classification, the transaction in the context, read-only state and
   migrations. Replacing PostgreSQL later means new `postgres/` packages and a new `db`,
   and no change to a service (D8).
2. **`Deps.DB` is an interface (`DB`), implemented by `*Pool` and `dbtest.Fake`.**
   module-contract's conformance rules (`start_read_only`, `delete_read_only`,
   `export_unknown_person_is_empty`) need a `DB` that works with no database and refuses
   writes on demand. The interface is three things a store needs (statements, a
   transaction, the read-only state); `Ping` and `Close` stay on `*Pool` because only `app`
   and `api` need them.
3. **Every error leaving `db` is already classified; `Wrap` is the one mapping function,
   and stores call it on every return path.** go-conventions puts the translation at the
   storage boundary, in `db` and in each `postgres/` package. `Querier`, `Rows`, `Row` and
   `Transact` return classified errors, so a store that only uses them needs `Wrap` for
   the op name alone; a store that uses a pgx helper which returns `pgx.ErrNoRows` itself
   (`pgx.CollectOneRow`) gets it mapped by the same `Wrap`. One function, one table of
   kinds, idempotent, so there is no second mapping to drift.
4. **Server errors are scrubbed to names: SQLSTATE, schema, table, column, constraint;
   never message, detail, hint or context.** PostgreSQL's detail says `Key
   (email)=(someone@example.org) already exists`, its message for `22P02` echoes the
   input, and its context can hold PL/pgSQL variables. Those strings are personal data in
   an error that will be logged and may reach an API body (privacy-data: never in logs or
   error messages). `*Error` keeps what a maintainer needs to find the statement (the code
   and the names) and drops the rest; the driver's error is not kept in the chain, so no
   later `%w` can print it. Lost debugging detail is recovered with `EXPLAIN` on invented
   data, not with production values.
5. **The transaction travels in the context.** A use case that writes several rows calls
   `Transact` and then ordinary store methods with the ctx it was given; each store method
   calls `deps.DB.Exec(ctx, ...)` and runs on the transaction without knowing it. The
   alternative, a `Tx` value threaded through every store method, doubles each store's
   interface (one method with a `Tx`, one without) or forces every call into a
   transaction. The cost is the rule that fn's ctx must not cross goroutines, stated in the
   doc comment and checked by the race detector in the integration test.
6. **A nested `Transact` joins the enclosing transaction; no savepoints.** A service that
   calls another service's store inside its transaction wants one atomic unit, not a
   partial rollback. Savepoints would need a design for which errors release and which
   roll back to them; no consumer needs that. An inner fn's error propagates and the outer
   fn returns it, so the whole transaction rolls back (after an error PostgreSQL aborts
   the transaction anyway).
7. **Read-only means "in recovery or default-read-only", probed by `db`, with no
   configuration flag.** config.md decision 6 rejected a flag. `pg_is_in_recovery()` is
   true on a streaming replica (the on-site mirror during an event, D27).
   `transaction_read_only`, which a session inherits from `default_transaction_read_only`,
   is true when an operator ran `ALTER DATABASE bms SET default_transaction_read_only =
   on`: a one-line lever to freeze writes on one copy before a switchover, usable today
   without any switchover code, and the way the integration test puts a plain server into
   read-only mode. One `SELECT pg_is_in_recovery() OR
   current_setting('transaction_read_only')::bool` answers both. **Confirmed (owner,
   2026-10-16):** `default_transaction_read_only = on` counts as "this copy is read-only";
   the `switchover` spec (Q-OFFLINE) builds on or replaces the lever. The alternative,
   probing `pg_is_in_recovery()` alone, would make read-only mode untestable without a
   replica and leave operators with no lever.
8. **The read-only state is cached and refreshed three ways: a watcher every 15 s, every
   `Ping`, and every `25006` write error.** `Transact(Write)` and `jobs` ask on every call,
   so the answer must be free; `/readyz` wants it fresh, so `Ping` refreshes it; a
   switchover is noticed within one interval or at the first refused write, whichever
   comes first. A probe that fails keeps the last known state and logs once per
   transition.
9. **A write refused on a read-only copy is `errs.Unavailable` wrapping `ErrReadOnly`.**
   Adopted from module-contract Proposed change 4. 503 is the HTTP status that says "not
   now, the service is otherwise fine", and the api maps the sentinel itself to the
   "this copy is read-only" message (09 ground rules). The alternative, `Unsupported`
   (501), would tell an operator a feature is turned off, which is false. One consequence
   is stated rather than hidden: `retry.Retryable` treats `Unavailable` as retryable, so a
   retry loop around a write (none exists in the MVP; jobs run only on the writer) must
   check `errors.Is(err, db.ErrReadOnly)` first. `db` itself never retries a statement.
10. **One transaction per migration file, with its `core.schema_migration` row in the same
    transaction.** The version recorded can then never disagree with what was applied: a
    crash leaves the database at the last complete file and the next run continues from
    there; a failing file is rolled back and named. 09 §1 said "one transaction per
    module"; per file is what every migration tool does and what makes a later
    non-transactional migration possible, and a module half-applied is no worse than a
    module not applied: `CheckSchema` refuses to start either way until the operator
    acts. The observable promise is unchanged: the recorded version always matches what
    was applied, and `serve` refuses to start until `migrate` finishes. **Confirmed
    (owner, 2026-10-16):** per file, not per module; 09 §1's wording changes accordingly
    (Proposed change 6).
11. **No non-transactional migrations yet.** `CREATE INDEX CONCURRENTLY` is for tables
    too large to lock, and a regional's tables hold thousands of rows. Supporting a
    `-- bms:no-transaction` marker means a second execution path, an idempotency rule and
    a crash story. It is designed when a consumer appears (Proposed change 7 amends the
    skill).
12. **Checksums are recorded and checked, at `Migrate` and at `CheckSchema`.** A released
    migration must never be edited (`postgres-sql`); without a checksum that rule has no
    teeth, and an edited file applied on one copy and not another is exactly the drift
    D17 exists to prevent. A changed file stops `migrate` before anything is applied and
    stops `serve` with a message naming the file. A developer who edits a local migration
    resets the development database, which is the right habit.
13. **The migrator creates each set's schema and nothing else about it.** D10 says the
    schema name is the module name; having the migrator run `CREATE SCHEMA IF NOT EXISTS
    "<schema>"` guarantees it, lets `0001_init.sql` start with tables, and removes one
    line every module would otherwise copy. Migrations write schema-qualified names
    (`volunteers.shift`); no `search_path` is set, so nothing depends on session state.
14. **pgx's `PG*` variables and `~/.pgpass` are honoured for what the URL leaves unset,
    and documented.** pgx reads them inside `ParseConfig` and offers no switch; pretending
    otherwise would be a lie. They are also useful: `PGSSLROOTCERT` and `PGSSLMODE` keep
    certificates out of the URL. Because config.md requires a host in the URL, `PGHOST`
    can never redirect a connection; only parameters the URL omits are filled in. The
    operator docs say so, and `bms check` prints the names of any `PG*` variables that
    are set (never values; Proposed change 5), so an inherited variable is visible.
15. **`Open` retries only what retrying can fix.** Connection refused, DNS failure, the
    server starting up (`57P03`) and timeouts are `Unavailable` or `Timeout` and retried
    under `ConnectTimeout` with the policy above (`retry.Do`, so the fake clock drives the
    test). Bad credentials (`28xxx`), a missing database (`3D000`), a rejected URL and an
    old server are `Invalid` and returned at once: no amount of waiting fixes a typo, and
    an operator should see it in the first second.
16. **Every statement has a deadline, derived by `db`, not trusted to the caller.** The
    principle says every I/O call has a deadline; a store author who forgets one would
    otherwise hold a connection forever. Each `Querier` call uses the earlier of the
    caller's deadline and now + `QueryTimeout`; `Query` keeps its cancel until `Rows.Close`.
    The request-level deadline (`http.request_timeout`) still bounds the whole use case.
    `Migrate` is the one exception: a backfill may legitimately run longer, and the
    command's ctx (app.md, proposed 30 min) bounds it.
17. **`Migrate` serialises itself with a session advisory lock and fails fast when it is
    held.** Two operators (or a container restart loop) running `bms migrate` at once must
    not interleave files. `pg_try_advisory_lock` on a dedicated connection, held for the
    run, gives a clear `Conflict` ("another bms migrate is running") instead of a wait of
    unknown length.
18. **`Status` and `CheckSchema` treat a missing `core.schema_migration` as "nothing
    applied".** On an empty database the first thing an operator sees must be "run `bms
    migrate`", not `42P01 undefined_table`. Both query `to_regclass('core.schema_migration')`
    first.
19. **Integration tests get a throw-away database, not a schema.** `CREATE DATABASE` with a
    random suffix isolates `core.schema_migration`, advisory locks and
    `default_transaction_read_only` per test, which a schema cannot; it costs about a
    hundred milliseconds and needs `CREATEDB`, which the compose role has. `dbtest` lives
    under `core/db` because `lib/testkit` may not import `core/db` (layer rule 1).
20. **The minimum PostgreSQL version is 16: `MinServerVersion = 160000` (owner,
    2026-10-16).** 16 is the oldest release still in community support through the first
    events this software will run at (support ends November 2028); the development stack
    runs 17; nothing here needs a 17-only feature, so a regional on a distribution package
    of 16 is fine. Accepting 14 or 15 would cost nothing in code but would commit the
    project to testing against them. An older server is refused at `Open` (F8) with both
    versions named.

### Migration files

A set's `Files` holds, at its root, files named `NNNN_name.sql`: four decimal digits,
an underscore, a name of `[a-z0-9_]+`, the suffix `.sql`; nothing else is at the root (a
`README` or a subdirectory is `Invalid`, so a misplaced file cannot be silently skipped).
Numbers start at `0001`, are unique and contiguous; the set's **version is the highest
number** (module-contract Proposed change 4). A file is applied as one `Exec` with no
arguments (pgx then uses the simple protocol, so a file may hold several statements),
inside one transaction, followed in the same transaction by the row insert. The checksum
is SHA-256 over the file's bytes exactly as embedded (no newline normalisation; a CRLF
edit is a change). Files are never edited after release; a change is a new number.

### Error mapping (`Wrap`)

| Input | Kind | Notes |
|---|---|---|
| `nil` | — | returns nil |
| an error already carrying an `*errs.Error` | its kind | op is prepended; nothing else changes |
| `pgx.ErrNoRows` | `NotFound` | also from `Row.Scan` |
| `pgx.ErrTooManyRows`, `pgx.ErrTxClosed`, `pgx.ErrTxCommitRollback` | `Internal` | a bug in the store |
| `context.Canceled` (anywhere in the chain) | `Canceled` | the caller gave up |
| `context.DeadlineExceeded` | `Timeout` | |
| `*pgconn.PgError` `23505` unique_violation | `Conflict` | `*Error.Constraint` set |
| `23503` foreign_key_violation | `Invalid` | the store re-kinds to `Conflict` for "in use" |
| `23502`, `23514`, `22xxx` | `Invalid` | not-null, check, data exceptions |
| `25006` read_only_sql_transaction | `Unavailable`, wraps `ErrReadOnly` | sets `ReadOnly()` true |
| `40001`, `40P01`, `55P03` | `Unavailable` | serialization, deadlock, lock not available; retryable by a caller that chooses to |
| `08xxx`, `53xxx`, `57P01`, `57P02`, `57P03` | `Unavailable` | connection, resources, shutdown, cannot connect now |
| `57014` query_canceled | `Timeout` | unless ctx was canceled: then `Canceled` |
| `28xxx`, `3D000` | `Invalid` | credentials, database name (at `Open`; at runtime it is still the operator's to fix) |
| `42xxx` | `Internal` | SQL that does not match the schema: a bug or a missing migration (`CheckSchema` should have caught it) |
| other `*pgconn.PgError` | `Internal` | |
| `*pgconn.ConnectError`, `net.Error`, `io.EOF`/`io.ErrUnexpectedEOF` from a dropped connection, `pgconn.SafeToRetry(err)` true, a closed-pool error | `Unavailable` | |
| anything else | `Internal` | |

A `*pgconn.PgError` is replaced by an `*Error` built from `Code`, `SchemaName`, `TableName`,
`ColumnName`, `ConstraintName`; `Message`, `Detail`, `Hint`, `Where`, `InternalQuery` are
dropped. Connect errors are wrapped as they are: pgx prints host, user and database, never
the password; a test pins that the password marker never appears.

### Deadlines

For each `Exec`, `Query`, `QueryRow`: `d := min(ctx.Deadline(), now + QueryTimeout)`;
`ctx, cancel := context.WithDeadline(ctx, d)`. `Exec` and `QueryRow` cancel on return
(`Row.Scan` runs the statement, so `QueryRow` returns a row that cancels after `Scan`);
`Query` returns rows whose `Close` cancels, and `Rows.Err` reports `Timeout` or `Canceled`
when the deadline cut the result set short. `Transact` applies the same to `BEGIN`,
`COMMIT` and `ROLLBACK`; the statements inside apply it themselves. Acquiring a connection
from a full pool waits under the same deadline (`Timeout` when it passes: the pool is the
dependency that was unavailable, and the message says "waiting for a connection").

### `Open`, step by step

1. `opts.Validate()` (F1–F3); `clk` and `logger` non-nil (F4).
2. `pgxpool.ParseConfig(opts.URL.Value())`; a parse error becomes `Invalid` with the
   fixed reason "the driver rejected db.url (check scheme, host, port and parameters)",
   dropping the driver's text (F5). Set `MaxConns` and `application_name`. When
   `opts.Tracer` is set, set `ConnConfig.Tracer` to this package's `pgx.QueryTracer`
   adapter (unexported): `TraceQueryStart` opens a `telemetry.SpanClient` span named
   `db.<OPERATION>`, where `OPERATION` is the statement's first SQL keyword upper-cased
   (`SELECT`, `INSERT`, `UPDATE`, `DELETE`, `BEGIN`, `COMMIT`, ...; `OTHER` when the text
   does not start with a letter), with `Token("db.operation", OPERATION)` and
   `Token("db.system", "postgresql")` as start attributes, and stores the span in the ctx
   pgx hands back; `TraceQueryEnd` calls `RecordError` on a failed statement, adds
   `Token("db.response.status_code", <SQLSTATE>)` when the error is a `*pgconn.PgError`,
   and ends the span. The adapter never records statement text, arguments, row values or
   the connection URL (decision 4 applies to spans as to errors); a statement whose text
   cannot be classified still gets a span (`db.OTHER`), never the text. Pool-level hooks
   (`BeforeAcquire`, `AfterRelease`) are not used: `Stats` covers the pool.
3. Under `ctx` bounded by `ConnectTimeout`: `retry.Do` with the connect policy; each
   attempt derives a 5 s attempt timeout, acquires one connection and runs the probe
   statement (step 4). `Invalid` stops the loop at once (F7); `Unavailable` and `Timeout`
   retry; the deadline passing yields `Unavailable` wrapping the last error ("could not
   reach the database within db.connect_timeout", F6), unless the caller's own `ctx` ended,
   which yields `Canceled` or `Timeout` (F9).
4. The probe: `SELECT current_setting('server_version_num')::int, pg_is_in_recovery() OR
   current_setting('transaction_read_only')::bool`. Version below `MinServerVersion` →
   `Invalid` naming both versions (F8). Store the read-only state.
5. When `opts.Meter` is set, register the three gauges from telemetry.md's metrics table,
   each observed from `Stats()`: `db.client.connection.count` (unit `{connection}`; two
   samples per collection, `Token("state", "idle")` = `Idle` and `Token("state", "used")` =
   `Acquired`), `db.client.connection.max` (`{connection}`; `Max`) and
   `db.client.connection.pending_requests` (`{request}`; `Pending`). A registration error
   (telemetry F20–F22) fails `Open` with the meter's kind after closing the pgx pool
   (F56); the observers return nil after `Close` (telemetry's gauge rule). Log the start
   line; start the watcher goroutine (interval from the constant; `clk.NewTicker`); return
   the `Pool`.

### The watcher

One goroutine per `Pool`. Every interval it runs the probe with a 5 s timeout; on success
it stores the state and logs at warn if it changed ("this copy is now read-only" / "this
copy is writable again"); on failure it keeps the state and logs at warn once until the
next success. It exits when `Close` cancels its context, and `Close` waits for it. The
`clock.Clock` drives the ticker, so a unit test with `clock.Fake` and a fake prober covers
the transitions without a database.

### `Transact`, step by step

1. Validate `mode` and `fn` (F21). If ctx carries a transaction: run `fn(ctx)` and return
   its error (joined; decision 6). Hooks registered inside are the outer transaction's.
2. If `mode == Write && ReadOnly()`: return `errs.E(op, errs.Unavailable, ErrReadOnly)`
   (F22).
3. Acquire; `BEGIN READ ONLY` for `Read`, plain `BEGIN` for `Write` (so a session with
   `default_transaction_read_only` makes the server answer `25006`; deadline rule). Failure → `Wrap` (F25).
4. Build `txCtx` carrying the transaction and an empty hook list; `defer` a recover that
   rolls back and re-panics (F24).
5. `err := fn(txCtx)`. If non-nil: `ROLLBACK`; return `err` joined with any rollback error
   (`errors.Join(err, rollbackErr)`, so `errs.KindOf` is `err`'s kind, F23).
6. `COMMIT`. Failure → `ROLLBACK` is implicit; `Wrap` (F25); hooks do not run. A `25006`
   at commit is the read-only case (F17).
7. Run the hooks in order, each with `ctx` (the caller's, not `txCtx`); return nil.

### `Migrate`, step by step

1. Validate every set including the bootstrap set (F32, F41); `Version` each (F33–F35).
2. Acquire a dedicated connection for the run; `ReadOnly()` and a fresh probe: read-only →
   F36. `pg_try_advisory_lock(key)` → false → F37.
3. Bootstrap: run the `db` set's `0001` file idempotently if `to_regclass` finds no
   table (it is `IF NOT EXISTS` throughout), and record it if no row exists.
4. Load every `core.schema_migration` row. For each set, for each applied version: the
   file must exist with the same name and checksum (F38); no applied version may exceed
   `Version` (F39). Any problem anywhere → stop before applying anything, all problems
   joined.
5. For each set in order: `CREATE SCHEMA IF NOT EXISTS "<schema>"`; for each file above
   the applied version, in order: `BEGIN`; `Exec(file)`; `INSERT INTO
   core.schema_migration`; `COMMIT`; log and append to the result. A failure rolls back the
   file and returns `Wrap` wrapped with `"<set> <NNNN_name>"` (F40). ctx done → F42.
6. Release the lock and the connection (also on every error path).

### Concurrency and lifecycle

`*Pool` is safe for concurrent use between `Open` and `Close`. A transaction (the ctx
`Transact` hands to fn) is not: it is one connection, used from one goroutine. `Close` is
idempotent and waits for the watcher and for released connections, within ctx and never
past it (a handler that ignored its ctx past `api.Shutdown` may still hold a connection;
it sees `Unavailable` on its next call, F20). `Migrator`
holds no state beyond its pool and may be shared; `Migrate` runs are serialised by the
advisory lock across processes and by that same lock within one process. `dbtest.Fake` is
safe for concurrent use. Nothing in `db` runs a goroutine other than the watcher, and the
watcher's exit is `Close`.

### Imports

Standard library (`context`, `crypto/sha256`, `encoding/hex`, `errors`, `fmt`, `io`,
`io/fs`, `log/slog`, `net`, `regexp`, `sort`, `strconv`, `sync`, `sync/atomic`, `time`),
`github.com/jackc/pgx/v5` (`pgx`, `pgconn`, `pgxpool`), `internal/lib/errs`,
`internal/lib/clock`, `internal/lib/retry`, `internal/lib/validate`,
`internal/lib/secret`, `internal/core/telemetry` (`telemetry` is below `db` in the core
graph and imports no core package). Not `model`, not `core/module` (`module` imports `db`;
the `SchemaChecker` is satisfied structurally and asserted in `app`), not `config`, never
`go.opentelemetry.io` (depguard rule `otel-seam`). `dbtest` adds `testing`, `crypto/rand`,
`os`; its tests use `telemetrytest`. depguard's `storage-seam` rule already allows pgx
under `**/internal/core/db/**`.

### Accepted implementation deviations (owner, 2026-10-17)

- `Write` begins with plain `BEGIN`; `Read` with `BEGIN READ ONLY`. The session's
  `default_transaction_read_only` then applies to `Write` and the server answers `25006`.
- The probe also reads `pg_db_role_setting` (role and database defaults of
  `default_transaction_read_only`), and the pool is Reset (connections recycled) when the
  state changes, so new sessions pick the setting up.
- A `25006` inside a `Read` transaction owned by the pool is `Internal` (a bug in the
  caller's SQL, not a read-only copy) and does not flip the cached state.
- Using a closed pool is `Unavailable`.
- An internal `txctx` package carries the transaction in the ctx and is shared with `dbtest`.
- `Wrap` keeps only `%T` of an error it does not recognise, and for a pgx `ScanArgError`
  only the column index and name; never messages.
- A `Write` `Transact` nested in a `Read` one is `Invalid`; `AfterCommit` called after its
  transaction ended without committing is `Invalid`.
- The statement-span name parser skips leading SQL comments.
- `Migrate` returns the files applied so far together with the error.
- `Close` runs one extra goroutine (it waits for in-flight work, then closes the pool).

## Failure modes

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F1 | `Options.Validate` | `URL` unset | Invalid, field `db.url` | fix config (app prints it) | `TestOptionsValidate_URLUnset_ReturnsInvalid` |
| F2 | `Options.Validate` | `URL` not an absolute `postgres`/`postgresql` URL with a host | Invalid, never echoes the URL | fix config | `TestOptionsValidate_BadURL_ReturnsInvalidWithoutEcho` |
| F3 | `Options.Validate` | `MaxConns`, `ConnectTimeout` or `QueryTimeout` out of range; several at once all reported | Invalid, one `FieldError` each | fix config | `TestOptionsValidate_RangesTable` |
| F4 | `Open` | `clk` or `logger` nil | Invalid | fix app | `TestOpen_NilClockOrLogger_ReturnsInvalid` |
| F5 | `Open` | the driver rejects the URL | Invalid; fixed reason, driver text dropped | fix config | `TestOpen_DriverRejectsURL_ReturnsInvalidWithoutEcho` |
| F6 | `Open` | the server cannot be reached within `ConnectTimeout` (refused, DNS, starting up) | Unavailable wrapping the last attempt's error | start the database, fix host or port | `TestOpen_Unreachable_RetriesThenReturnsUnavailable` (fake clock drives the schedule; unit, with a closed port) |
| F7 | `Open` | bad credentials (`28xxx`) or missing database (`3D000`) | Invalid, not retried | fix config | `TestOpen_BadCredentials_ReturnsInvalidWithoutRetry` (integration) |
| F8 | `Open` | `server_version_num` below `MinServerVersion` | Invalid naming both | upgrade PostgreSQL | `TestOpen_OldServer_ReturnsInvalid` (unit, with a fake probe result) |
| F9 | `Open` | the caller's ctx ends first | Canceled / Timeout | stop | `TestOpen_ContextCanceled_ReturnsCanceled` |
| F10 | `Exec`, `Query`, `QueryRow`, `Rows.Err` | ctx canceled before or during | Canceled | stop | `TestQuerier_ContextCanceled_ReturnsCanceled` |
| F11 | same | the statement runs past `QueryTimeout` or the ctx deadline (`pg_sleep`) | Timeout | 504; shorten the query | `TestQuerier_ExceedsQueryTimeout_ReturnsTimeout` (integration) |
| F12 | `Exec`, `Row.Scan` | `23505` | Conflict; `*Error.Constraint` names it | 409 / field error | `TestWrap_UniqueViolation_ReturnsConflictWithConstraint` |
| F13 | same | `23503` | Invalid (store may re-kind) | 400 / 409 | `TestWrap_ForeignKeyViolation_ReturnsInvalid` |
| F14 | same | `23502`, `23514`, `22xxx` | Invalid | 400 | `TestWrap_DataAndCheckViolations_ReturnInvalid` |
| F15 | same | `40001`, `40P01`, `55P03` | Unavailable | 503; a caller that chose to may retry | `TestWrap_SerializationAndDeadlock_ReturnUnavailable` |
| F16 | same | connection lost, server shutting down, pool closed, `SafeToRetry` | Unavailable | 503 | `TestWrap_ConnectionErrors_ReturnUnavailable` |
| F17 | `Exec`, `Row.Scan`, `Transact` commit | `25006` on a read-only copy | Unavailable wrapping `ErrReadOnly`; `ReadOnly()` becomes true | "this copy is read-only" | `TestExec_OnReadOnlyDatabase_ReturnsErrReadOnlyAndFlipsState` (integration, via `default_transaction_read_only`) |
| F18 | `QueryRow` → `Row.Scan` | no row | NotFound | 404 | `TestQueryRow_NoRows_ReturnsNotFound` (integration) |
| F19 | `Exec`, `Query` | `42xxx` (bad SQL, missing table) | Internal; `*Error` carries the code | 500; fix the store or migration | `TestWrap_SyntaxAndUndefinedTable_ReturnInternal` |
| F20 | any `Querier` method | called after `Close` | Unavailable | fix app ordering | `TestPool_UseAfterClose_ReturnsUnavailable` (integration) |
| F21 | `Transact` | invalid `Mode` or nil `fn` | Invalid | fix the store | `TestTransact_InvalidModeOrNilFn_ReturnsInvalid` |
| F22 | `Transact` | `Write` while `ReadOnly()` is true | Unavailable wrapping `ErrReadOnly`, no round trip | "this copy is read-only" | `TestTransact_WriteOnReadOnly_FailsFastWithErrReadOnly` |
| F23 | `Transact` | `fn` returns an error | that error's kind; rolled back; rollback error joined | per the kind | `TestTransact_FnFails_RollsBackAndKeepsKind` (integration) |
| F24 | `Transact` | `fn` panics | rolled back, panic re-raised | crash (a bug) | `TestTransact_FnPanics_RollsBackAndRepanics` (integration) |
| F25 | `Transact` | `BEGIN` or `COMMIT` fails | `Wrap`'s kind; hooks not run | per the kind | `TestTransact_CommitFails_SkipsHooks` (integration, a deferred constraint) |
| F26 | `Transact` | ctx ends inside | Canceled / Timeout; rolled back | stop | `TestTransact_ContextCanceled_RollsBack` (integration) |
| F27 | `AfterCommit` | `fn` nil | Invalid | fix the caller | `TestAfterCommit_NilFn_ReturnsInvalid` |
| F28 | `Ping` | the server does not answer | Unavailable | `/readyz` 503 | `TestPing_Unreachable_ReturnsUnavailable` (unit, closed port) |
| F29 | `Ping` | ctx ends | Canceled / Timeout | `/readyz` 503 | `TestPing_ContextCanceled_ReturnsCanceled` |
| F30 | `Close` | ctx ends before the watcher stopped or connections were released (a late caller still holds one) | Timeout / Canceled, returned when ctx ends and not later; the pool keeps closing in the background; the holder's next call is Unavailable (F20); second `Close` returns nil | log and exit | `TestClose_ContextExpired_ReturnsTimeout` (a held connection; `Close` returns at the deadline; the holder's next `Exec` is `Unavailable`; releasing it afterwards does not panic), `TestClose_Twice_IsNil` |
| F31 | `Wrap` | an unrecognised error | Internal | 500 | `TestWrap_UnknownError_ReturnsInternal` |
| F32 | `Set.Validate` | bad `Name` or `Schema` grammar; `Schema` neither `core` nor `Name`; nil `Files` | Invalid, one `FieldError` each | fix app or the module | `TestSetValidate_Table` |
| F33 | `Version` | a root entry is not `NNNN_name.sql` (a subdirectory, `README`, `1_init.sql`, `0001-init.sql`, upper case) | Invalid naming the entry | fix the module | `TestVersion_BadFileName_ReturnsInvalid` |
| F34 | `Version` | numbers repeat, have a gap, do not start at `0001`, or there are no files | Invalid | fix the module | `TestVersion_DuplicateGapStartOrEmpty_ReturnsInvalid` |
| F35 | `Version` | a file cannot be read or is empty | Invalid | fix the module | `TestVersion_UnreadableOrEmptyFile_ReturnsInvalid` |
| F36 | `Migrate` | the database is read-only | Unavailable wrapping `ErrReadOnly`; nothing applied | run on the writer | `TestMigrate_ReadOnly_RefusesBeforeApplying` (integration) |
| F37 | `Migrate` | another run holds the advisory lock | Conflict; nothing applied | wait, rerun | `TestMigrate_Concurrent_SecondReturnsConflict` (integration) |
| F38 | `Migrate`, `CheckSchema` | an applied file's checksum or name differs from the embedded file | Conflict naming set and file; nothing applied | restore the file; a new number for the change | `TestMigrate_ChangedAppliedFile_ReturnsConflict`, `TestCheckSchema_ChangedAppliedFile_ReturnsConflict` (integration) |
| F39 | `Migrate`, `CheckSchema` | the recorded version is higher than the binary's | Conflict naming both; "upgrade the binary" | upgrade | `TestMigrate_DatabaseNewerThanBinary_ReturnsConflict`, `TestCheckSchema_RecordedHigher_ReturnsConflict` (integration) |
| F40 | `Migrate` | a file fails | `Wrap`'s kind, wrapped with `<set> <NNNN_name>`; that file rolled back; earlier files and their rows stay | fix the migration, rerun | `TestMigrate_FailingFile_RollsBackThatFileOnly` (integration) |
| F41 | `Migrate`, `Status` | a set invalid, or two sets with one `Name` | Invalid | fix app | `TestMigrate_DuplicateSetName_ReturnsInvalid` |
| F42 | `Migrate` | ctx ends | Canceled / Timeout; the current file rolled back; lock released | rerun | `TestMigrate_ContextCanceled_RollsBackCurrentFile` (integration) |
| F43 | `Migrate` | the bootstrap cannot create `core` or the table (privilege) | `Wrap`'s kind (Internal for `42501`) | grant the role | `TestMigrate_BootstrapDenied_ReturnsKind` (integration, a role without `CREATE`) |
| F44 | `Status` | database unreachable or ctx ends | Unavailable / Timeout / Canceled | `bms check` reports it | `TestStatus_Unreachable_ReturnsUnavailable` |
| F45 | `CheckSchema` | no version recorded for the module, including a database with no `core.schema_migration` | Conflict: "module volunteers: schema version 0003 expected, none applied: run bms migrate" | run `bms migrate` | `TestCheckSchema_EmptyDatabase_ReturnsConflict`, `TestCheckSchema_NoRowForModule_ReturnsConflict` (integration) |
| F46 | `CheckSchema` | recorded version differs | Conflict naming module and both versions; "run bms migrate" when lower | run `bms migrate` | `TestCheckSchema_RecordedLower_ReturnsConflictSayingMigrate` (integration) |
| F47 | `CheckSchema` | see F38 | Conflict | — | (F38's test) |
| F48 | `CheckSchema` | `migrations` malformed (F33–F35) | Invalid | fix the module | `TestCheckSchema_MalformedMigrations_ReturnsInvalid` |
| F49 | `CheckSchema` | database unreachable or ctx ends | Unavailable / Timeout / Canceled | fix the database | `TestCheckSchema_Unreachable_ReturnsUnavailable` |
| F50 | `CheckSchema` | `module` fails the name grammar | Invalid | fix app | `TestCheckSchema_BadModuleName_ReturnsInvalid` |
| F51 | `NewMigrator` | `pool` or `logger` nil | Invalid | fix app | `TestNewMigrator_NilDependency_ReturnsInvalid` |
| F52 | `dbtest.Fake` | `Exec` or `Transact(Write)` while read-only | Unavailable wrapping `ErrReadOnly` | the module under test reports it | `TestFake_ReadOnly_RefusesWrites` |
| F53 | `dbtest.Fake` | `QueryRow(...).Scan` | NotFound (always: the fake holds no rows) | treat as absent | `TestFake_QueryRow_ReturnsNotFound` |
| F54 | `dbtest.TempDB` | `BMS_TEST_DB_DSN` unset | `t.Skip` | — | `TestTempDB_SkipsWithoutDSN` |
| F55 | `dbtest.TempDB` | the role cannot create a database | `t.Fatal` with the reason | grant `CREATEDB` | covered by running the suite with a restricted role once, by hand |
| F56 | `Open` | `Options.Meter` set and a gauge registration fails (telemetry F20–F22: a duplicate name in the `db` scope) | the meter's kind (Invalid / Conflict); the pgx pool closed before returning | fix app (one `Open` per meter scope) | `TestOpen_GaugeRegistrationFails_ReturnsKindAndClosesPool` (unit, a fake `telemetry.Meter` that refuses) |
| F57 | the `QueryTracer` adapter | a statement fails | none to the caller beyond the statement's own error: the span is marked failed with `error.kind` and the SQLSTATE; statement text and arguments are never on it | — | `TestQueryTracer_FailedStatement_SpanHasKindAndSQLSTATENoText` |

Members that cannot fail, and why: `ReadOnly` (an atomic read); `Stats` (a copy of
pgxpool's counters; zeros after `Close`); `Mode.String`, `Error.Error` (formatting of
fields); `Wrap` on nil (returns nil); `NewFake`, `SetReadOnly`, `Calls` (in-memory). Each
has a success-path test.

## Lock-in check

External technology wrapped: PostgreSQL (PostgreSQL License, OSI-approved) through pgx v5
(MIT). Both are OSI-licensed and self-hosted by every regional (D8), so there is nothing to
substitute to stay free. The interface relies on three PostgreSQL features beyond SQL:
`pg_is_in_recovery()`, `transaction_read_only`, and advisory locks; every PostgreSQL
release since 9.x has them, and the PostgreSQL-protocol forks that are OSI-licensed
(YugabyteDB, Apache 2.0) implement them. Nothing depends on a hosted service. Replacing
PostgreSQL with another database is D8's storage seam, not this package.

## Test plan

**Unit** (no infrastructure, `go test -race`, package `db_test` plus internal tests for
the watcher and the connect loop, with `clock.Fake` and fake probers; invented values; a
password marker `ZQ9MARKER7` in every URL and `!strings.Contains(err.Error(), marker)` on
every error path):

- Options: `TestOptionsValidate_MirrorsConfigBounds` (the same bounds as config.md's
  catalogue, pinned by a table); F1–F3.
- Open: F4, F5, F6 (a listener on `127.0.0.1:0` closed before `Open`; the fake clock shows
  the schedule `500ms, 1s, 2s, 4s, 5s, 5s...` and the `ConnectTimeout` deadline), F8, F9;
  `TestOpen_NeverLogsOrEchoesPassword` (a capturing `slog.Handler`).
- Wrap: `TestWrap_MappingTable` (one row per line of the mapping table, built from
  synthetic `*pgconn.PgError` values with `Detail` and `Message` set to the marker and
  asserting the result's text lacks it); `TestWrap_NilIsNil`;
  `TestWrap_KeepsExistingKindAndPrependsOp`; `TestError_String_NamesOnly`;
  `TestWrap_ErrReadOnlyIsErrorsIsThroughWrapping`; F12–F16, F19, F31.
- Watcher: `TestWatcher_FlipsStateAndLogsOncePerTransition` (fake prober, fake clock:
  writable → read-only → probe failures keep state and log once → writable);
  `TestWatcher_StopsOnClose`.
- Transact without a database: F21, F22 (a `Pool` built in an internal test with the
  read-only flag set), F27; `TestAfterCommit_OutsideTransaction_RunsImmediately`.
- Telemetry (internal tests over the adapter with `telemetrytest.New(t)`; the SQL text and
  an argument carry the marker): `TestQueryTracer_SpanNamedByOperation_Table` (`select`,
  `  INSERT`, `with ... `, `begin`, an unclassifiable text → `db.OTHER`; kind
  `SpanClient`; `db.system=postgresql`); `TestQueryTracer_NeverRecordsSQLOrArgs`
  (`!c.Contains(marker)` over spans and logs); `TestOpen_NilTracerAndMeter_NoSpansNoGauges`;
  `TestGauges_ObserveFromStats` (a fake `Stats` source; two `state` samples, max,
  pending; nil after `Close`); F56, F57.
- Migrations: `TestVersion_ParsesContiguousFiles` (`fstest.MapFS`);
  `TestVersion_HighestNumberIsVersion`; F32–F35, F41, F48, F50, F51.
- dbtest: `TestFake_RecordsCallsInOrderWithInTx`;
  `TestFake_TransactRunsHooksAfterFn`; `TestFake_SafeForConcurrentUse`; F52, F53.
- Lint: passes depguard (`core`, `storage-seam`), `errcheck`, `errorlint`, `wrapcheck`,
  `rowserrcheck`, `sqlclosecheck`, `noctx`, `contextcheck`, `sloglint`, `gosec`.

**Integration** (`//go:build integration`, `BMS_TEST_DB_DSN` from the running
`deploy/compose` stack, each test on its own `dbtest.TempDB`):

- Scanning: `TestPgx_ScansModelIDAndEmailByUnderlyingType` (model.md Proposed change 4): a
  table with `uuid` and `text` columns; insert and select `model.PersonID`, a
  module-style `model.ID[shift]`, and `model.Email` directly, and once more through the
  documented fallback `[16]byte(id)` / `string(e)`; a `NULL` uuid column scans into a `*model.PersonID` (nil = none), not into a zero ID
  (owner, 2026-10-17).
- Querier: `TestQuerier_RoundTrip`; `TestQuery_RowsCloseCancelsDeadline` (no goroutine
  leak, `runtime.NumGoroutine` before and after); F11, F18, F20.
- Transactions: `TestTransact_CommitRunsHooksInOrderWithCallerCtx`;
  `TestTransact_NestedJoinsOuter` (an inner `Transact` sees the outer's uncommitted row;
  an inner error rolls back both); `TestTransact_ReadModeOnReadOnlyDatabase_Works`;
  `TestTransact_CtxFromAnotherGoroutine_RaceDetectorClean` (documents the rule by a test
  that stays single-goroutine); F17 (via `ALTER DATABASE ... SET
  default_transaction_read_only = on` then a new pool), F23–F26.
- Read-only: `TestReadOnly_ProbeSeesDefaultTransactionReadOnly`;
  `TestPing_RefreshesReadOnlyState`; F28 (a pool whose server is stopped is hard to
  arrange; the unit test covers it with a closed port).
- Telemetry: `TestOpen_WithTracer_OneSpanPerStatementUnderCallerSpan` (a `Query` inside a
  span started by the test yields one child `db.SELECT` span; a `Transact` yields
  `db.BEGIN`, the statement, `db.COMMIT`); `TestStats_ReflectsAcquiredAndIdle` (hold one
  connection open in a transaction: `Acquired` 1; after commit: `Idle` ≥ 1, `Max` equals
  `MaxConns`).
- Migrations: `TestMigrate_EmptyDatabase_AppliesBootstrapThenSetsInOrder` (two invented
  sets, the schemas exist after, `core.schema_migration` has one row per file with
  checksums and durations); `TestMigrate_Idempotent_SecondRunAppliesNothing`;
  `TestMigrate_NewFileOnRerun_AppliesOnlyThePendingOne`;
  `TestMigrate_CreatesSchemaIfMissing`; `TestMigrate_DisabledSetNotPassed_IsLeftAlone`;
  `TestStatus_ReportsBuiltAppliedAndProblem` (empty database: `Applied 0`, `Err` Conflict;
  after migrate: equal and nil); `TestCheckSchema_CurrentSet_ReturnsNil`;
  `TestCheckSchema_OnEmptyDatabase_ReturnsConflictNotInternal` (module-contract Proposed
  change 4); F36–F40, F42, F43, F45–F47, F49.
- Conformance with the contract: `TestMigratorSatisfiesSchemaChecker` is a compile-time
  assertion in `app` (`var _ module.SchemaChecker = (*db.Migrator)(nil)`), since `db`
  may not import `module`.

No test uses a real person, host name or password. The compose role is a superuser, which
`TempDB` needs for `CREATE DATABASE` and F43 needs to create a restricted role.

## Open questions

None. The three questions the Draft asked (minimum PostgreSQL version; one transaction per
file rather than per module; `default_transaction_read_only` as read-only) were answered by
the owner on 2026-10-16 and are recorded as decisions 20, 10 and 7.

## Proposed changes elsewhere

Status as of 2026-10-17: items 1, 2, 3, 4, 5, 6, 7, 8, 9, 12 and the `.env.example` half of
11 are applied. Items 13, item 10 for the specs not yet written, and the `docs/operating.md` half of 11 wait for their
target to be written and are carried by this list until then.

1. **`plan/07-abstractions.md`, `db` entry** (applied 2026-10-16): replace "Owns" with: the pool and its
   bounded, retried connect; statements with deadlines, on the pool or on the transaction
   the context carries; `Transact` with after-commit hooks; `Wrap` and `*Error`
   (driver errors classified into `errs` kinds, values scrubbed); read-only detection
   (`pg_is_in_recovery()` or `default_transaction_read_only`, probed on a schedule, on
   `Ping` and on every refused write) exposed as `ReadOnly()` for `/readyz`, `jobs` and
   `notify`; `ErrReadOnly` (`Unavailable`) for a write on a read-only copy; the migration
   runner over ordered sets (the core's, then `Plan.Order()`), one transaction per file,
   recorded with checksums in `core.schema_migration`; `CheckSchema` for `module.Build`
   (D17); `dbtest` (a fake `DB`, a temporary database). "Doesn't do": any domain table or
   query; retrying transactions; delivering signals; the switchover.
2. **`plan/design/module-contract.md`** (applied 2026-10-16): `Deps.DB` is `db.DB` as defined here;
   `moduletest.Conformance` uses `dbtest.NewFake()` with `SetReadOnly(true)` for
   `start_read_only` and `delete_read_only`, and the plain fake for
   `export_unknown_person_is_empty`; F45's kind is `errs.Unavailable` wrapping
   `db.ErrReadOnly`; `Build` passes `*db.Migrator` as the `SchemaChecker` (asserted in
   `app`).
3. **`plan/design/signals.md` (row 10)** (applied 2026-10-16, by signals.md's Proposed
   change 2: delivery from `AfterCommit`, no outbox, no change to `db` beyond the sentence
   on ctx added to `AfterCommit`'s doc): publish inside the publisher's transaction and
   deliver from a `db.AfterCommit` hook (09 §1: after commit, failures logged and retried
   by `jobs`, never rolling back the publisher); outside a transaction the hook runs at
   once. If `signals` instead chooses an outbox table, it writes it through the same
   `Transact` and still needs no change here.
4. **`plan/design/api.md` (row 16)** (applied 2026-10-17, by api.md's Proposed change 5:
   `503 read_only` in its decision 16 and F36; `/readyz` as proposed, single-flighted and
   cached for 1 s; `Built`, `Applied` and `current` per set on `GET /api/admin/modules`
   through `api.SchemaStatus`, which `app` builds over `Migrator.Status`; stores turn
   `*db.Error.Constraint` into a `validate.FieldError` themselves; this spec's `Close` doc,
   F30 and "Concurrency and lifecycle" gained the late-holder rule): the error mapper checks `errors.Is(err,
   db.ErrReadOnly)` before the kind and answers 503 with code `read_only` and the message
   key for "this copy is read-only"; `/readyz` calls `Pool.Ping` and reports `ReadOnly()`
   as `{"database": "ok", "mode": "read-write" | "read-only"}`; `GET /api/admin/modules` adds
   each module's `Status` (`Built`, `Applied`) from `Migrator.Status`; a store turns
   `*db.Error.Constraint` into a `validate.FieldError` itself (api does not know
   constraints).
5. **`plan/design/app.md` (row 18)** (applied 2026-10-17, by app.md's Proposed change 3: "`serve`, step by step" step 5, "`migrate`, step by step", "`check`, step by step" and the compile-time assertion in its test plan; `None()` and `ErrNoDatabase` added above): `Open` once per command with `Options` copied from
   `config.DB`, `clock.Real()` and the telemetry logger; `Close` at shutdown after
   `Set.Stop` and the HTTP server, within `http.shutdown_timeout`; build the migration
   sets as the `db` bootstrap (implicit), then each core package's exported `Set` in a
   fixed order (`identity`, `people`, `authz`, `departments`, `events`, `audit`,
   `settings`, `jobs`, `files`, as each spec defines them), then `Plan.Order()` as
   `Set{Name: m.Name, Schema: m.Name, Files: m.Migrations}` for manifests with
   `Migrations != nil`; `bms migrate` runs under a 30 min deadline and prints each
   `Applied`; `bms check` prints `Status` per set and the names (never values) of any
   `PG*` variables in the environment; the compile-time assertion `var _
   module.SchemaChecker = (*db.Migrator)(nil)`; development and tests may run `migrate`
   automatically (D17).
6. **`plan/09-core-features.md` §1 "Done when"** (applied 2026-10-16): "applies ... in one transaction per
   module" becomes "applies ... one transaction per migration file, recording the version
   in the same transaction" (decision 10); add "`/readyz` reports read-only from
   `db`'s probe".
7. **`.claude/skills/postgres-sql/SKILL.md`** (applied 2026-10-16): minimum version 16 (decision 20);
   primary keys are `uuid` (model.md decision 1; `bigint identity` dropped); migration
   file grammar `NNNN_name.sql` with contiguous numbers from `0001`, the highest being the
   version, checksummed and never edited; the migrator creates the schema, so `0001` starts
   with tables; no non-transactional migrations yet (decision 11; drop "a migration that
   can't says so at the top"); every store method returns through `db.Wrap(op, err)`;
   `*db.Error` for constraint names; the error-mapping table replaced by a pointer to this
   spec's table (with `25006` → `Unavailable` + `ErrReadOnly`, `23503` → `Invalid` by
   default); transactions through `deps.DB.Transact` with the ctx-carried transaction and
   `db.AfterCommit`.
8. **`.claude/skills/go-conventions/SKILL.md`, "Resources" example** (applied 2026-10-16): use
   `db.Wrap(op, err)` instead of `errs.E(op, kindOf(err), err)`, and note that `Rows.Close`
   releases the statement deadline.
9. **`plan/design/lib-core.md`, `retry` consumer rows** (applied 2026-10-16): `db.Open` uses `retry.Do` with
   its own constant policy; note beside `Retryable` that `db.ErrReadOnly` is `Unavailable`
   and a loop around a write must check the sentinel first (decision 9).
10. **Every core spec with tables (`identity`, `people`, `departments-events`, `authz`,
    `audit`, `settings`, `jobs`, `files`)** (applied 2026-10-18 for `audit`, `identity`, `authz` and `people`: `postgres.Migrations`, `audit.Set`/`identity.Set`/`authz.Set`/`people.Set`, schema `core`, set names `authz` and `people` in the fixed order; people's `0001` backfills one person row per existing account, the one migration-time read of `core.account` by another set; pending for the rest: specs not written): export `var Migrations fs.FS` from its
    `postgres/` package (or a `db.Set`), schema `core`, set name equal to the package name;
    the set names are reserved module names already (module-contract "Reserved module
    names"); `audit`'s migration revokes `UPDATE` and `TRUNCATE` from the application role and
    installs the guard trigger, so the table is append-only except through `core.audit_purge`
    (audit.md decision 9; this replaces "insert and select only", which a one-role installation
    cannot enforce while keeping the retention purge).
11. **`docs/operating.md` (pending: not written) and `deploy/compose/.env.example` (applied
    2026-10-16):** PostgreSQL 16
    or newer; the `bms` role needs `CREATE` on the database (schemas) and ownership of
    what it creates; pgx honours `PG*` variables and `~/.pgpass` for anything `BMS_DB_URL`
    leaves unset (decision 14), with `PGSSLMODE`/`PGSSLROOTCERT` as the recommended way to
    pin TLS; `ALTER DATABASE ... SET default_transaction_read_only = on` as the manual
    freeze (decision 7); backups contain everything (privacy-data); integration tests
    need `CREATEDB` for `dbtest.TempDB`.
12. **`plan/design/telemetry.md` (row 15b)** (applied 2026-10-16, by telemetry.md's
    Proposed change 2): if pool metrics are wanted, propose
    `Pool.Stats()` returning a plain struct (acquired, idle, total, waits) back to this
    spec; statement tracing through pgx's `QueryTracer` is set in `Open` by an option
    added then. Answered: `Options.Tracer` and `Options.Meter` (nil means none), the
    `QueryTracer` adapter in `Open` step 2, `Pool.Stats()` and the three gauges in `Open`
    step 5, F56 and F57.
13. **`plan/design/jobs.md` (row 14) and `notify.md` (row 11)** (pending: specs not written): read `ReadOnly()` before
    each run and send, and treat `errors.Is(err, db.ErrReadOnly)` as "stop until the next
    probe", not as a retryable failure.
