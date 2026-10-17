---
name: postgres-sql
description: The project's PostgreSQL rules — one schema per module, forward-only migrations, plain SQL in embedded files with pgx, query and index style, transactions, and translating PostgreSQL errors into errs kinds. Load before writing or reviewing schema, migrations or queries for the core or any module.
---

# PostgreSQL

PostgreSQL is the only database (D8). Minimum version: 16 (`db.MinServerVersion`, db.md
decision 20). The `db` spec (`plan/design/db.md`, Approved) is authoritative where it
differs from this file.

## Schemas

- The core owns schema `core` (people, departments, events, roles, grants, audit, settings,
  files, jobs, and `core.schema_migration`, which `db` owns).
- Each module owns schema `<module>` (for example `volunteers`). Its tables reference only
  its own tables and `core` tables. Never another module's schema.
- Table names are singular, lowercase, `snake_case` (`volunteers.shift`). Primary keys are
  `id uuid` (model.md decision 1: `model.ID[E]`, scanned by pgx by underlying type). Every
  table has `created_at` and `updated_at` (`timestamptz`); an append-only table a spec
  explicitly exempts (`core.schema_migration`) may drop `updated_at`.
- Data that belongs to a department carries `department_id`; data that belongs to an event
  (a gathering such as a burn, D24) carries `event_id`.
- Comment personal-data columns with their sensitivity level:
  `COMMENT ON COLUMN core.person.legal_name IS 'sensitivity: personal';`
- Write schema-qualified names everywhere (`volunteers.shift`, `core.person`). No
  `search_path` is set, so nothing may depend on it.

## Migrations

- Forward only, one set per module or core package, at the root of
  `postgres/migrations/`: files named `NNNN_name.sql` (four digits, underscore, a name of
  `[a-z0-9_]+`, `.sql`). Numbers start at `0001`, are unique and contiguous; the highest
  number is the set's version. Nothing else may sit at that root (a `README` or a
  subdirectory fails `db.Version`).
- The migrator creates the set's schema (`CREATE SCHEMA IF NOT EXISTS`) before the first
  file runs, so `0001_init.sql` starts with tables.
- Applied by `bms migrate`, never by the app at start-up (D17): the core's sets first, then
  the enabled modules' in `Plan.Order()`. Each file runs as one statement batch inside one
  transaction, and its `core.schema_migration` row (set, version, name, SHA-256 checksum)
  is inserted in that same transaction. `module.Build` checks the recorded version and the
  checksums at start and refuses to start on a mismatch or a changed file.
- Every migration is transactional. There is no non-transactional path yet (db.md decision
  11): no `CREATE INDEX CONCURRENTLY`. Propose a marker to the `db` spec if a table ever
  needs it.
- Safe on a live database with data: add columns as nullable or with a default; backfill in
  a separate step; no long locks on large tables without saying so. Migrations must be safe
  to apply on the primary while a replica keeps serving reads.
- Never edit a migration that has been released: its checksum is recorded, and `migrate`
  and `serve` both stop on a changed file. Write a new number.
- A module's tests call `db.Version(Migrations)` to prove its files parse; integration tests
  get a throw-away database from `dbtest.TempDB`.

## Queries

- Plain SQL in `.sql` files embedded with `go:embed`, executed with `pgx` through `db.DB`
  (`Exec`, `Query`, `QueryRow`; `Transact`). No ORM, no query builder. `db` derives a
  deadline for every statement; the store does not add one.
- All of it lives in the `postgres/` subpackage, behind the storage interface the service
  declares (D8). The interface speaks in domain types; no `pgx` or SQL types leak out.
- Every error that leaves a store method goes through `db.Wrap(op, err)`, including errors
  from pgx helpers that bypass `Querier` (`pgx.CollectOneRow`). `Wrap` is idempotent: an
  already-classified error only gains the op.
- To turn a constraint into a field error, `errors.As(err, &pgErr)` with `*db.Error` and
  read `pgErr.Constraint`; never parse the message. Where a `23503` means "in use" rather
  than "bad reference", the store re-kinds it with `errs.E(op, errs.Conflict, err)`.
- Parameters only (`$1`). Dynamic sort columns or filters come from a fixed allow-list in Go.
- Every list query is paginated (keyset preferred) and has a stable order.
- Permission filters are part of the query (for example `WHERE department_id = ANY($2)`),
  not applied after fetching rows.
- Check `EXPLAIN (ANALYZE, BUFFERS)` for list and report queries on representative invented
  data. Never debug with production values: `*db.Error` carries names, not row values, on
  purpose.

## Transactions

- A use case that writes more than one row runs
  `deps.DB.Transact(ctx, db.Write, func(ctx context.Context) error { ... })` and calls
  ordinary store methods with the ctx it is given. The transaction travels in the context:
  every `Querier` call with that ctx runs on it, so a store method has one code path in or
  out of a transaction. Reads that may run on a read-only copy use `db.Read`.
- A nested `Transact` joins the enclosing transaction (no savepoints). The ctx a
  transaction hands out must not cross goroutines.
- Work that must happen only if the transaction commits (delivering a signal, enqueueing a
  job) is registered with `db.AfterCommit(ctx, fn)`; outside a transaction it runs at once.
  Signals (domain events, D24) are published through the `signals` contract inside the
  transaction and delivered from that hook, as the `signals` spec defines.
- A write on a read-only copy fails with `errs.Unavailable` wrapping `db.ErrReadOnly`; the
  api turns it into "this copy is read-only". A retry loop around a write must check
  `errors.Is(err, db.ErrReadOnly)` before `retry.Retryable`.

## Error mapping (at the `db` boundary)

The full table lives in db.md, "Error mapping (`Wrap`)"; it is the one source. The rows a
store author meets most:

| PostgreSQL | errs kind |
|---|---|
| `23505` unique_violation | `Conflict`, `*db.Error.Constraint` set |
| `23503` foreign_key_violation | `Invalid` by default; the store re-kinds to `Conflict` for "in use" |
| `23502`, `23514`, `22xxx` | `Invalid` |
| `25006` read_only_sql_transaction | `Unavailable` wrapping `db.ErrReadOnly` |
| `40001` serialization_failure, `40P01` deadlock, `55P03` | `Unavailable` (retryable by a caller that chooses to; `db` never retries) |
| `57014` query_canceled, deadline passed | `Timeout` |
| `57014` query_canceled, caller canceled the context | `Canceled` |
| connection errors, `08xxx`, `53xxx`, `57P0x` | `Unavailable` |
| `42xxx` (bad SQL, missing table) | `Internal`: a bug or a missing migration |
| `pgx.ErrNoRows` | `NotFound` |
| anything else | `Internal` |
