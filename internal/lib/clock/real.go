package clock

import (
	"time"

	"github.com/zedgama3/bms/internal/lib/errs"
)

// Real returns the system clock. Safe for concurrent use. Cannot fail.
func Real() Clock { return realClock{} }

type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) Since(t time.Time) time.Duration        { return time.Since(t) }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

func (realClock) NewTimer(d time.Duration) Timer {
	return realTimer{t: time.NewTimer(d)}
}

func (realClock) NewTicker(d time.Duration) (Ticker, error) {
	if d <= 0 {
		return nil, errs.E("clock.Real.NewTicker", errs.Invalid, errNonPositive)
	}
	return realTicker{t: time.NewTicker(d)}, nil
}

type realTimer struct{ t *time.Timer }

func (r realTimer) C() <-chan time.Time        { return r.t.C }
func (r realTimer) Stop() bool                 { return r.t.Stop() }
func (r realTimer) Reset(d time.Duration) bool { return r.t.Reset(d) }

type realTicker struct{ t *time.Ticker }

func (r realTicker) C() <-chan time.Time { return r.t.C }
func (r realTicker) Stop()               { r.t.Stop() }

func (r realTicker) Reset(d time.Duration) error {
	if d <= 0 {
		return errs.E("clock.Real.Ticker.Reset", errs.Invalid, errNonPositive)
	}
	r.t.Reset(d)
	return nil
}
