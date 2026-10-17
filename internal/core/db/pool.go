// Package db is the one place the process reaches PostgreSQL: the pool, statements with
// deadlines, transactions, driver-error classification, read-only detection and the
// migration runner (D8, D17). It imports pgx; nothing above the storage seam does.
package db

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/retry"
)

// Constants pinned by tests (db.md "Settings").
const (
	probeInterval = 15 * time.Second
	probeTimeout  = 5 * time.Second
	appName       = "bms"
)

// connectPolicy is the schedule of Open's connect attempts, bounded by ConnectTimeout.
var connectPolicy = retry.Policy{Initial: 500 * time.Millisecond, Max: 5 * time.Second, Multiplier: 2, MaxAttempts: 0}

// noJitter makes the connect schedule exactly 500ms, 1s, 2s, 4s, 5s, 5s...
func noJitter() float64 { return 1 }

// probeResult is what the probe statement returns.
type probeResult struct {
	version  int
	readOnly bool
}

// probeFunc runs the probe statement. Errors are the driver's, unclassified.
type probeFunc func(ctx context.Context) (probeResult, error)

// Pool is the process's connection pool. It implements DB. Safe for concurrent use from
// Open until Close returns.
type Pool struct {
	pool   *pgxpool.Pool
	opts   Options
	clock  clock.Clock
	logger *slog.Logger
	probe  probeFunc

	readOnly atomic.Bool
	closed   atomic.Bool
	pending  atomic.Int64

	cancelWatcher context.CancelFunc
	watcherDone   chan struct{}

	closeMu    sync.Mutex
	closeBegun bool
	poolClosed chan struct{}
}

// Stats is a snapshot of the pool, copied from pgxpool.Stat so no caller sees pgx types.
type Stats struct {
	Acquired     int           // connections in use
	Idle         int           // open connections not in use
	Total        int           // Acquired + Idle + connections being opened
	Max          int           // Options.MaxConns
	Pending      int           // callers waiting to acquire
	WaitDuration time.Duration // total time callers have spent waiting since Open
}

// newPool builds a Pool over an unconnected pgxpool. The watcher is not started.
func newPool(pool *pgxpool.Pool, opts Options, clk clock.Clock, logger *slog.Logger) *Pool {
	p := &Pool{
		pool:        pool,
		opts:        opts,
		clock:       clk,
		logger:      logger,
		watcherDone: make(chan struct{}),
		poolClosed:  make(chan struct{}),
	}
	p.probe = p.pgProbe
	return p
}

// pgProbe runs the probe statement on a pooled connection.
func (p *Pool) pgProbe(ctx context.Context) (probeResult, error) {
	conn, err := p.acquire(ctx)
	if err != nil {
		return probeResult{}, err
	}
	defer conn.Release()
	var r probeResult
	if err := conn.QueryRow(ctx, sqlReadonlyProbe).Scan(&r.version, &r.readOnly); err != nil {
		return probeResult{}, err //nolint:wrapcheck // classified by the caller
	}
	return r, nil
}

// Stats returns the pool's current counters. Cannot fail: it reads pgxpool's own counters
// and after Close returns zeros. Safe for concurrent use.
func (p *Pool) Stats() Stats {
	if p.closed.Load() {
		return Stats{}
	}
	s := p.pool.Stat()
	return Stats{
		Acquired:     int(s.AcquiredConns()),
		Idle:         int(s.IdleConns()),
		Total:        int(s.TotalConns()),
		Max:          p.opts.MaxConns,
		Pending:      int(p.pending.Load()),
		WaitDuration: s.EmptyAcquireWaitTime(),
	}
}

// ReadOnly reports whether this copy may not write: the server is in recovery (a replica)
// or its default transaction mode is read-only (decision 7). A cached value; cannot fail.
func (p *Pool) ReadOnly() bool { return p.readOnly.Load() }

// setReadOnly stores the state and logs, at warn, a change.
func (p *Pool) setReadOnly(ctx context.Context, ro bool) {
	if p.readOnly.Swap(ro) == ro {
		return
	}
	// A session keeps the default_transaction_read_only it started with; replace the
	// pooled ones so every statement after a switchover sees the new state.
	if p.pool != nil {
		p.pool.Reset()
	}
	if ro {
		p.logger.WarnContext(ctx, "this copy of the database is now read-only")
		return
	}
	p.logger.WarnContext(ctx, "this copy of the database is writable again")
}

// Ping checks that the server answers and refreshes the read-only state in the same round
// trip, for /readyz. Fails with errs.Unavailable when the server does not answer (F28),
// errs.Canceled or errs.Timeout when ctx ends (F29).
func (p *Pool) Ping(ctx context.Context) error {
	const op = "db.Pool.Ping"
	if err := p.usable(op); err != nil {
		return err
	}
	dctx, cancel := p.deadline(ctx)
	defer cancel()
	res, err := p.probe(dctx)
	if err != nil {
		return p.fail(ctx, op, err)
	}
	p.setReadOnly(ctx, res.readOnly)
	return nil
}

// Close stops the read-only watcher, waits for it, and closes the pool, which waits for
// acquired connections to be released. Idempotent: a second Close returns nil once the
// first finished. Fails with errs.Timeout (or errs.Canceled) if ctx ends before the
// watcher stopped or the pool closed (F30); it returns then, never later, even if a late
// caller still holds a connection: the pool keeps closing in the background, the held
// connection is closed when released, and every call on the Pool after Close returned
// fails with errs.Unavailable (F20).
func (p *Pool) Close(ctx context.Context) error {
	const op = "db.Pool.Close"
	p.closeMu.Lock()
	if !p.closeBegun {
		p.closeBegun = true
		p.closed.Store(true)
		if p.cancelWatcher != nil {
			p.cancelWatcher()
		} else {
			close(p.watcherDone)
		}
		go func() { // exits when the pool has closed; Close waits for it under ctx
			<-p.watcherDone
			p.pool.Close()
			close(p.poolClosed)
		}()
	}
	p.closeMu.Unlock()

	select {
	case <-p.poolClosed:
		return nil
	case <-ctx.Done():
		return errs.E(op, kindOfContext(ctx.Err()), fmt.Errorf("closing the pool: %w", ctx.Err()))
	}
}

// kindOfContext maps a context error to Timeout or Canceled.
func kindOfContext(err error) errs.Kind {
	if errors.Is(err, context.DeadlineExceeded) {
		return errs.Timeout
	}
	return errs.Canceled
}

// usable fails with errs.Unavailable once Close has begun.
func (p *Pool) usable(op string) error {
	if p.closed.Load() {
		return errs.E(op, errs.Unavailable, errors.New("the database pool is closed"))
	}
	return nil
}

// watch refreshes the read-only state on every tick until ctx ends. A probe that fails
// keeps the state and logs once until the next success.
func (p *Pool) watch(ctx context.Context, tick clock.Ticker) {
	defer close(p.watcherDone)
	defer tick.Stop()
	failing := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C():
		}
		pctx, cancel := context.WithTimeout(ctx, probeTimeout)
		res, err := p.probe(pctx)
		cancel()
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			if !failing {
				failing = true
				p.logger.WarnContext(ctx, "the read-only probe failed; keeping the last known state",
					slog.Any("error", Wrap("db.watch", err)))
			}
			continue
		}
		failing = false
		p.setReadOnly(ctx, res.readOnly)
	}
}
