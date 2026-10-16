package retry_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/retry"
	"github.com/zedgama3/bms/internal/lib/validate"
)

var t0 = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func one() float64  { return 1 }
func zero() float64 { return 0 }

func testPolicy() retry.Policy {
	return retry.Policy{Initial: time.Second, Max: 10 * time.Second, Multiplier: 2, MaxAttempts: 5}
}

func bctx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// runDo runs retry.Do on its own goroutine and returns a channel with its result.
func runDo(ctx context.Context, clk clock.Clock, p retry.Policy, rnd retry.Rand, fn func(context.Context) error) <-chan error {
	ch := make(chan error, 1)
	go func() { ch <- retry.Do(ctx, clk, p, rnd, fn) }()
	return ch
}

func result(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-bctx(t).Done():
		t.Fatal("Do did not return")
		return nil
	}
}

// ---- Default / Validate ----

func TestDefault_Values(t *testing.T) {
	want := retry.Policy{Initial: 500 * time.Millisecond, Max: 30 * time.Second, Multiplier: 2, MaxAttempts: 5}
	if got := retry.Default(); got != want {
		t.Errorf("Default = %+v, want %+v", got, want)
	}
	if err := retry.Default().Validate(); err != nil {
		t.Errorf("Default invalid: %v", err)
	}
}

func TestPolicyValidate_EachField_ReturnsInvalid(t *testing.T) {
	ok := testPolicy()
	tests := []struct {
		name    string
		mutate  func(*retry.Policy)
		invalid bool
		field   string
	}{
		{"valid", func(*retry.Policy) {}, false, ""},
		{"unlimited attempts valid", func(p *retry.Policy) { p.MaxAttempts = 0 }, false, ""},
		{"multiplier exactly 1 valid", func(p *retry.Policy) { p.Multiplier = 1 }, false, ""},
		{"max equals initial valid", func(p *retry.Policy) { p.Max = p.Initial }, false, ""},
		{"initial zero", func(p *retry.Policy) { p.Initial = 0 }, true, "initial"},
		{"initial negative", func(p *retry.Policy) { p.Initial = -time.Second }, true, "initial"},
		{"max below initial", func(p *retry.Policy) { p.Max = p.Initial - 1 }, true, "max"},
		{"multiplier below 1", func(p *retry.Policy) { p.Multiplier = 0.99 }, true, "multiplier"},
		{"multiplier zero", func(p *retry.Policy) { p.Multiplier = 0 }, true, "multiplier"},
		{"negative attempts", func(p *retry.Policy) { p.MaxAttempts = -1 }, true, "max_attempts"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := ok
			tc.mutate(&p)
			err := p.Validate()
			if tc.invalid != errs.Is(err, errs.Invalid) {
				t.Fatalf("err = %v, want invalid=%v", err, tc.invalid)
			}
			if !tc.invalid && err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
			if tc.invalid {
				fs := validate.Fields(err)
				if len(fs) != 1 || fs[0].Field != tc.field {
					t.Errorf("Fields = %v, want exactly [%s]", fs, tc.field)
				}
			}
		})
	}
}

func TestPolicyValidate_ManyProblems_ReportsAll(t *testing.T) {
	p := retry.Policy{Initial: 0, Max: -1, Multiplier: 0.5, MaxAttempts: -3}
	err := p.Validate()
	if !errs.Is(err, errs.Invalid) {
		t.Fatalf("err = %v, want Invalid", err)
	}
	var got []string
	for _, fe := range validate.Fields(err) {
		got = append(got, fe.Field)
	}
	want := []string{"initial", "max", "multiplier", "max_attempts"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("fields = %v, want %v", got, want)
	}
}

// ---- Delay ----

func TestDelay_Schedule(t *testing.T) {
	p := retry.Policy{Initial: 100 * time.Millisecond, Max: time.Second, Multiplier: 2, MaxAttempts: 10}
	tests := []struct {
		attempt int
		u       float64
		want    time.Duration
	}{
		{-5, 1, 0},
		{0, 1, 0},
		{1, 1, 0},
		{2, 1, 100 * time.Millisecond},
		{3, 1, 200 * time.Millisecond},
		{4, 1, 400 * time.Millisecond},
		{5, 1, 800 * time.Millisecond},
		{6, 1, time.Second},
		{7, 1, time.Second},
		{2, 0, 0},
		{4, 0, 0},
		{2, 0.5, 50 * time.Millisecond},
		{4, 0.25, 100 * time.Millisecond},
		{6, 0.5, 500 * time.Millisecond},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("attempt%d_u%v", tc.attempt, tc.u), func(t *testing.T) {
			if got := p.Delay(tc.attempt, tc.u); got != tc.want {
				t.Errorf("Delay(%d,%v) = %v, want %v", tc.attempt, tc.u, got, tc.want)
			}
		})
	}
}

func TestDelay_CapsAtMax(t *testing.T) {
	p := retry.Policy{Initial: time.Second, Max: 5 * time.Second, Multiplier: 3, MaxAttempts: 0}
	for n := 2; n < 40; n++ {
		if got := p.Delay(n, 1); got > p.Max {
			t.Fatalf("Delay(%d) = %v > Max", n, got)
		}
	}
	if got := p.Delay(10, 1); got != p.Max {
		t.Errorf("Delay(10) = %v, want Max", got)
	}
}

func TestDelay_ConstantMultiplierOne(t *testing.T) {
	p := retry.Policy{Initial: time.Second, Max: time.Minute, Multiplier: 1}
	for n := 2; n < 20; n++ {
		if got := p.Delay(n, 1); got != time.Second {
			t.Fatalf("Delay(%d) = %v, want 1s", n, got)
		}
	}
}

func TestDelay_LargeAttemptNoOverflow(t *testing.T) {
	p := retry.Policy{Initial: time.Second, Max: time.Minute, Multiplier: 10, MaxAttempts: 0}
	for _, n := range []int{100, 1000, 1 << 20, math.MaxInt32, math.MaxInt} {
		if got := p.Delay(n, 1); got != time.Minute {
			t.Errorf("Delay(%d) = %v, want Max", n, got)
		}
	}
	big := retry.Policy{Initial: time.Hour, Max: time.Duration(math.MaxInt64), Multiplier: 1000}
	for _, n := range []int{5, 50, 5000} {
		if got := big.Delay(n, 1); got < 0 {
			t.Errorf("Delay(%d) = %v, negative (overflow)", n, got)
		}
	}
}

func TestDelay_RandOutOfRange_TreatedAsOne(t *testing.T) {
	p := retry.Policy{Initial: 100 * time.Millisecond, Max: time.Second, Multiplier: 2}
	want := p.Delay(3, 1)
	for _, u := range []float64{-0.1, -1, 1.0000001, 2, 1e9, math.NaN(), math.Inf(1), math.Inf(-1)} {
		if got := p.Delay(3, u); got != want {
			t.Errorf("Delay(3,%v) = %v, want %v", u, got, want)
		}
	}
}

// ---- Retryable ----

func TestRetryable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"unavailable", errs.E("x", errs.Unavailable, errors.New("e")), true},
		{"timeout", errs.E("x", errs.Timeout, errors.New("e")), true},
		{"wrapped unavailable", fmt.Errorf("ctx: %w", errs.E("x", errs.Unavailable, errors.New("e"))), true},
		{"deadline exceeded sentinel", context.DeadlineExceeded, true},
		{"canceled", errs.E("x", errs.Canceled, errors.New("e")), false},
		{"canceled sentinel", context.Canceled, false},
		{"invalid", errs.E("x", errs.Invalid, errors.New("e")), false},
		{"permission", errs.E("x", errs.Permission, errors.New("e")), false},
		{"internal", errs.E("x", errs.Internal, errors.New("e")), false},
		{"plain", errors.New("boom"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := retry.Retryable(tc.err); got != tc.want {
				t.Errorf("Retryable = %v, want %v", got, tc.want)
			}
		})
	}
}

// ---- Do ----

func TestDo_InvalidPolicy_ReturnsInvalid(t *testing.T) {
	var calls atomic.Int32
	err := retry.Do(bctx(t), clock.NewFake(t0), retry.Policy{}, one, func(context.Context) error {
		calls.Add(1)
		return nil
	})
	if !errs.Is(err, errs.Invalid) {
		t.Fatalf("err = %v, want Invalid", err)
	}
	if calls.Load() != 0 {
		t.Error("fn called despite invalid policy")
	}
}

func TestDo_NilClock_ReturnsInvalid(t *testing.T) {
	err := retry.Do(bctx(t), nil, testPolicy(), one, func(context.Context) error { return nil })
	if !errs.Is(err, errs.Invalid) {
		t.Fatalf("err = %v, want Invalid", err)
	}
}

func TestDo_NilFn_ReturnsInvalid(t *testing.T) {
	err := retry.Do(bctx(t), clock.NewFake(t0), testPolicy(), one, nil)
	if !errs.Is(err, errs.Invalid) {
		t.Fatalf("err = %v, want Invalid", err)
	}
}

func TestDo_UnlimitedWithoutDeadline_ReturnsInvalid(t *testing.T) {
	p := testPolicy()
	p.MaxAttempts = 0
	var calls atomic.Int32
	err := retry.Do(t.Context(), clock.NewFake(t0), p, one, func(context.Context) error {
		calls.Add(1)
		return nil
	})
	if !errs.Is(err, errs.Invalid) {
		t.Fatalf("err = %v, want Invalid", err)
	}
	if calls.Load() != 0 {
		t.Error("fn called")
	}
}

func TestDo_UnlimitedWithDeadline_Runs(t *testing.T) {
	p := testPolicy()
	p.MaxAttempts = 0
	if err := retry.Do(bctx(t), clock.NewFake(t0), p, one, func(context.Context) error { return nil }); err != nil {
		t.Fatalf("err = %v", err)
	}
}

func TestDo_SucceedsFirstTry_NoWait(t *testing.T) {
	f := clock.NewFake(t0)
	var calls int
	err := retry.Do(bctx(t), f, testPolicy(), nil, func(context.Context) error { calls++; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
	if !f.Now().Equal(t0) {
		t.Error("clock moved")
	}
	if err := f.BlockUntil(bctx(t), 0); err != nil {
		t.Errorf("waiter left: %v", err)
	}
}

func TestDo_PassesSameContext(t *testing.T) {
	type key struct{}
	ctx := context.WithValue(bctx(t), key{}, "v")
	err := retry.Do(ctx, clock.NewFake(t0), testPolicy(), one, func(got context.Context) error {
		if got != ctx {
			t.Error("fn got a different ctx")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDo_RetriesUnavailableThenSucceeds(t *testing.T) {
	f := clock.NewFake(t0)
	var calls atomic.Int32
	fn := func(context.Context) error {
		if calls.Add(1) < 3 {
			return errs.E("fn", errs.Unavailable, errors.New("down"))
		}
		return nil
	}
	ch := runDo(bctx(t), f, testPolicy(), one, fn)

	if err := f.BlockUntil(bctx(t), 1); err != nil {
		t.Fatal(err)
	}
	// First wait is Initial (1s) with u=1: 999ms is not enough.
	if err := f.Advance(999 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := f.BlockUntil(bctx(t), 1); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("calls = %d before first delay elapsed, want 1", got)
	}
	if err := f.Advance(time.Millisecond); err != nil {
		t.Fatal(err)
	}
	// Second wait is 2s.
	if err := f.BlockUntil(bctx(t), 1); err != nil {
		t.Fatal(err)
	}
	if err := f.Advance(2 * time.Second); err != nil {
		t.Fatal(err)
	}
	if err := result(t, ch); err != nil {
		t.Fatalf("Do = %v, want nil", err)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("calls = %d, want 3", got)
	}
}

func TestDo_NonRetryable_ReturnsUnchangedAfterOneAttempt(t *testing.T) {
	for _, k := range []errs.Kind{errs.Permission, errs.Invalid, errs.Internal, errs.NotFound} {
		t.Run(k.String(), func(t *testing.T) {
			want := errs.E("fn", k, errors.New("nope"))
			var calls int
			err := retry.Do(bctx(t), clock.NewFake(t0), testPolicy(), one, func(context.Context) error {
				calls++
				return want
			})
			if !errors.Is(err, want) || err != want { //nolint:errorlint // identity check is the point: error must be returned unwrapped
				t.Errorf("err = %v, want the identical error value (no wrapping)", err)
			}
			if calls != 1 {
				t.Errorf("calls = %d, want 1", calls)
			}
		})
	}
}

func TestDo_Exhausted_ReturnsLastKindWithAttemptCount(t *testing.T) {
	for _, k := range []errs.Kind{errs.Unavailable, errs.Timeout} {
		t.Run(k.String(), func(t *testing.T) {
			p := testPolicy()
			p.MaxAttempts = 3
			var calls int
			last := errors.New("last")
			// rnd=0 gives zero delays so no clock driving is needed.
			err := retry.Do(bctx(t), clock.NewFake(t0), p, zero, func(context.Context) error {
				calls++
				return errs.E("fn", k, fmt.Errorf("attempt %d: %w", calls, last))
			})
			if calls != 3 {
				t.Errorf("calls = %d, want 3", calls)
			}
			if !errs.Is(err, k) {
				t.Errorf("kind = %v, want %v", errs.KindOf(err), k)
			}
			if !errors.Is(err, last) {
				t.Error("last error not in chain")
			}
		})
	}
}

func TestDo_ExhaustedWithMaxAttemptsOne_NoWait(t *testing.T) {
	p := testPolicy()
	p.MaxAttempts = 1
	f := clock.NewFake(t0)
	var calls int
	err := retry.Do(bctx(t), f, p, one, func(context.Context) error {
		calls++
		return errs.E("fn", errs.Unavailable, errors.New("x"))
	})
	if !errs.Is(err, errs.Unavailable) || calls != 1 {
		t.Fatalf("err = %v calls = %d", err, calls)
	}
	if err := f.BlockUntil(bctx(t), 0); err != nil {
		t.Errorf("waiter left: %v", err)
	}
}

func TestDo_CanceledDuringWait_ReturnsCanceledJoinedWithLast(t *testing.T) {
	f := clock.NewFake(t0)
	ctx, cancel := context.WithCancel(bctx(t))
	defer cancel()
	last := errs.E("fn", errs.Unavailable, errors.New("down"))
	ch := runDo(ctx, f, testPolicy(), one, func(context.Context) error { return last })
	if err := f.BlockUntil(bctx(t), 1); err != nil {
		t.Fatal(err)
	}
	cancel()
	err := result(t, ch)
	if !errs.Is(err, errs.Canceled) {
		t.Fatalf("kind = %v, want Canceled", errs.KindOf(err))
	}
	if !errors.Is(err, context.Canceled) {
		t.Error("errors.Is(context.Canceled) = false")
	}
	if !errors.Is(err, last) {
		t.Error("last error not joined")
	}
	if err := f.BlockUntil(bctx(t), 0); err != nil {
		t.Errorf("timer leaked: %v", err)
	}
}

func TestDo_AlreadyCanceled_NoAttempt(t *testing.T) {
	ctx, cancel := context.WithCancel(bctx(t))
	cancel()
	var calls int
	err := retry.Do(ctx, clock.NewFake(t0), testPolicy(), one, func(context.Context) error { calls++; return nil })
	if !errs.Is(err, errs.Canceled) || !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want Canceled", err)
	}
	if calls != 0 {
		t.Errorf("calls = %d, want 0 (ctx checked before each attempt)", calls)
	}
}

func TestDo_DeadlineDuringWait_ReturnsTimeout(t *testing.T) {
	f := clock.NewFake(t0)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	last := errs.E("fn", errs.Unavailable, errors.New("down"))
	err := retry.Do(ctx, f, testPolicy(), one, func(context.Context) error { return last })
	if !errs.Is(err, errs.Timeout) {
		t.Fatalf("kind = %v, want Timeout", errs.KindOf(err))
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Error("DeadlineExceeded not in chain")
	}
	if !errors.Is(err, last) {
		t.Error("last not joined")
	}
}

func TestDo_RetryableAfterCtxDone_NotRetried(t *testing.T) {
	ctx, cancel := context.WithCancel(bctx(t))
	defer cancel()
	f := clock.NewFake(t0)
	var calls int
	last := errs.E("fn", errs.Unavailable, errors.New("down"))
	err := retry.Do(ctx, f, testPolicy(), zero, func(context.Context) error {
		calls++
		cancel()
		return last
	})
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
	if !errs.Is(err, errs.Canceled) {
		t.Errorf("kind = %v, want Canceled", errs.KindOf(err))
	}
	if !errors.Is(err, last) {
		t.Error("last not joined")
	}
}

func TestDo_RetryableAfterDeadline_ReturnsTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	var calls int
	err := retry.Do(ctx, clock.NewFake(t0), testPolicy(), zero, func(c context.Context) error {
		calls++
		<-c.Done()
		return errs.E("fn", errs.Unavailable, errors.New("down"))
	})
	if calls != 1 || !errs.Is(err, errs.Timeout) {
		t.Fatalf("calls = %d err = %v, want 1 call and Timeout", calls, err)
	}
}

func TestDo_ConcurrentSafe(t *testing.T) {
	f := clock.NewFake(t0)
	chs := make([]<-chan error, 8)
	for i := range chs {
		chs[i] = runDo(bctx(t), f, testPolicy(), nil, func(context.Context) error { return nil })
	}
	for _, ch := range chs {
		if err := result(t, ch); err != nil {
			t.Error(err)
		}
	}
}

// ---- Backoff ----

func TestNewBackoff_InvalidPolicy_ReturnsInvalid(t *testing.T) {
	b, err := retry.NewBackoff(retry.Policy{}, one)
	if !errs.Is(err, errs.Invalid) {
		t.Fatalf("err = %v, want Invalid", err)
	}
	if b != nil {
		t.Error("backoff non-nil on error")
	}
}

func TestBackoff_GrowsThenResets(t *testing.T) {
	p := retry.Policy{Initial: 100 * time.Millisecond, Max: 350 * time.Millisecond, Multiplier: 2, MaxAttempts: 0}
	b, err := retry.NewBackoff(p, one)
	if err != nil {
		t.Fatal(err)
	}
	if b.Attempt() != 0 {
		t.Errorf("fresh Attempt = %d", b.Attempt())
	}
	want := []time.Duration{100, 200, 350, 350}
	for i, w := range want {
		d, ok := b.Next()
		if !ok || d != w*time.Millisecond {
			t.Fatalf("Next #%d = %v,%v want %v,true", i+1, d, ok, w*time.Millisecond)
		}
		if b.Attempt() != i+1 {
			t.Errorf("Attempt = %d, want %d", b.Attempt(), i+1)
		}
	}
	b.Reset()
	if b.Attempt() != 0 {
		t.Errorf("Attempt after Reset = %d", b.Attempt())
	}
	if d, ok := b.Next(); !ok || d != 100*time.Millisecond {
		t.Errorf("Next after Reset = %v,%v", d, ok)
	}
}

func TestBackoffNext_Exhausted_ReturnsNotOK(t *testing.T) {
	p := retry.Policy{Initial: 100 * time.Millisecond, Max: time.Second, Multiplier: 2, MaxAttempts: 3}
	b, err := retry.NewBackoff(p, one)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, ok := b.Next(); !ok {
			t.Fatalf("Next #%d ok = false, want true", i+1)
		}
	}
	d, ok := b.Next() // third failure recorded
	if ok || d != 0 {
		t.Errorf("Next at MaxAttempts = %v,%v want 0,false", d, ok)
	}
	if b.Attempt() != 3 {
		t.Errorf("Attempt = %d, want 3", b.Attempt())
	}
	if d, ok := b.Next(); ok || d != 0 || b.Attempt() != 4 {
		t.Errorf("Next past exhaustion = %v,%v Attempt=%d, want 0,false,4 (keeps counting)", d, ok, b.Attempt())
	}
	b.Reset()
	if _, ok := b.Next(); !ok {
		t.Error("Next after Reset ok = false")
	}
}

func TestBackoff_UnlimitedNeverExhausts(t *testing.T) {
	b, err := retry.NewBackoff(retry.Policy{Initial: time.Millisecond, Max: time.Second, Multiplier: 2}, one)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200; i++ {
		if _, ok := b.Next(); !ok {
			t.Fatalf("exhausted at %d with MaxAttempts 0", i)
		}
	}
}

func TestBackoff_NilRandUsesDefault(t *testing.T) {
	b, err := retry.NewBackoff(testPolicy(), nil)
	if err != nil {
		t.Fatal(err)
	}
	d, ok := b.Next()
	if !ok || d < 0 || d > time.Second {
		t.Errorf("Next = %v,%v want within [0,1s]", d, ok)
	}
}
