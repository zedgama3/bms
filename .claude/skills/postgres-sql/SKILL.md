---
name: postgres-sql
description: The project's PostgreSQL rules — one schema per module, forward-only migrations, plain SQL in embedded files with pgx, query and index style, transactions, and translating PostgreSQL errors into errs kinds. Load before writing or reviewing schema, migrations or queries for the core or any module.
---

# PostgreSQL

PostgreSQL is the only database (D8, Proposed). Minimum version: 16 (to confirm in the `db`
spec). The `db` spec, once Approved, is authoritative where it differs from this file.

## Schemas

- The core owns schema `core` (people, editions, departments, roles, grants, audit, settings,
  files, jobs).
- Each module owns schema `<module>` (for example `volunteers`). Its tables reference only
  its own tables and `core` tables. Never another module's schema.
- Table names are singular, lowercase, `snake_case` (`volunteers.shift`). Primary keys are
  `id` (UUID v7 or bigint identity, decided in the `db` spec). Every table has
  `created_at` and `updated_at` (`timestamptz`).
- Data that belongs to an event year carries `edition_id`.
- Comment personal-data columns with their sensitivity level:
  `COMMENT ON COLUMN core.person.legal_name IS 'sensitivity: personal';`

## Migrations

- Forward only, numbered per module: `postgres/migrations/0001_init.sql`, `0002_...`.
- Applied by the `bms migrate` command, never by the app at start-up (D17). The installer
  records the module's schema version in the same transaction; the module checks it when
  it initializes and refuses to start on a mismatch. They must be safe
  to apply on the primary while a replica keeps serving reads.
- Each runs in a transaction. A migration that can't (for example `CREATE INDEX
  CONCURRENTLY`) says so at the top.
- Safe on a live database with data: add columns as nullable or with a default; backfill in
  a separate step; no long locks on large tables without saying so.
- Never edit a migration that has been released. Write a new one.

## Queries

- Plain SQL in `.sql` files embedded with `go:embed`, executed with `pgx` through the `db`
  contract. No ORM, no query builder.
- All of it lives in the `postgres/` subpackage, behind the storage interface the service
  declares (D8). The interface speaks in domain types; no `pgx` or SQL types leak out.
- Parameters only (`$1`). Dynamic sort columns or filters come from a fixed allow-list in Go.
- Every list query is paginated (keyset preferred) and has a stable order.
- Permission filters are part of the query (for example `WHERE department_id = ANY($2)`),
  not applied after fetching rows.
- Check `EXPLAIN (ANALYZE, BUFFERS)` for list and report queries on representative invented
  data.

## Transactions

- A use case that writes more than one row runs in one transaction, obtained from the `db`
  contract.
- Domain events are published through the `events` contract in the same transaction
  (delivery after commit, as the `events` spec defines).

## Error mapping (at the `db` boundary)

| PostgreSQL | errs kind |
|---|---|
| `23505` unique_violation | `Conflict` |
| `23503` foreign_key_violation | `Invalid` (bad reference) or `Conflict` (in use), per spec |
| `23514` check_violation, `22xxx` data exceptions | `Invalid` |
| `40001` serialization_failure, `40P01` deadlock | `Unavailable` (retryable) |
| `57014` query_canceled with a passed deadline | `Timeout` |
| connection errors, `08xxx`, `53xxx` | `Unavailable` |
| `pgx.ErrNoRows` | `NotFound` |
| anything else | `Internal` |
