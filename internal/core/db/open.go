package db

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/retry"
)

// Open validates opts, parses the URL, connects with retries until ConnectTimeout, checks
// the server version and probes the read-only state, then starts the read-only watcher.
// It fails with errs.Invalid for bad options, a URL the driver rejects, bad credentials, a
// missing database or an old server (none retried); errs.Unavailable when the server could
// not be reached within ConnectTimeout; errs.Canceled or errs.Timeout when ctx ends first.
// The caller owns the Pool and must Close it.
//
// pgx also reads the PG* environment variables and ~/.pgpass for what the URL leaves
// unset (decision 14).
func Open(ctx context.Context, opts Options, clk clock.Clock, logger *slog.Logger) (*Pool, error) {
	return open(ctx, opts, clk, logger, nil)
}

// open is Open with the probe replaceable, so unit tests need no server.
func open(ctx context.Context, opts Options, clk clock.Clock, logger *slog.Logger, probe probeFunc) (*Pool, error) {
	const op = "db.Open"
	if err := opts.Validate(); err != nil {
		return nil, errs.E(op, errs.Invalid, err)
	}
	if clk == nil || logger == nil {
		return nil, errs.E(op, errs.Invalid, errors.New("nil clock or logger"))
	}
	cfg, err := pgxpool.ParseConfig(opts.URL.Value())
	if err != nil { // the driver's text can echo the URL, so it is dropped
		return nil, errs.E(op, errs.Invalid,
			errors.New("the driver rejected db.url (check scheme, host, port and parameters)"))
	}
	cfg.MaxConns = int32(opts.MaxConns) //nolint:gosec // validated to 1..1000
	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	cfg.ConnConfig.RuntimeParams["application_name"] = appName
	if opts.Tracer != nil {
		cfg.ConnConfig.Tracer = newQueryTracer(opts.Tracer)
	}
	pgPool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, Wrap(op, err)
	}
	p := newPool(pgPool, opts, clk, logger)
	if probe != nil {
		p.probe = probe
	}

	res, err := p.connect(ctx)
	if err != nil {
		p.pool.Close()
		return nil, err
	}
	p.readOnly.Store(res.readOnly)

	if opts.Meter != nil {
		if err := registerGauges(opts.Meter, p.Stats, p.closed.Load); err != nil {
			p.closed.Store(true) // gauges registered before the failure stop observing
			p.pool.Close()
			return nil, fmt.Errorf("%s: %w", op, err)
		}
	}
	tick, err := clk.NewTicker(probeInterval)
	if err != nil {
		p.closed.Store(true)
		p.pool.Close()
		return nil, errs.E(op, errs.Internal, err)
	}
	logger.InfoContext(ctx, "database connected",
		slog.String("host", cfg.ConnConfig.Host),
		slog.Int("port", int(cfg.ConnConfig.Port)),
		slog.String("database", cfg.ConnConfig.Database),
		slog.String("user", cfg.ConnConfig.User),
		slog.Int("server_version", res.version),
		slog.Bool("read_only", res.readOnly))
	wctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	p.cancelWatcher = cancel
	go p.watch(wctx, tick) // exits when Close cancels wctx; Close waits for watcherDone
	return p, nil
}

// connect retries the probe under ConnectTimeout (driven by both the real deadline and
// the injected clock) and checks the server version.
func (p *Pool) connect(ctx context.Context) (probeResult, error) {
	const op = "db.Open"
	cctx, cancel := context.WithTimeout(ctx, p.opts.ConnectTimeout)
	timer := p.clock.NewTimer(p.opts.ConnectTimeout)
	var timedOut atomic.Bool
	stopped := make(chan struct{})
	go func() { // exits when the timer fires or cctx ends; waited for below
		defer close(stopped)
		select {
		case <-timer.C():
			timedOut.Store(true)
			cancel()
		case <-cctx.Done():
		}
	}()
	defer func() {
		timer.Stop()
		cancel()
		<-stopped
	}()

	var res probeResult
	err := retry.Do(cctx, p.clock, connectPolicy, noJitter, func(actx context.Context) error {
		actx, acancel := context.WithTimeout(actx, probeTimeout)
		defer acancel()
		r, perr := p.probe(actx)
		if perr != nil {
			return Wrap(op, perr)
		}
		if r.version < MinServerVersion {
			return errs.E(op, errs.Invalid, fmt.Errorf(
				"PostgreSQL server version %d is older than the minimum %d", r.version, MinServerVersion))
		}
		res = r
		return nil
	})
	if err == nil {
		return res, nil
	}
	switch {
	case ctx.Err() != nil:
		return probeResult{}, errs.E(op, kindOfContext(ctx.Err()), err)
	case errs.Is(err, errs.Invalid):
		return probeResult{}, errs.E(op, errs.Invalid, err)
	case timedOut.Load() || cctx.Err() != nil:
		return probeResult{}, errs.E(op, errs.Unavailable,
			fmt.Errorf("could not reach the database within db.connect_timeout: %w", err))
	default:
		return probeResult{}, errs.E(op, errs.KindOf(err), err)
	}
}
