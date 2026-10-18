package identity_test

import (
	"context"
	"errors"
	"sync"

	"github.com/jackc/pgx/v5"        //nolint:depguard // faultDB must implement db.DB, whose Query returns pgx.Rows
	"github.com/jackc/pgx/v5/pgconn" //nolint:depguard // faultDB must implement db.DB, whose Exec returns pgconn.CommandTag

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// faultDB wraps a db.DB to inject failures, to look like a read-only copy and to count the
// statements a use case makes. Safe for concurrent use.
type faultDB struct {
	db.DB

	mu      sync.Mutex
	readErr error // returned by Query and QueryRow
	execErr error // returned by Exec
	txErr   error // returned by every Transact
	ro      bool
	reads   int      // Query, QueryRow
	writes  int      // Exec, Write Transact
	txs     int      // every Transact
	sqls    []string // the text of every statement run, in order
}

func newFaultDB(inner db.DB) *faultDB { return &faultDB{DB: inner} }

func (f *faultDB) set(mod func(*faultDB)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	mod(f)
}

func (f *faultDB) ReadOnly() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ro || f.DB.ReadOnly()
}

func (f *faultDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.mu.Lock()
	f.writes++
	f.sqls = append(f.sqls, sql)
	err, ro := f.execErr, f.ro
	f.mu.Unlock()
	if ro {
		return pgconn.CommandTag{}, errs.E("test.faultDB.Exec", errs.Unavailable, db.ErrReadOnly)
	}
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	return f.DB.Exec(ctx, sql, args...) //nolint:wrapcheck // test double
}

func (f *faultDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	f.mu.Lock()
	f.reads++
	f.sqls = append(f.sqls, sql)
	err := f.readErr
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return f.DB.Query(ctx, sql, args...) //nolint:wrapcheck // test double
}

type errRow struct{ err error }

func (r errRow) Scan(...any) error { return r.err }

func (f *faultDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	f.mu.Lock()
	f.reads++
	f.sqls = append(f.sqls, sql)
	err := f.readErr
	f.mu.Unlock()
	if err != nil {
		return errRow{err}
	}
	return f.DB.QueryRow(ctx, sql, args...)
}

func (f *faultDB) Transact(ctx context.Context, mode db.Mode, fn func(context.Context) error) error {
	f.mu.Lock()
	f.txs++
	if mode == db.Write {
		f.writes++
	}
	err, ro := f.txErr, f.ro
	f.mu.Unlock()
	if mode == db.Write && ro {
		return errs.E("test.faultDB.Transact", errs.Unavailable, db.ErrReadOnly)
	}
	if err != nil {
		return err
	}
	return f.DB.Transact(ctx, mode, fn) //nolint:wrapcheck // test double
}

var (
	errDown    = errs.E("test.down", errs.Unavailable, errors.New("database down"))
	errTimeout = errs.E("test.timeout", errs.Timeout, errors.New("database too slow"))
)
