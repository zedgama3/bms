package db

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/zedgama3/bms/internal/lib/errs"
)

// deadline derives the statement deadline: the earlier of ctx's and now + QueryTimeout
// (context.WithTimeout takes the earlier of the two).
func (p *Pool) deadline(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, p.opts.QueryTimeout)
}

// acquire takes a connection, counting the caller as pending while it waits.
func (p *Pool) acquire(ctx context.Context) (*pgxpool.Conn, error) {
	p.pending.Add(1)
	conn, err := p.pool.Acquire(ctx)
	p.pending.Add(-1)
	if err != nil {
		if p.closed.Load() { // Close began while this caller waited
			return nil, errs.E("db.Pool.acquire", errs.Unavailable, errors.New("the database pool is closed"))
		}
		if ctx.Err() != nil {
			return nil, fmt.Errorf("waiting for a connection: %w", err)
		}
		return nil, err //nolint:wrapcheck // classified by the caller
	}
	return conn, nil
}

// fail classifies a driver error for a statement run under ctx: Wrap, plus the pool's
// read-only state on a refused write and Canceled for a 57014 the caller's cancel caused.
func (p *Pool) fail(ctx context.Context, op string, err error) error {
	if err == nil {
		return nil
	}
	out := Wrap(op, err)
	if errors.Is(out, ErrReadOnly) {
		if t, _ := txFrom(ctx, p); t != nil && !t.Write {
			// A write statement inside a Read transaction is the caller's bug; it says
			// nothing about the copy, so the cached state stays.
			var pe *Error
			if errors.As(out, &pe) {
				return errs.E(op, errs.Internal, fmt.Errorf("write statement in a read transaction: %w", pe))
			}
			return errs.E(op, errs.Internal, errors.New("write statement in a read transaction"))
		}
		p.setReadOnly(ctx, true)
		return out
	}
	if errs.Is(out, errs.Timeout) && errors.Is(ctx.Err(), context.Canceled) {
		return errs.E(op, errs.Canceled, out)
	}
	return out
}

// Exec runs a statement that returns no rows, on the transaction ctx carries or on a
// pooled connection, under the statement deadline. Fails with the kind Wrap gives the
// driver's error; errs.Unavailable after Close.
func (p *Pool) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	const op = "db.Pool.Exec"
	if err := p.usable(op); err != nil {
		return pgconn.CommandTag{}, err
	}
	pctx := ctx // the caller's context: what classification consults, unaffected by our own cancel
	ctx, cancel := p.deadline(ctx)
	defer cancel()
	if _, tx := txFrom(ctx, p); tx != nil {
		tag, err := tx.Exec(ctx, sql, args...)
		return tag, p.fail(pctx, op, err)
	}
	conn, err := p.acquire(ctx)
	if err != nil {
		return pgconn.CommandTag{}, p.fail(pctx, op, err)
	}
	defer conn.Release()
	tag, err := conn.Exec(ctx, sql, args...)
	return tag, p.fail(pctx, op, err)
}

// Query runs a statement and returns its rows; the caller must Close them, which releases
// the connection and the statement deadline. Fails as Exec does; Rows.Err and Rows.Scan
// fail the same way. On failure the returned Rows is non-nil, closed and carries the error.
func (p *Pool) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	const op = "db.Pool.Query"
	if err := p.usable(op); err != nil {
		return failedRows{err: err}, err
	}
	pctx := ctx // the caller's context: what classification consults, unaffected by our own cancel
	ctx, cancel := p.deadline(ctx)
	if _, tx := txFrom(ctx, p); tx != nil {
		rows, err := tx.Query(ctx, sql, args...)
		if err != nil {
			if rows != nil {
				rows.Close()
			}
			cancel()
			e := p.fail(pctx, op, err)
			return failedRows{err: e}, e
		}
		return &poolRows{Rows: rows, p: p, op: op, ctx: pctx, cancel: cancel}, nil
	}
	conn, err := p.acquire(ctx)
	if err != nil {
		cancel()
		e := p.fail(pctx, op, err)
		return failedRows{err: e}, e
	}
	rows, err := conn.Query(ctx, sql, args...)
	if err != nil {
		if rows != nil {
			rows.Close()
		}
		conn.Release()
		cancel()
		e := p.fail(pctx, op, err)
		return failedRows{err: e}, e
	}
	return &poolRows{Rows: rows, p: p, op: op, ctx: pctx, cancel: cancel, release: conn.Release}, nil
}

// QueryRow runs a statement expected to return one row. Errors surface from Row.Scan,
// classified: errs.NotFound when there is no row, otherwise as Exec. The caller must call
// Scan, which releases the connection.
func (p *Pool) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	const op = "db.Pool.QueryRow"
	if err := p.usable(op); err != nil {
		return failedRow{err: err}
	}
	pctx := ctx // the caller's context: what classification consults, unaffected by our own cancel
	ctx, cancel := p.deadline(ctx)
	if _, tx := txFrom(ctx, p); tx != nil {
		r := tx.QueryRow(ctx, sql, args...)
		return &poolRow{Row: r, p: p, op: op, ctx: pctx, cancel: cancel}
	}
	conn, err := p.acquire(ctx)
	if err != nil {
		cancel()
		return failedRow{err: p.fail(pctx, op, err)}
	}
	return &poolRow{Row: conn.QueryRow(ctx, sql, args...), p: p, op: op, ctx: pctx, cancel: cancel, release: conn.Release}
}

// poolRows wraps pgx.Rows: Close releases the connection and the deadline; Err and Scan
// return classified errors.
type poolRows struct {
	pgx.Rows
	p       *Pool
	op      string
	ctx     context.Context //nolint:containedctx // the statement's own context, kept for classification until Close
	cancel  context.CancelFunc
	release func()
	once    sync.Once
}

func (r *poolRows) Close() {
	r.Rows.Close()
	r.once.Do(func() {
		if r.release != nil {
			r.release()
		}
		r.cancel()
	})
}

func (r *poolRows) Err() error { return r.p.fail(r.ctx, r.op, r.Rows.Err()) }

func (r *poolRows) Values() ([]any, error) {
	v, err := r.Rows.Values()
	return v, r.p.fail(r.ctx, r.op, err)
}

func (r *poolRows) Scan(dest ...any) error {
	return r.p.fail(r.ctx, r.op, r.Rows.Scan(dest...))
}

// poolRow wraps pgx.Row: Scan runs the statement, then releases.
type poolRow struct {
	pgx.Row
	p       *Pool
	op      string
	ctx     context.Context //nolint:containedctx // see poolRows
	cancel  context.CancelFunc
	release func()
}

func (r *poolRow) Scan(dest ...any) error {
	err := r.Row.Scan(dest...)
	if r.release != nil {
		r.release()
	}
	r.cancel()
	return r.p.fail(r.ctx, r.op, err)
}

// failedRow is a Row whose Scan returns a fixed, classified error.
type failedRow struct{ err error }

func (r failedRow) Scan(...any) error { return r.err }

// failedRows is a closed Rows carrying a fixed, classified error.
type failedRows struct{ err error }

func (failedRows) Close()                                       {}
func (r failedRows) Err() error                                 { return r.err }
func (failedRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (failedRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (failedRows) Next() bool                                   { return false }
func (r failedRows) Scan(...any) error                          { return r.err }
func (r failedRows) Values() ([]any, error)                     { return nil, r.err }
func (failedRows) RawValues() [][]byte                          { return nil }
func (failedRows) Conn() *pgx.Conn                              { return nil }
func (failedRows) TypeMap() *pgtype.Map                         { return nil }
