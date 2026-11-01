package jobs

import (
	"context"
	"fmt"
	"math/rand/v2"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/jobs/postgres"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// DeferrerOptions configure NewDeferrer.
type DeferrerOptions struct {
	DB    db.DB
	Clock clock.Clock
	// Instance is config Instance.Name, checked by the same rule as Options.Instance so a
	// misconfigured instance fails at build on both paths. The row does not carry it: the
	// runner records the instance when it leases the row.
	Instance string
}

// Validate fails errs.Invalid joining one validate.FieldError per problem: a nil dependency
// (fields jobs.db, jobs.clock) and jobs.instance empty or malformed.
func (o DeferrerOptions) Validate() error {
	v := validate.Errors{Op: "jobs.DeferrerOptions.Validate"}
	if o.DB == nil {
		v.Addf("jobs.db", codeRequired, "must be set")
	}
	if o.Clock == nil {
		v.Addf("jobs.clock", codeRequired, "must be set")
	}
	if len(o.Instance) == 0 || len(o.Instance) > maxInstanceBytes || !instanceRE.MatchString(o.Instance) {
		v.Addf("jobs.instance", codeMalformed, "must be 1 to %d characters of lower-case letters, digits and hyphens", maxInstanceBytes)
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// Deferrer is signals' Deferrer over core.job alone. It needs neither the runner nor the
// dispatcher, so it is built below the dispatcher (boundaries.md decision 2). Safe for
// concurrent use.
type Deferrer struct {
	store store
	db    db.DB
	clock clock.Clock
}

var _ signals.Deferrer = (*Deferrer)(nil)

// NewDeferrer validates o and builds the Deferrer over postgres.Store. No I/O: it works
// against db.None() and on a read-only copy. Fails errs.Invalid for a nil dependency or a
// malformed instance name.
func NewDeferrer(o DeferrerOptions) (*Deferrer, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	st, err := postgres.NewStore(o.DB)
	if err != nil {
		return nil, fmt.Errorf("jobs.NewDeferrer: %w", err)
	}
	return &Deferrer{store: st, db: o.DB, clock: o.Clock}, nil
}

// Defer stores d as a signals.redeliver job, the row Runner.Defer writes (redeliverRow): one
// insert, never blocking on a worker. Fails errs.Invalid for an invalid d; errs.Unavailable
// wrapping db.ErrReadOnly on a read-only copy; store kinds. A repeated Defer of the same
// (signal, subscriber) is a no-op.
func (f *Deferrer) Defer(ctx context.Context, d signals.Delivery) error {
	const op = "jobs.Deferrer.Defer"
	if err := d.Validate(); err != nil {
		return wrap(op, err)
	}
	if f.db.ReadOnly() {
		return readOnlyErr(op)
	}
	now := f.clock.Now().UTC()
	trace, _ := telemetry.TraceIDFrom(ctx)
	n, err := redeliverRow(d, now, DefaultRetry, rand.Float64(), trace) //nolint:gosec // backoff jitter, not security
	if err != nil {
		return wrap(op, err)
	}
	_, err = f.store.Defer(ctx, n, now)
	return wrap(op, err)
}
