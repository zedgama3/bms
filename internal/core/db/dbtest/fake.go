// Package dbtest gives tests a DB. Fake needs no database and is what moduletest and a
// module's unit tests hand to a Factory; TempDB is a real, throw-away database for
// integration tests of postgres/ packages and of db itself.
package dbtest

import (
	"context"
	"errors"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/internal/txctx"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// Fake implements db.DB with no database behind it: Exec records the call and returns an
// empty CommandTag; Query returns rows with no row; QueryRow's Scan fails with
// errs.NotFound; Transact runs fn (Write fails first when read-only) and then the
// AfterCommit hooks; ReadOnly reports what SetReadOnly set. Safe for concurrent use.
type Fake struct {
	mu       sync.Mutex
	readOnly bool
	calls    []Call
}

var _ db.DB = (*Fake)(nil)

// Call is one recorded statement.
type Call struct {
	SQL  string
	Args []any
	InTx bool
}

// NewFake returns a writable Fake. Cannot fail.
func NewFake() *Fake { return &Fake{} }

// SetReadOnly makes every Exec and every Write Transact fail with errs.Unavailable
// wrapping db.ErrReadOnly (F52), as a Pool does on a replica. Cannot fail.
func (f *Fake) SetReadOnly(ro bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.readOnly = ro
}

// ReadOnly reports what SetReadOnly set. Cannot fail.
func (f *Fake) ReadOnly() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.readOnly
}

// Calls returns every statement run so far, in order, as a copy. A statement refused
// because the Fake is read-only was not run and is not listed. Cannot fail.
func (f *Fake) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Call, len(f.calls))
	for i, c := range f.calls {
		c.Args = append([]any(nil), c.Args...)
		out[i] = c
	}
	return out
}

func (f *Fake) record(ctx context.Context, sql string, args []any) {
	t := txctx.From(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, Call{SQL: sql, Args: append([]any(nil), args...), InTx: t != nil && t.Owner == f})
}

// Exec records the call and returns an empty CommandTag; it fails with errs.Unavailable
// wrapping db.ErrReadOnly while the Fake is read-only (F52).
func (f *Fake) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if f.ReadOnly() {
		return pgconn.CommandTag{}, errs.E("dbtest.Fake.Exec", errs.Unavailable, db.ErrReadOnly)
	}
	f.record(ctx, sql, args)
	return pgconn.CommandTag{}, nil
}

// Query records the call and returns rows with no row.
func (f *Fake) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	f.record(ctx, sql, args)
	return emptyRows{}, nil
}

// QueryRow records the call; its Scan fails with errs.NotFound (F53).
func (f *Fake) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	f.record(ctx, sql, args)
	return noRow{}
}

// Transact runs fn, then the AfterCommit hooks when fn returned nil. A Write transaction
// fails with errs.Unavailable wrapping db.ErrReadOnly while the Fake is read-only (F52);
// an invalid mode or nil fn is errs.Invalid; a nested call joins the outer one.
func (f *Fake) Transact(ctx context.Context, mode db.Mode, fn func(ctx context.Context) error) error {
	const op = "dbtest.Fake.Transact"
	if mode != db.Read && mode != db.Write {
		return errs.E(op, errs.Invalid, errors.New("invalid transaction mode "+mode.String()))
	}
	if fn == nil {
		return errs.E(op, errs.Invalid, errors.New("nil function"))
	}
	if outer := txctx.From(ctx); outer != nil && outer.Owner == f {
		if mode == db.Write && !outer.Write {
			return errs.E(op, errs.Invalid, errors.New("a write transaction cannot join a read transaction"))
		}
		return fn(ctx)
	}
	if mode == db.Write && f.ReadOnly() {
		return errs.E(op, errs.Unavailable, db.ErrReadOnly)
	}
	state := &txctx.State{Owner: f, Write: mode == db.Write}
	finished := false
	defer func() {
		if !finished { // fn panicked: its hooks must not run
			state.Finish(false)
		}
	}()
	err := fn(txctx.With(ctx, state))
	finished = true
	if err != nil {
		state.Finish(false)
		return err
	}
	for _, hook := range state.Finish(true) {
		hook(ctx)
	}
	return nil
}

// noRow is a Row with no row.
type noRow struct{}

func (noRow) Scan(...any) error { return db.Wrap("dbtest.Fake.QueryRow", pgx.ErrNoRows) }

// emptyRows is Rows with no row.
type emptyRows struct{}

func (emptyRows) Close()                                       {}
func (emptyRows) Err() error                                   { return nil }
func (emptyRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (emptyRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (emptyRows) Next() bool                                   { return false }
func (emptyRows) Scan(...any) error                            { return db.Wrap("dbtest.Fake.Rows", pgx.ErrNoRows) }
func (emptyRows) Values() ([]any, error)                       { return nil, nil }
func (emptyRows) RawValues() [][]byte                          { return nil }
func (emptyRows) Conn() *pgx.Conn                              { return nil }
func (emptyRows) TypeMap() *pgtype.Map                         { return nil }
