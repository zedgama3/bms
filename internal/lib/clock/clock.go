// Package clock is the only way production code reads the time or waits for it. Real wraps
// the standard library; Fake is a deterministic clock for tests so they never sleep.
// It depends only on the standard library and lib/errs.
// Ported from Sojourn (D11).
package clock

import (
	"context"
	"errors"
	"time"

	"github.com/zedgama3/bms/internal/lib/errs"
)

// Clock is the source of time for all production code. Real() returns the system clock;
// NewFake returns a controllable one for tests.
type Clock interface {
	// Now returns the current time. It cannot fail.
	Now() time.Time
	// Since returns Now().Sub(t). It cannot fail.
	Since(t time.Time) time.Duration
	// After returns a channel that receives the time once, after d. A non-positive d
	// fires immediately. It cannot fail. Prefer NewTimer when the caller may need to Stop.
	After(d time.Duration) <-chan time.Time
	// NewTimer returns a timer that fires once after d. A non-positive d fires at once.
	// It cannot fail.
	NewTimer(d time.Duration) Timer
	// NewTicker returns a ticker that fires every d. It fails with errs.Invalid if d <= 0.
	NewTicker(d time.Duration) (Ticker, error)
}

// Timer is a one-shot timer with time.Timer semantics (Go 1.23+: Stop and Reset guarantee
// no stale value is received afterwards).
type Timer interface {
	// C returns the channel the timer fires on.
	C() <-chan time.Time
	// Stop prevents the timer from firing. Reports whether it was still pending.
	Stop() bool
	// Reset re-arms the timer to fire after d. Reports whether it was still pending.
	Reset(d time.Duration) bool
}

// Ticker fires repeatedly with time.Ticker semantics: its channel holds one tick, and
// ticks that arrive while it is full are dropped.
type Ticker interface {
	// C returns the channel the ticker fires on. It is never closed.
	C() <-chan time.Time
	// Stop stops the ticker. Idempotent. The channel is never closed.
	Stop()
	// Reset changes the period and restarts the ticker. Fails with errs.Invalid if d <= 0.
	Reset(d time.Duration) error
}

var errNonPositive = errors.New("duration must be positive")

// Wait blocks for d or until ctx is done, whichever is first. It returns nil after d,
// errs.Canceled if ctx was canceled, errs.Timeout if ctx's deadline passed, and
// errs.Invalid if c is nil. A non-positive d returns nil immediately (after checking ctx).
// The timer it creates is always stopped on return.
func Wait(ctx context.Context, c Clock, d time.Duration) error {
	const op = "clock.Wait"
	if c == nil {
		return errs.E(op, errs.Invalid, errors.New("nil clock"))
	}
	if err := ctx.Err(); err != nil {
		return errs.E(op, errs.KindOf(err), err)
	}
	if d <= 0 {
		return nil
	}
	t := c.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C():
		return nil
	case <-ctx.Done():
		err := ctx.Err()
		return errs.E(op, errs.KindOf(err), err)
	}
}
