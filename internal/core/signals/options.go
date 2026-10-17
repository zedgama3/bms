package signals

import (
	"log/slog"
	"time"

	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// Options is what New needs. app builds it once per process.
type Options struct {
	Logger *slog.Logger     // component=signals; never nil
	Tracer telemetry.Tracer // scope "signals"; never nil
	Meter  telemetry.Meter  // scope "signals"; never nil
	Clock  clock.Clock      // Signal.At and handler durations; never nil
	// HandlerTimeout is the deadline each handler gets. 100ms..1m. app passes 5s
	// (Settings). A handler that needs longer registers a job instead.
	HandlerTimeout time.Duration
	// Deferrer receives a delivery that failed with a retryable kind so it can be tried
	// again later (jobs, M4). nil means a failure is final and is logged as such. It is
	// optional in every milestone: the dispatcher works without it.
	Deferrer Deferrer
}

// Bounds of Options.HandlerTimeout.
const (
	minHandlerTimeout = 100 * time.Millisecond
	maxHandlerTimeout = time.Minute
)

// Validate fails with errs.Invalid joining one validate.FieldError per problem, fields
// "signals.logger", "signals.tracer", "signals.meter", "signals.clock",
// "signals.handler_timeout" (F7).
func (o Options) Validate() error {
	acc := validate.Errors{Op: "signals.Options.Validate"}
	if o.Logger == nil {
		acc.Addf("signals.logger", validate.CodeRequired, "must not be nil")
	}
	if o.Tracer == nil {
		acc.Addf("signals.tracer", validate.CodeRequired, "must not be nil")
	}
	if o.Meter == nil {
		acc.Addf("signals.meter", validate.CodeRequired, "must not be nil")
	}
	if o.Clock == nil {
		acc.Addf("signals.clock", validate.CodeRequired, "must not be nil")
	}
	acc.Add(validate.Range("signals.handler_timeout", o.HandlerTimeout, minHandlerTimeout, maxHandlerTimeout))
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}
