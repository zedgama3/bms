package clock

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/zedgama3/bms/internal/lib/errs"
)

// waiter is one armed timer, ticker or pending After channel.
type waiter struct {
	ch       chan time.Time
	deadline time.Time
	seq      uint64
	period   time.Duration // 0 for a one-shot
	armed    bool
}

// Fake is a Clock that moves only when Advance is called. Safe for concurrent use. The zero
// value is usable (time starts at the zero Time); NewFake sets the start.
type Fake struct {
	mu      sync.Mutex
	now     time.Time
	waiters []*waiter
	seq     uint64
	changed chan struct{} // created lazily by BlockUntil; closed when the waiter count changes
}

var _ Clock = (*Fake)(nil)

// NewFake returns a Fake whose Now() is start. Cannot fail (a zero start is allowed).
func NewFake(start time.Time) *Fake {
	return &Fake{now: start}
}

// Now returns the fake time.
func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// Since returns Now().Sub(t).
func (f *Fake) Since(t time.Time) time.Duration {
	return f.Now().Sub(t)
}

// After returns a channel that receives the fake time once, after d. A non-positive d
// fires immediately.
func (f *Fake) After(d time.Duration) <-chan time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.armLocked(d, 0).ch
}

// NewTimer returns a timer that fires once after d on the fake time. A non-positive d
// fires at once.
func (f *Fake) NewTimer(d time.Duration) Timer {
	f.mu.Lock()
	defer f.mu.Unlock()
	return &fakeTimer{f: f, w: f.armLocked(d, 0)}
}

// NewTicker returns a ticker that fires every d on the fake time. It fails with
// errs.Invalid if d <= 0.
func (f *Fake) NewTicker(d time.Duration) (Ticker, error) {
	if d <= 0 {
		return nil, errs.E("clock.Fake.NewTicker", errs.Invalid, errNonPositive)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return &fakeTicker{f: f, w: f.armLocked(d, d)}, nil
}

// Advance moves Now forward by d, firing every timer and ticker whose deadline is <= the
// new time, in deadline order (ties by creation order). Each channel receives its own
// deadline. Delivery is a non-blocking send, so a full channel drops the value. Fails with
// errs.Invalid if d < 0.
func (f *Fake) Advance(d time.Duration) error {
	if d < 0 {
		return errs.E("clock.Fake.Advance", errs.Invalid, errors.New("negative duration"))
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	target := f.now.Add(d)
	for {
		w := f.nextDueLocked(target)
		if w == nil {
			break
		}
		f.now = w.deadline
		select {
		case w.ch <- w.deadline:
		default:
		}
		if w.period > 0 {
			w.deadline = w.deadline.Add(w.period)
		} else {
			f.removeLocked(w)
		}
	}
	f.now = target
	return nil
}

// BlockUntil returns once exactly n waiters are armed (unfired timers, running tickers,
// pending After channels), or fails with errs.Canceled / errs.Timeout when ctx ends first,
// or errs.Invalid if n < 0.
func (f *Fake) BlockUntil(ctx context.Context, n int) error {
	const op = "clock.Fake.BlockUntil"
	if n < 0 {
		return errs.E(op, errs.Invalid, errors.New("negative waiter count"))
	}
	for {
		f.mu.Lock()
		if len(f.waiters) == n {
			f.mu.Unlock()
			return nil
		}
		if f.changed == nil {
			f.changed = make(chan struct{})
		}
		changed := f.changed
		f.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			err := ctx.Err()
			return errs.E(op, errs.KindOf(err), err)
		}
	}
}

// armLocked creates a waiter that fires after d (period 0 means one-shot). A non-positive
// d on a one-shot fills the channel at once and leaves the waiter unarmed.
func (f *Fake) armLocked(d, period time.Duration) *waiter {
	w := &waiter{ch: make(chan time.Time, 1), period: period}
	if d <= 0 {
		w.ch <- f.now
		return w
	}
	f.addLocked(w, d)
	return w
}

func (f *Fake) addLocked(w *waiter, d time.Duration) {
	f.seq++
	w.seq = f.seq
	w.deadline = f.now.Add(d)
	w.armed = true
	f.waiters = append(f.waiters, w)
	f.notifyLocked()
}

func (f *Fake) removeLocked(w *waiter) bool {
	if !w.armed {
		return false
	}
	w.armed = false
	for i, x := range f.waiters {
		if x == w {
			f.waiters = append(f.waiters[:i], f.waiters[i+1:]...)
			break
		}
	}
	f.notifyLocked()
	return true
}

func (f *Fake) nextDueLocked(target time.Time) *waiter {
	var best *waiter
	for _, w := range f.waiters {
		if w.deadline.After(target) {
			continue
		}
		if best == nil || w.deadline.Before(best.deadline) ||
			(w.deadline.Equal(best.deadline) && w.seq < best.seq) {
			best = w
		}
	}
	return best
}

func (f *Fake) notifyLocked() {
	if f.changed != nil {
		close(f.changed)
		f.changed = nil
	}
}

func drain(ch chan time.Time) {
	select {
	case <-ch:
	default:
	}
}

type fakeTimer struct {
	f *Fake
	w *waiter
}

func (t *fakeTimer) C() <-chan time.Time { return t.w.ch }

func (t *fakeTimer) Stop() bool {
	t.f.mu.Lock()
	defer t.f.mu.Unlock()
	was := t.f.removeLocked(t.w)
	drain(t.w.ch)
	return was
}

func (t *fakeTimer) Reset(d time.Duration) bool {
	t.f.mu.Lock()
	defer t.f.mu.Unlock()
	was := t.f.removeLocked(t.w)
	drain(t.w.ch)
	if d <= 0 {
		t.w.ch <- t.f.now
		return was
	}
	t.f.addLocked(t.w, d)
	return was
}

type fakeTicker struct {
	f *Fake
	w *waiter
}

func (t *fakeTicker) C() <-chan time.Time { return t.w.ch }

func (t *fakeTicker) Stop() {
	t.f.mu.Lock()
	defer t.f.mu.Unlock()
	t.f.removeLocked(t.w)
	drain(t.w.ch)
}

func (t *fakeTicker) Reset(d time.Duration) error {
	if d <= 0 {
		return errs.E("clock.Fake.Ticker.Reset", errs.Invalid, errNonPositive)
	}
	t.f.mu.Lock()
	defer t.f.mu.Unlock()
	t.f.removeLocked(t.w)
	drain(t.w.ch)
	t.w.period = d
	t.f.addLocked(t.w, d)
	return nil
}
