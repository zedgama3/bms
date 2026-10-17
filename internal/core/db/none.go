package db

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/zedgama3/bms/internal/lib/errs"
)

// None returns a DB with no database behind it, for `bms openapi` and `bms check`, which
// build every module without connecting (app.md decision 7). Every Exec, Query,
// Row.Scan, Rows.Err and Transact fails with errs.Unavailable wrapping ErrNoDatabase;
// ReadOnly() is false; AfterCommit outside a transaction still runs its hook at once
// (nothing was written). Cannot fail.
func None() DB { return noDB{} }

type noDB struct{}

func (noDB) err(op string) error { return errs.E(op, errs.Unavailable, ErrNoDatabase) }

func (n noDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, n.err("db.None.Exec")
}

func (n noDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	e := n.err("db.None.Query")
	return failedRows{err: e}, e
}

func (n noDB) QueryRow(context.Context, string, ...any) pgx.Row {
	return failedRow{err: n.err("db.None.QueryRow")}
}

func (n noDB) Transact(context.Context, Mode, func(context.Context) error) error {
	return n.err("db.None.Transact")
}

func (noDB) ReadOnly() bool { return false }
