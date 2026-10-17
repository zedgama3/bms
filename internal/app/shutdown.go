package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// httpMinShare is the least time the HTTP server gets to drain, whatever the budget.
const httpMinShare = time.Second

// closer is one shutdown step. shutdown runs them in order within budget: the first
// (HTTP) under max(1s, 2*budget/3), every other under the shared deadline start+budget;
// each is called even after an earlier failure or an exhausted deadline (every closer is
// idempotent and returns at once on a done ctx); errors are joined and returned (F15,
// F16). ctx is detached from cancellation (context.WithoutCancel): the signal that
// started the shutdown must not cut it short. Consumers: serve; the lifecycle tests.
type closer struct {
	name  string
	close func(ctx context.Context) error
}

// shutdown runs steps in order and returns every failure joined (nil when all succeeded).
// Each step is logged as "step stopped" (at error level, with its error kind, when it
// failed) with its name and its duration in milliseconds. It never skips a step.
//
// Durations come from clk (the share of each step is computed from how long the shutdown
// has run by clk), but they are applied with context.WithTimeout on the wall clock, so a
// fake clock can neither produce an already-dead context nor one centuries away.
func shutdown(ctx context.Context, logger *slog.Logger, clk clock.Clock, budget time.Duration, steps []closer) error {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	base := context.WithoutCancel(ctx)
	start := clk.Now()
	var failed []error
	for i, s := range steps {
		share := budget - clk.Since(start) // what is left of the shared deadline start+budget
		if i == 0 {
			share = max(httpMinShare, 2*budget/3)
		}
		sctx, cancel := context.WithTimeout(base, share) // a share <= 0 gives a done ctx: the closer still runs
		began := clk.Now()
		err := s.close(sctx)
		elapsed := clk.Since(began)
		cancel()
		attrs := []slog.Attr{
			slog.String("name", s.name),
			slog.Int64("duration_ms", elapsed.Milliseconds()),
		}
		if err != nil {
			attrs = append(attrs, slog.String("error.kind", errs.KindOf(err).String()))
			logger.LogAttrs(base, slog.LevelError, "step stopped", attrs...)
			failed = append(failed, fmt.Errorf("stop %s: %w", s.name, err))
			continue
		}
		logger.LogAttrs(base, slog.LevelInfo, "step stopped", attrs...)
	}
	return errors.Join(failed...)
}
