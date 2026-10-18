package app

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/config"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// requireSessionKey fails with errs.Invalid under BMS_SESSION_KEY when no session key is
// configured and the secrets file lacks one (F6; config.md F22b).
func requireSessionKey(cfg config.Config) error {
	if cfg.Session.Key.IsSet() {
		if cfg.Session.Key.Value() == placeholderSessionKey {
			return errs.E("app.requireSessionKey", errs.Invalid, &validate.FieldError{
				Field: "session.key", Code: "placeholder",
				Reason: "BMS_SESSION_KEY holds the documentation placeholder, which anyone can read in the source; unset it and run `bms migrate` to generate a key, or set one of your own",
			})
		}
		return nil
	}
	acc := validate.Errors{Op: "app.requireSessionKey"}
	acc.Addf(config.Prefix+"SESSION_KEY", validate.CodeRequired,
		"no session key: run `bms migrate` on a new installation, or restore %s from your backup", cfg.Secrets.File)
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// debugWarning is what an operator reads whenever the log level is debug: debug logs may
// hold personal data (owner, 2026-10-17). Scrubbing and redaction stay on at every level.
const debugWarning = "debug logging is on: logs may contain personal data; do not run this way in production"

// debugLogging reports whether cfg sets the log level to debug or lower.
func debugLogging(cfg config.Config) bool { return cfg.Log.Level <= slog.LevelDebug }

// warnDebugToStderr prints debugWarning to stderr when the log level is debug, for the
// commands that do not log it themselves (serve logs it; check also reports it in its
// configuration section).
func (p *process) warnDebugToStderr(cfg config.Config) {
	if debugLogging(cfg) {
		sayln(p.stderr, debugWarning)
	}
}

// moduleNames returns the enabled modules' names in build order.
func (a *assembly) moduleNames() []string {
	order := a.plan.Order()
	names := make([]string, len(order))
	for i, m := range order {
		names[i] = m.Name
	}
	return names
}

// runServe builds, starts, serves and stops the server ("serve, step by step").
func runServe(ctx context.Context, p *process) int {
	cfg, err := p.loadConfig(ctx)
	if err != nil {
		return p.earlyFail(ctx, err, ExitUsage)
	}
	a := newAssembly(p, cfg)
	if err := requireSessionKey(cfg); err != nil {
		p.problem(err)
		return ExitUsage
	}
	if err := a.startTelemetry(ctx, p.lookup, p.stdout); err != nil {
		return p.earlyFail(ctx, err, ExitUsage)
	}
	a.logger.InfoContext(ctx, "bms starting",
		slog.String("version", a.version),
		slog.String("go_version", runtime.Version()),
		slog.Bool("exporting", a.tel.Exporting()),
		slog.Any("config", cfg))
	if debugLogging(cfg) {
		a.logger.WarnContext(ctx, debugWarning, slog.String("log.level", "debug"))
	}

	// bail reports err, stops what started, and returns the exit status.
	bail := func(err error, status int) int {
		code := a.fail(ctx, p, err, status)
		a.unwind(ctx)
		return code
	}

	enabled, err := enabledModules(p.registry, cfg.Modules.Disabled)
	if err != nil {
		return bail(err, ExitUsage)
	}
	if err := a.resolve(ctx, p.registry, enabled); err != nil {
		return bail(err, ExitUsage)
	}
	if err := a.openDatabase(ctx); err != nil {
		return bail(err, statusOf(err, ExitFailure))
	}
	if err := a.newDispatcher(); err != nil {
		return bail(err, ExitFailure)
	}
	if err := a.buildModules(ctx, a.migrator); err != nil {
		return bail(err, statusOf(err, ExitSchema))
	}
	a.loadUI(ctx)

	opts, err := a.apiOptions(a.version, a.pool, func(ctx context.Context) ([]db.Status, error) {
		return a.migrator.Status(ctx, a.setsFor(a.plan.Order()))
	})
	if err != nil {
		return bail(err, ExitFailure)
	}
	srv, err := newServer(opts)
	if err != nil {
		return bail(fmt.Errorf("api: %w", err), statusOf(err, ExitUsage))
	}
	a.srv = srv

	// A module that fails to start is a runtime failure (F11); a runnable that finds the
	// operator's input unusable (the audit spool under BMS_DATA_DIR, F33) is exit 2.
	sctx, cancel := context.WithTimeout(ctx, startTimeout)
	status := ExitFailure
	err = a.set.Start(sctx)
	if err == nil {
		if err = a.startRunnables(sctx); err != nil {
			status = statusOf(err, ExitFailure)
		}
	}
	cancel()
	if err != nil {
		return bail(err, status)
	}

	lctx, cancel := context.WithTimeout(ctx, listenTimeout)
	addr, err := srv.Listen(lctx)
	cancel()
	if err != nil {
		return bail(err, statusOf(err, ExitFailure))
	}
	a.logger.InfoContext(ctx, "listening",
		slog.String("addr", addr.String()),
		slog.String("modules", strings.Join(a.moduleNames(), ",")),
		slog.Bool("read_only", a.pool.ReadOnly()),
		slog.String("ui", a.uiLabel()),
		slog.String("ui_version", a.uiVersion))

	return a.serve(ctx, p, srv)
}

// serve waits for a signal or a serving failure, then stops everything in order (steps 13
// to 15).
func (a *assembly) serve(ctx context.Context, p *process, srv httpServer) int {
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve() }() // exits when Shutdown closes the listener; its result is read below

	var serveErr error
	reason := "signal"
	select {
	case <-ctx.Done():
	case serveErr = <-errc:
		reason = "serve_error"
		a.logger.ErrorContext(ctx, "the server stopped accepting connections", slog.String("error.kind", errs.KindOf(serveErr).String()))
		p.problem(serveErr)
	}
	a.logger.InfoContext(ctx, "stopping", slog.String("reason", reason))

	began := a.clock.Now()
	shutErr := shutdown(ctx, a.logger, a.clock, a.cfg.HTTP.ShutdownTimeout, a.closers())
	if reason == "signal" {
		serveErr = <-errc // Shutdown has begun, so Serve has returned or returns at once
	}
	a.logger.InfoContext(ctx, "stopped", slog.Int64("duration_ms", durationMS(a.clock.Since(began))))

	if shutErr != nil {
		p.problem(shutErr)
	}
	if serveErr != nil {
		if reason == "signal" {
			p.problem(serveErr)
		}
		return ExitFailure
	}
	if shutErr != nil {
		return ExitFailure
	}
	return ExitOK
}

func durationMS(d time.Duration) int64 { return d.Milliseconds() }
