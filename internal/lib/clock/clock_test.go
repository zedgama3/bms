package clock_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
)

var t0 = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

// harness lets one contract test drive both the real and the fake clock. step is the
// duration used for timers; advance moves a Fake and is a no-op for Real (whose timers
// fire on their own within a few milliseconds).
type harness struct {
	name    string
	clk     clock.Clock
	step    time.Duration
	advance func(t *testing.T, d time.Duration)
}

func harnesses() []harness {
	f := clock.NewFake(t0)
	return []harness{
		{
			name: "real", clk: clock.Real(), step: 5 * time.Millisecond,
			advance: func(*testing.T, time.Duration) {},
		},
		{
			name: "fake", clk: f, step: time.Second,
			advance: func(t *testing.T, d time.Duration) {
				t.Helper()
				if err := f.Advance(d); err != nil {
					t.Fatalf("Advance(%v): %v", d, err)
				}
			},
		},
	}
}

// recv waits for a value on ch, bounded by a deadline so a bug fails instead of hanging.
func recv(t *testing.T, ch <-chan time.Time) time.Time {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	select {
	case v := <-ch:
		return v
	case <-ctx.Done():
		t.Fatal("timed out waiting for a value")
		return time.Time{}
	}
}

func empty(t *testing.T, ch <-chan time.Time) {
	t.Helper()
	select {
	case v := <-ch:
		t.Fatalf("unexpected value %v", v)
	default:
	}
}

func bctx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// ---- shared contract (Real and Fake) ----

func TestNewTicker_NonPositive_ReturnsInvalid(t *testing.T) {
	for _, h := range harnesses() {
		for _, d := range []time.Duration{0, -1, -time.Hour} {
			t.Run(h.name+"/"+d.String(), func(t *testing.T) {
				tk, err := h.clk.NewTicker(d)
				if !errs.Is(err, errs.Invalid) {
					t.Fatalf("err = %v, want Invalid", err)
				}
				if tk != nil {
					t.Errorf("ticker = %v, want nil on error", tk)
				}
			})
		}
	}
}

func TestTickerReset_NonPositive_ReturnsInvalid(t *testing.T) {
	for _, h := range harnesses() {
		for _, d := range []time.Duration{0, -1} {
			t.Run(h.name+"/"+d.String(), func(t *testing.T) {
				tk, err := h.clk.NewTicker(h.step)
				if err != nil {
					t.Fatal(err)
				}
				defer tk.Stop()
				if err := tk.Reset(d); !errs.Is(err, errs.Invalid) {
					t.Fatalf("Reset(%v) err = %v, want Invalid", d, err)
				}
			})
		}
	}
}

func TestContract_TimerFiresOnce(t *testing.T) {
	for _, h := range harnesses() {
		t.Run(h.name, func(t *testing.T) {
			tm := h.clk.NewTimer(h.step)
			h.advance(t, h.step)
			recv(t, tm.C())
			h.advance(t, h.step)
			empty(t, tm.C())
		})
	}
}

func TestContract_TimerNonPositiveFiresAtOnce(t *testing.T) {
	for _, h := range harnesses() {
		for _, d := range []time.Duration{0, -time.Second} {
			t.Run(h.name+"/"+d.String(), func(t *testing.T) {
				recv(t, h.clk.NewTimer(d).C())
				recv(t, h.clk.After(d))
			})
		}
	}
}

func TestContract_AfterFires(t *testing.T) {
	for _, h := range harnesses() {
		t.Run(h.name, func(t *testing.T) {
			ch := h.clk.After(h.step)
			h.advance(t, h.step)
			recv(t, ch)
		})
	}
}

func TestContract_TimerStopReportsPending(t *testing.T) {
	for _, h := range harnesses() {
		t.Run(h.name, func(t *testing.T) {
			tm := h.clk.NewTimer(time.Hour)
			if !tm.Stop() {
				t.Error("first Stop = false, want true (was pending)")
			}
			if tm.Stop() {
				t.Error("second Stop = true, want false")
			}
			empty(t, tm.C())
		})
	}
}

func TestContract_TimerStopAfterFire_ReturnsFalse(t *testing.T) {
	for _, h := range harnesses() {
		t.Run(h.name, func(t *testing.T) {
			tm := h.clk.NewTimer(h.step)
			h.advance(t, h.step)
			recv(t, tm.C())
			if tm.Stop() {
				t.Error("Stop after fire = true, want false")
			}
		})
	}
}

func TestContract_TimerReset(t *testing.T) {
	for _, h := range harnesses() {
		t.Run(h.name, func(t *testing.T) {
			tm := h.clk.NewTimer(time.Hour)
			if !tm.Reset(h.step) {
				t.Error("Reset on pending = false, want true")
			}
			h.advance(t, h.step)
			recv(t, tm.C())
			if tm.Reset(h.step) {
				t.Error("Reset on fired = true, want false")
			}
			h.advance(t, h.step)
			recv(t, tm.C())
		})
	}
}

func TestContract_TickerTicksRepeatedly(t *testing.T) {
	for _, h := range harnesses() {
		t.Run(h.name, func(t *testing.T) {
			tk, err := h.clk.NewTicker(h.step)
			if err != nil {
				t.Fatal(err)
			}
			defer tk.Stop()
			for i := 0; i < 3; i++ {
				h.advance(t, h.step)
				recv(t, tk.C())
			}
		})
	}
}

func TestContract_TickerStopIdempotentChannelOpen(t *testing.T) {
	for _, h := range harnesses() {
		t.Run(h.name, func(t *testing.T) {
			tk, err := h.clk.NewTicker(h.step)
			if err != nil {
				t.Fatal(err)
			}
			tk.Stop()
			tk.Stop()
			h.advance(t, 3*h.step)
			select {
			case _, ok := <-tk.C():
				if !ok {
					t.Fatal("channel was closed; must never be closed")
				}
				t.Fatal("stopped ticker delivered a tick")
			default:
			}
		})
	}
}

func TestContract_TickerResetValid(t *testing.T) {
	for _, h := range harnesses() {
		t.Run(h.name, func(t *testing.T) {
			tk, err := h.clk.NewTicker(time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			defer tk.Stop()
			if err := tk.Reset(h.step); err != nil {
				t.Fatalf("Reset: %v", err)
			}
			h.advance(t, h.step)
			recv(t, tk.C())
		})
	}
}

func TestContract_NowAndSince(t *testing.T) {
	for _, h := range harnesses() {
		t.Run(h.name, func(t *testing.T) {
			start := h.clk.Now()
			h.advance(t, h.step)
			if got := h.clk.Since(start); got < 0 {
				t.Errorf("Since = %v, want >= 0", got)
			}
			if h.clk.Now().Before(start) {
				t.Error("Now went backwards")
			}
		})
	}
}

// ---- Wait ----

func TestWait_NilClock_ReturnsInvalid(t *testing.T) {
	if err := clock.Wait(t.Context(), nil, time.Second); !errs.Is(err, errs.Invalid) {
		t.Fatalf("err = %v, want Invalid", err)
	}
}

func TestWait_ContextCanceled_ReturnsCanceled(t *testing.T) {
	for _, h := range harnesses() {
		t.Run(h.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if err := clock.Wait(ctx, h.clk, time.Hour); !errs.Is(err, errs.Canceled) {
				t.Fatalf("err = %v, want Canceled", err)
			}
		})
	}
}

func TestWait_DeadlineExceeded_ReturnsTimeout(t *testing.T) {
	for _, h := range harnesses() {
		t.Run(h.name, func(t *testing.T) {
			ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
			defer cancel()
			if err := clock.Wait(ctx, h.clk, time.Hour); !errs.Is(err, errs.Timeout) {
				t.Fatalf("err = %v, want Timeout", err)
			}
		})
	}
}

func TestWait_DeadlineDuringWait_ReturnsTimeout(t *testing.T) {
	f := clock.NewFake(t0)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := clock.Wait(ctx, f, time.Hour); !errs.Is(err, errs.Timeout) {
		t.Fatalf("err = %v, want Timeout", err)
	}
	if err := f.BlockUntil(bctx(t), 0); err != nil {
		t.Fatalf("timer leaked after ctx won: %v", err)
	}
}

func TestWait_NonPositive_ReturnsNilButChecksCtx(t *testing.T) {
	for _, h := range harnesses() {
		t.Run(h.name, func(t *testing.T) {
			if err := clock.Wait(t.Context(), h.clk, 0); err != nil {
				t.Errorf("Wait(0) = %v, want nil", err)
			}
			if err := clock.Wait(t.Context(), h.clk, -time.Second); err != nil {
				t.Errorf("Wait(-1s) = %v, want nil", err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if err := clock.Wait(ctx, h.clk, 0); !errs.Is(err, errs.Canceled) {
				t.Errorf("Wait(0) on canceled ctx = %v, want Canceled", err)
			}
		})
	}
}

func TestWait_ElapsesOnFake(t *testing.T) {
	f := clock.NewFake(t0)
	done := make(chan error, 1)
	go func() { done <- clock.Wait(bctx(t), f, 10*time.Second) }()
	if err := f.BlockUntil(bctx(t), 1); err != nil {
		t.Fatal(err)
	}
	if err := f.Advance(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Wait = %v, want nil", err)
		}
	case <-bctx(t).Done():
		t.Fatal("Wait did not return")
	}
	if got := f.Since(t0); got < 10*time.Second {
		t.Errorf("Now advanced %v, want >= 10s", got)
	}
	if err := f.BlockUntil(bctx(t), 0); err != nil {
		t.Errorf("waiter left armed after Wait: %v", err)
	}
}

func TestWait_CancelDuringWaitOnFake_StopsTimer(t *testing.T) {
	f := clock.NewFake(t0)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- clock.Wait(ctx, f, time.Hour) }()
	if err := f.BlockUntil(bctx(t), 1); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := <-done; !errs.Is(err, errs.Canceled) {
		t.Fatalf("err = %v, want Canceled", err)
	}
	if err := f.BlockUntil(bctx(t), 0); err != nil {
		t.Errorf("waiter leaked: %v", err)
	}
}

func TestWait_RealClockShort(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	start := time.Now()
	if err := clock.Wait(ctx, clock.Real(), 5*time.Millisecond); err != nil {
		t.Fatalf("Wait = %v", err)
	}
	if time.Since(start) < 5*time.Millisecond {
		t.Error("returned before d elapsed")
	}
}

// ---- Fake ----

func TestFakeNow_StartAndZero(t *testing.T) {
	if got := clock.NewFake(t0).Now(); !got.Equal(t0) {
		t.Errorf("Now = %v, want %v", got, t0)
	}
	z := clock.NewFake(time.Time{})
	if !z.Now().IsZero() {
		t.Errorf("zero start Now = %v", z.Now())
	}
	if got := z.Since(time.Time{}); got != 0 {
		t.Errorf("Since = %v, want 0", got)
	}
}

func TestFakeAdvance_Negative_ReturnsInvalid(t *testing.T) {
	f := clock.NewFake(t0)
	if err := f.Advance(-1); !errs.Is(err, errs.Invalid) {
		t.Fatalf("err = %v, want Invalid", err)
	}
	if !f.Now().Equal(t0) {
		t.Error("Now moved on a failed Advance")
	}
	if err := f.Advance(0); err != nil {
		t.Errorf("Advance(0) = %v, want nil", err)
	}
}

func TestFakeAdvance_SetsNowExactly(t *testing.T) {
	f := clock.NewFake(t0)
	f.NewTimer(time.Second)
	for _, d := range []time.Duration{500 * time.Millisecond, 3 * time.Second, 7 * time.Nanosecond} {
		before := f.Now()
		if err := f.Advance(d); err != nil {
			t.Fatal(err)
		}
		if want := before.Add(d); !f.Now().Equal(want) {
			t.Errorf("Now = %v, want %v", f.Now(), want)
		}
	}
	if got := f.Since(t0); got != 3500*time.Millisecond+7*time.Nanosecond {
		t.Errorf("Since(start) = %v", got)
	}
}

func TestFakeAdvance_FiresInDeadlineOrder(t *testing.T) {
	f := clock.NewFake(t0)
	// Created out of deadline order; each channel must carry its own deadline.
	a := f.NewTimer(3 * time.Second)
	b := f.NewTimer(1 * time.Second)
	c := f.After(2 * time.Second)
	if err := f.Advance(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		ch   <-chan time.Time
		want time.Time
	}{
		"a": {a.C(), t0.Add(3 * time.Second)},
		"b": {b.C(), t0.Add(1 * time.Second)},
		"c": {c, t0.Add(2 * time.Second)},
	} {
		if got := recv(t, tc.ch); !got.Equal(tc.want) {
			t.Errorf("%s delivered %v, want %v", name, got, tc.want)
		}
	}
	if !f.Now().Equal(t0.Add(10 * time.Second)) {
		t.Errorf("Now = %v", f.Now())
	}
}

func TestFakeAdvance_NotDueNotFired(t *testing.T) {
	f := clock.NewFake(t0)
	tm := f.NewTimer(2 * time.Second)
	if err := f.Advance(time.Second); err != nil {
		t.Fatal(err)
	}
	empty(t, tm.C())
	if err := f.Advance(time.Second); err != nil { // deadline == target is due
		t.Fatal(err)
	}
	if got := recv(t, tm.C()); !got.Equal(t0.Add(2 * time.Second)) {
		t.Errorf("got %v", got)
	}
}

func TestFakeAdvance_TiesFireInCreationOrder(t *testing.T) {
	// Delivery order across channels is not observable once Advance returns, so assert
	// the observable part: every tied waiter fires and carries the shared deadline.
	f := clock.NewFake(t0)
	tm := f.NewTimer(time.Second)
	tk, err := f.NewTicker(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	af := f.After(time.Second)
	if err := f.Advance(time.Second); err != nil {
		t.Fatal(err)
	}
	want := t0.Add(time.Second)
	for name, ch := range map[string]<-chan time.Time{"timer": tm.C(), "ticker": tk.C(), "after": af} {
		if got := recv(t, ch); !got.Equal(want) {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
}

func TestFakeAdvance_TickerFiresRepeatedlyWhenDrained(t *testing.T) {
	f := clock.NewFake(t0)
	tk, err := f.NewTicker(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 4; i++ {
		if err := f.Advance(time.Second); err != nil {
			t.Fatal(err)
		}
		if got, want := recv(t, tk.C()), t0.Add(time.Duration(i)*time.Second); !got.Equal(want) {
			t.Errorf("tick %d = %v, want %v", i, got, want)
		}
	}
}

func TestFakeTicker_LargeAdvanceUndrained_DeliversOne(t *testing.T) {
	f := clock.NewFake(t0)
	tk, err := f.NewTicker(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Advance(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	if got := recv(t, tk.C()); !got.Equal(t0.Add(time.Second)) {
		t.Errorf("first tick = %v, want first deadline", got)
	}
	empty(t, tk.C())
	// The ticker keeps its schedule: next deadline is start+11s.
	if err := f.Advance(time.Second); err != nil {
		t.Fatal(err)
	}
	if got := recv(t, tk.C()); !got.Equal(t0.Add(11 * time.Second)) {
		t.Errorf("next tick = %v, want %v", got, t0.Add(11*time.Second))
	}
}

func TestFakeTicker_ResetDiscardsBufferedTick(t *testing.T) {
	f := clock.NewFake(t0)
	tk, err := f.NewTicker(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Advance(time.Second); err != nil { // buffered, not received
		t.Fatal(err)
	}
	if err := tk.Reset(5 * time.Second); err != nil {
		t.Fatal(err)
	}
	empty(t, tk.C())
	if err := f.Advance(4 * time.Second); err != nil {
		t.Fatal(err)
	}
	empty(t, tk.C())
	if err := f.Advance(time.Second); err != nil {
		t.Fatal(err)
	}
	if got := recv(t, tk.C()); !got.Equal(t0.Add(6 * time.Second)) {
		t.Errorf("tick = %v, want Now+5s from reset", got)
	}
}

func TestFakeTicker_StopDrainsAndRemoves(t *testing.T) {
	f := clock.NewFake(t0)
	tk, err := f.NewTicker(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Advance(time.Second); err != nil {
		t.Fatal(err)
	}
	tk.Stop()
	empty(t, tk.C())
	if err := f.BlockUntil(bctx(t), 0); err != nil {
		t.Errorf("stopped ticker still a waiter: %v", err)
	}
}

func TestFakeTimer_StopThenAdvance_NoDelivery(t *testing.T) {
	f := clock.NewFake(t0)
	tm := f.NewTimer(time.Second)
	if !tm.Stop() {
		t.Fatal("Stop = false, want true")
	}
	if err := f.Advance(time.Minute); err != nil {
		t.Fatal(err)
	}
	empty(t, tm.C())
}

func TestFakeTimer_StopDrainsFiredValue(t *testing.T) {
	f := clock.NewFake(t0)
	tm := f.NewTimer(time.Second)
	if err := f.Advance(time.Second); err != nil {
		t.Fatal(err)
	}
	if tm.Stop() {
		t.Error("Stop after fire = true, want false")
	}
	empty(t, tm.C())
}

func TestFakeTimer_ResetDrainsStaleValue(t *testing.T) {
	f := clock.NewFake(t0)
	tm := f.NewTimer(time.Second)
	if err := f.Advance(time.Second); err != nil { // fired, unreceived
		t.Fatal(err)
	}
	if tm.Reset(5 * time.Second) {
		t.Error("Reset on fired timer = true, want false")
	}
	empty(t, tm.C())
	if err := f.Advance(4 * time.Second); err != nil {
		t.Fatal(err)
	}
	empty(t, tm.C())
	if err := f.Advance(time.Second); err != nil {
		t.Fatal(err)
	}
	if got := recv(t, tm.C()); !got.Equal(t0.Add(6 * time.Second)) {
		t.Errorf("got %v", got)
	}
}

func TestFakeTimer_ResetNonPositiveFiresAtOnce(t *testing.T) {
	f := clock.NewFake(t0)
	tm := f.NewTimer(time.Hour)
	tm.Reset(0)
	if got := recv(t, tm.C()); !got.Equal(t0) {
		t.Errorf("got %v, want %v", got, t0)
	}
	if err := f.BlockUntil(bctx(t), 0); err != nil {
		t.Errorf("immediate timer counted as waiter: %v", err)
	}
}

func TestFakeBlockUntil_ReturnsWhenCountReached(t *testing.T) {
	f := clock.NewFake(t0)
	if err := f.BlockUntil(bctx(t), 0); err != nil {
		t.Fatalf("n=0 with no waiters: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- f.BlockUntil(bctx(t), 3) }()
	f.NewTimer(time.Hour)
	f.After(time.Hour)
	tk, err := f.NewTicker(time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer tk.Stop()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("BlockUntil = %v", err)
		}
	case <-bctx(t).Done():
		t.Fatal("BlockUntil did not return")
	}
	// Already satisfied returns immediately.
	if err := f.BlockUntil(bctx(t), 3); err != nil {
		t.Errorf("already-satisfied BlockUntil = %v", err)
	}
}

func TestFakeBlockUntil_ZeroAfterStop(t *testing.T) {
	f := clock.NewFake(t0)
	tm := f.NewTimer(time.Hour)
	if err := f.BlockUntil(bctx(t), 1); err != nil {
		t.Fatal(err)
	}
	tm.Stop()
	if err := f.BlockUntil(bctx(t), 0); err != nil {
		t.Fatalf("BlockUntil(0) after Stop = %v", err)
	}
}

func TestFakeBlockUntil_FiredOneShotNotAWaiter(t *testing.T) {
	f := clock.NewFake(t0)
	f.NewTimer(time.Second)
	if err := f.Advance(time.Second); err != nil {
		t.Fatal(err)
	}
	if err := f.BlockUntil(bctx(t), 0); err != nil {
		t.Fatalf("fired timer still counted: %v", err)
	}
}

func TestFakeBlockUntil_ContextDone_ReturnsKind(t *testing.T) {
	f := clock.NewFake(t0)
	t.Run("canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if err := f.BlockUntil(ctx, 1); !errs.Is(err, errs.Canceled) {
			t.Fatalf("err = %v, want Canceled", err)
		}
	})
	t.Run("deadline", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
		defer cancel()
		if err := f.BlockUntil(ctx, 1); !errs.Is(err, errs.Timeout) {
			t.Fatalf("err = %v, want Timeout", err)
		}
	})
	t.Run("exactly n, not at least n", func(t *testing.T) {
		f.NewTimer(time.Hour)
		f.NewTimer(time.Hour)
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
		defer cancel()
		if err := f.BlockUntil(ctx, 1); !errs.Is(err, errs.Timeout) {
			t.Fatalf("err = %v, want Timeout with 2 waiters and n=1", err)
		}
	})
}

func TestFakeBlockUntil_Negative_ReturnsInvalid(t *testing.T) {
	f := clock.NewFake(t0)
	if err := f.BlockUntil(bctx(t), -1); !errs.Is(err, errs.Invalid) {
		t.Fatalf("err = %v, want Invalid", err)
	}
}

func TestFake_ConcurrentUse(t *testing.T) {
	f := clock.NewFake(t0)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				tm := f.NewTimer(time.Duration(j+1) * time.Millisecond)
				_ = f.Now()
				_ = f.Since(t0)
				tm.Stop()
				tk, err := f.NewTicker(time.Millisecond)
				if err != nil {
					t.Error(err)
					return
				}
				tk.Stop()
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 50; j++ {
			if err := f.Advance(time.Millisecond); err != nil {
				t.Error(err)
			}
		}
	}()
	wg.Wait()
	if err := f.BlockUntil(bctx(t), 0); err != nil {
		t.Errorf("waiters leaked: %v", err)
	}
}

func TestReal_Concurrent(*testing.T) {
	c := clock.Real()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = c.Now()
			tm := c.NewTimer(time.Hour)
			tm.Stop()
		}()
	}
	wg.Wait()
}

func TestFake_ZeroValue_DoesNotPanic(t *testing.T) {
	var f clock.Fake
	tm := f.NewTimer(time.Second)
	if err := f.Advance(time.Second); err != nil {
		t.Fatalf("Advance: %v", err)
	}
	recv(t, tm.C())
	tk, err := f.NewTicker(time.Second)
	if err != nil {
		t.Fatalf("NewTicker: %v", err)
	}
	tk.Stop()
}
