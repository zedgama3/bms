package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/zedgama3/bms/internal/core/db/internal/txctx"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// Querier runs one statement. DB and the transaction a context carries both implement it.
// Each call derives a deadline (the earlier of the context's and now + QueryTimeout) and
// returns errors already classified by Wrap: Rows.Err, Rows.Scan and Row.Scan likewise.
// Values returned are pgx types, so pgx.CollectRows and friends work; the postgres-sql
// skill says how to use them.
type Querier interface {
	// Exec runs a statement that returns no rows. Fails with the kind Wrap gives the
	// driver's error (F10-F20).
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

// The transaction modes.
const (
	Read  Mode = iota + 1 // BEGIN READ ONLY; allowed on a read-only copy
	Write                 // plain BEGIN, so the session's default_transaction_read_only applies and the server answers 25006; refused on a read-only copy
)

// String returns "read", "write" or "Mode(<n>)". Cannot fail.
func (m Mode) String() string {
	switch m {
	case Read:
		return "read"
	case Write:
		return "write"
	default:
		return fmt.Sprintf("Mode(%d)", uint8(m))
	}
}

// txFrom returns the transaction ctx carries, if owner began it.
func txFrom(ctx context.Context, owner any) (*txctx.State, pgx.Tx) {
	t := txctx.From(ctx)
	if t == nil || t.Owner != owner {
		return nil, nil
	}
	tx, ok := t.Impl.(pgx.Tx)
	if !ok {
		return nil, nil
	}
	return t, tx
}

// AfterCommit registers fn to run once the transaction carried by ctx has committed
// (Transact runs the hooks after Commit returned nil, in order, each with a context
// derived from Transact's own ctx, not the transaction's). When ctx carries no
// transaction, fn runs immediately, because an autocommit write is already durable.
// Hooks are for work that must not run if the transaction rolls back: signals delivers
// published signals from one. A hook cannot fail the transaction (it is already
// committed); it handles its own errors. db passes the caller's ctx unchanged; signals
// detaches the hook's ctx from the request's cancellation itself (signals.md decision 6).
// Fails with errs.Invalid if fn is nil (F27), and if the transaction ctx carries has
// already ended without committing.
func AfterCommit(ctx context.Context, fn func(ctx context.Context)) error {
	t := txctx.From(ctx)
	if t == nil {
		if fn == nil {
			return errs.E("db.AfterCommit", errs.Invalid, errors.New("nil function"))
		}
		fn(ctx)
		return nil
	}
	return t.Add(ctx, fn) //nolint:wrapcheck // txctx returns errs errors
}

// TxMode reports the transaction ctx carries: its Mode and true when some DB's Transact
// (a *Pool's or a dbtest.Fake's; the owner is not checked, as AfterCommit does not check
// it) handed this ctx, or a child of it, to its fn; the zero Mode and false otherwise.
// It still reports a transaction that has ended when the caller kept fn's ctx past
// Transact's return. It reads one context value, does no I/O and cannot fail. audit uses
// it to never spool an entry while ctx carries a transaction. db.None() never runs a
// Transact fn, so a ctx under it never carries a transaction.
func TxMode(ctx context.Context) (Mode, bool) {
	t := txctx.From(ctx)
	if t == nil {
		return 0, false
	}
	if t.Write {
		return Write, true
	}
	return Read, true
}
