package db

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/zedgama3/bms/internal/core/db/internal/txctx"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// Transact runs fn inside one transaction and commits when fn returns nil; see DB.Transact.
// A nested call with a ctx carrying this Pool's transaction joins it. A Write nested in a
// Read transaction fails with errs.Invalid (the server would refuse it with 25006, which
// must not be mistaken for a read-only copy).
func (p *Pool) Transact(ctx context.Context, mode Mode, fn func(ctx context.Context) error) error {
	const op = "db.Pool.Transact"
	if mode != Read && mode != Write {
		return errs.E(op, errs.Invalid, errors.New("invalid transaction mode "+mode.String()))
	}
	if fn == nil {
		return errs.E(op, errs.Invalid, errors.New("nil function"))
	}
	if outer, _ := txFrom(ctx, p); outer != nil {
		if mode == Write && !outer.Write {
			return errs.E(op, errs.Invalid, errors.New("a write transaction cannot join a read transaction"))
		}
		return fn(ctx)
	}
	if err := p.usable(op); err != nil {
		return err
	}
	if mode == Write && p.ReadOnly() {
		return errs.E(op, errs.Unavailable, ErrReadOnly)
	}

	bctx, cancel := p.deadline(ctx)
	conn, err := p.acquire(bctx)
	if err != nil {
		cancel()
		return p.fail(ctx, op, err)
	}
	defer conn.Release()
	// Write is a plain BEGIN, so the session's default_transaction_read_only applies and
	// the server answers 25006 on a read-only copy; Read is BEGIN READ ONLY.
	var access pgx.TxAccessMode
	if mode == Read {
		access = pgx.ReadOnly
	}
	tx, err := conn.BeginTx(bctx, pgx.TxOptions{AccessMode: access})
	cancel()
	if err != nil {
		return p.fail(ctx, op, err)
	}

	state := &txctx.State{Owner: p, Impl: tx, Write: mode == Write}
	finished := false
	defer func() {
		if !finished { // fn panicked or called runtime.Goexit: roll back, the panic goes on
			state.Finish(false)
			if rerr := p.rollback(ctx, tx); rerr != nil {
				// A panic is unwinding, so there is no return value to carry the error.
				p.logger.ErrorContext(ctx, "rollback after a panic failed", slog.Any("error", Wrap(op, rerr)))
			}
		}
	}()

	if ferr := fn(txctx.With(ctx, state)); ferr != nil {
		finished = true
		state.Finish(false)
		if rerr := p.rollback(ctx, tx); rerr != nil {
			return errors.Join(ferr, scrub(rerr))
		}
		return ferr
	}

	cctx, ccancel := p.deadline(ctx)
	cerr := tx.Commit(cctx)
	ccancel()
	finished = true
	if cerr != nil {
		state.Finish(false)
		return p.fail(ctx, op, cerr)
	}
	for _, hook := range state.Finish(true) {
		hook(ctx)
	}
	return nil
}

// rollback rolls tx back on a context that survives the caller's cancellation, so a
// canceled request still returns its connection cleanly.
func (p *Pool) rollback(ctx context.Context, tx pgx.Tx) error {
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), p.opts.QueryTimeout)
	defer cancel()
	err := tx.Rollback(rctx)
	if err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		return err //nolint:wrapcheck // scrubbed by the caller
	}
	return nil
}
