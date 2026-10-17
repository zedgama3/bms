package app

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// step is a fake closer and what it saw when it was called.
type step struct {
	name      string
	remaining time.Duration // wall time left on its ctx when it was called
	hasDL     bool
	ctxErr    error
	err       error
	advance   time.Duration // how far the fake clock moves while this closer runs
}

// closersOf turns the fakes into closers that append to *calls in call order.
func closersOf(clk *clock.Fake, steps []*step, calls *[]*step) []closer {
	out := make([]closer, len(steps))
	for i, s := range steps {
		out[i] = closer{name: s.name, close: func(ctx context.Context) error {
			dl, ok := ctx.Deadline()
			s.hasDL, s.ctxErr = ok, ctx.Err()
			s.remaining = time.Until(dl)
			*calls = append(*calls, s)
			if s.advance > 0 {
				if err := clk.Advance(s.advance); err != nil {
					return err //nolint:wrapcheck // a test fake
				}
			}
			return s.err
		}}
	}
	return out
}

func sixSteps() []*step {
	names := []string{"http", "runnables", "signals", "modules", "db", "telemetry"}
	out := make([]*step, len(names))
	for i, n := range names {
		out[i] = &step{name: n}
	}
	return out
}

// near reports whether got is within slack below want (a deadline measured a moment after
// it was set is a little shorter than the share, never longer).
func near(got, want, slack time.Duration) bool { return got <= want && got > want-slack }

// F15
func TestShutdown_ContinuesPastFailuresAndJoins(t *testing.T) {
	c := telemetrytest.New(t)
	clk := clock.NewFake(time.Now())
	steps := sixSteps()
	errA, errB := errors.New("runnables failed"), errors.New("db failed")
	steps[1].err, steps[4].err = errA, errB
	var calls []*step

	err := shutdown(context.Background(), c.Logger(), clk, 15*time.Second, closersOf(clk, steps, &calls))

	if len(calls) != len(steps) {
		t.Fatalf("%d closers ran, want all %d", len(calls), len(steps))
	}
	if !errors.Is(err, errA) || !errors.Is(err, errB) {
		t.Errorf("err = %v, want both failures joined", err)
	}
	failed, stopped := 0, 0
	for _, r := range c.Records() {
		if r.Message != "step stopped" {
			continue
		}
		if r.Attrs["name"] == "" || r.Attrs["duration_ms"] == "" {
			t.Errorf("a step record lacks name or duration_ms: %v", r.Attrs)
		}
		if r.Level == slog.LevelError {
			failed++
			if r.Attrs["error.kind"] == "" {
				t.Errorf("a failed step's record lacks error.kind: %v", r.Attrs)
			}
		} else {
			stopped++
		}
	}
	if failed != 2 || stopped != 4 {
		t.Errorf("records: %d failures, %d successes; want 2 and 4", failed, stopped)
	}
}

func TestShutdown_AllSucceed_ReturnsNil(t *testing.T) {
	clk := clock.NewFake(time.Now())
	var calls []*step
	if err := shutdown(context.Background(), nil, clk, 15*time.Second, closersOf(clk, sixSteps(), &calls)); err != nil {
		t.Errorf("err = %v, want nil", err)
	}
}

// F15: the order is the caller's; HTTP's share is max(1s, 2b/3), every other closer gets
// what is left of start+b.
func TestShutdown_OrderAndDeadlines(t *testing.T) {
	for _, tc := range []struct {
		name       string
		budget     time.Duration
		wantFirst  time.Duration
		wantOthers time.Duration
	}{
		{"default budget", 15 * time.Second, 10 * time.Second, 15 * time.Second},
		{"three seconds", 3 * time.Second, 2 * time.Second, 3 * time.Second},
		{"the first share has a floor of one second", 1200 * time.Millisecond, time.Second, 1200 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clk := clock.NewFake(time.Now())
			steps := sixSteps()
			var calls []*step
			if err := shutdown(context.Background(), nil, clk, tc.budget, closersOf(clk, steps, &calls)); err != nil {
				t.Fatalf("shutdown: %v", err)
			}
			for i, s := range calls {
				if s.name != steps[i].name {
					t.Fatalf("call %d is %s, want %s (order http, runnables, signals, modules, db, telemetry)", i, s.name, steps[i].name)
				}
				want := tc.wantOthers
				if i == 0 {
					want = tc.wantFirst
				}
				if !s.hasDL || !near(s.remaining, want, 500*time.Millisecond) {
					t.Errorf("%s: %v left (deadline set %v), want about %v", s.name, s.remaining, s.hasDL, want)
				}
			}
		})
	}
}

// F15: the signal that started the shutdown must not cut it short.
func TestShutdown_DetachedFromCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	clk := clock.NewFake(time.Now())
	var calls []*step
	if err := shutdown(ctx, nil, clk, 15*time.Second, closersOf(clk, sixSteps(), &calls)); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	for _, s := range calls {
		if s.ctxErr != nil || !s.hasDL {
			t.Errorf("%s: ctx err = %v, deadline set = %v; want a live ctx with a deadline", s.name, s.ctxErr, s.hasDL)
		}
	}
}

// A closer is called even when the shared deadline has passed: the first one outlasts the
// whole budget, and every later one gets a done context and still runs.
func TestShutdown_PassedDeadline_StillCallsEveryCloser(t *testing.T) {
	clk := clock.NewFake(time.Now())
	steps := sixSteps()
	steps[0].advance = 20 * time.Second
	var calls []*step
	_ = shutdown(context.Background(), nil, clk, 15*time.Second, closersOf(clk, steps, &calls)) //nolint:errcheck // the result is not under test
	if len(calls) != len(steps) {
		t.Fatalf("%d closers ran, want %d", len(calls), len(steps))
	}
	for _, s := range calls[1:] {
		if s.ctxErr == nil {
			t.Errorf("%s: ctx is live although the budget was spent", s.name)
		}
	}
}

// F16: HTTP uses its whole share and fails with Timeout; the closers behind it still run
// and share the last third of the budget.
func TestShutdown_HTTPTimeout_RestStillRunWithinBudget(t *testing.T) {
	const budget = 15 * time.Second
	clk := clock.NewFake(time.Now())
	steps := sixSteps()
	steps[0].advance = 10 * time.Second // HTTP's share: the fake clock says it ran that long
	steps[0].err = errs.E("test.http", errs.Timeout, context.DeadlineExceeded)
	var calls []*step

	err := shutdown(context.Background(), nil, clk, budget, closersOf(clk, steps, &calls))

	if !errs.Is(err, errs.Timeout) {
		t.Errorf("err = %v, want it to carry the HTTP Timeout", err)
	}
	if len(calls) != len(steps) {
		t.Fatalf("%d closers ran, want %d", len(calls), len(steps))
	}
	for _, s := range calls[1:] {
		if !s.hasDL || s.ctxErr != nil {
			t.Fatalf("%s: deadline set = %v, ctx err = %v; want a live ctx", s.name, s.hasDL, s.ctxErr)
		}
		if !near(s.remaining, budget/3, 500*time.Millisecond) {
			t.Errorf("%s: %v left, want about a third of the budget (%v)", s.name, s.remaining, budget/3)
		}
	}
}
