package signals_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
)

func redeliverFixture(t *testing.T, h signals.Handler, mods ...func(*signals.Options)) *fixture {
	t.Helper()
	f := newFixture(t, mods...)
	f.scope("alpha", []signals.Name{nameX}, nil)
	b := f.scope("beta", nil, []signals.Name{nameX})
	subscribe(t, b, nameX, h)
	return f
}

func delivery(attempt int) signals.Delivery {
	return signals.Delivery{Signal: validSignal(nameX), Subscriber: "beta", Attempt: attempt}
}

func TestRedeliver_Succeeds_CallsHandlerOnceWithSignal(t *testing.T) {
	var mu sync.Mutex
	var got []signals.Signal
	var inDelivery, hasDL bool
	f := redeliverFixture(t, func(ctx context.Context, s signals.Signal) error {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, s)
		inDelivery = signals.InDelivery(ctx)
		_, hasDL = ctx.Deadline()
		return nil
	})
	dl := delivery(2)
	noErr(t, f.disp.Redeliver(context.Background(), dl))
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0].ID != dl.Signal.ID || !inDelivery || !hasDL {
		t.Errorf("got=%d inDelivery=%v deadline=%v", len(got), inDelivery, hasDL)
	}
	if o := outcomes(f.cap); o["beta"] != "ok" {
		t.Errorf("outcomes = %v", o)
	}
}

func TestRedeliver_InvalidDelivery_ReturnsInvalid(t *testing.T) {
	f := redeliverFixture(t, fail(nil))
	bad := []signals.Delivery{
		{},
		{Signal: validSignal(nameX), Subscriber: "beta", Attempt: 0},
		{Signal: validSignal(nameX), Subscriber: "BAD", Attempt: 1},
		{Signal: signals.Signal{Name: nameX}, Subscriber: "beta", Attempt: 1},
	}
	for _, dl := range bad {
		wantKind(t, f.disp.Redeliver(context.Background(), dl), errs.Invalid)
	}
}

func TestRedeliver_UnknownSubscriber_ReturnsNotFound(t *testing.T) {
	f := redeliverFixture(t, fail(nil))
	f.scope("gamma", nil, []signals.Name{nameY}) // scoped, subscribed to nothing registered
	for _, sub := range []string{"nobody", "alpha", "gamma"} {
		dl := delivery(1)
		dl.Subscriber = sub
		wantKind(t, f.disp.Redeliver(context.Background(), dl), errs.NotFound)
	}
	dl := delivery(1)
	dl.Signal.Name = nameY
	wantKind(t, f.disp.Redeliver(context.Background(), dl), errs.NotFound)
}

func TestRedeliver_HandlerFails_ReturnsErrorUnchanged(t *testing.T) {
	for _, k := range []errs.Kind{errs.Invalid, errs.Unavailable, errs.Timeout, errs.Internal} {
		t.Run(k.String(), func(t *testing.T) {
			want := errs.E("beta.h", k, errBoom)
			d := &fakeDeferrer{}
			f := redeliverFixture(t, fail(want), func(o *signals.Options) { o.Deferrer = d })
			err := f.disp.Redeliver(context.Background(), delivery(3))
			if !errors.Is(err, want) || !errors.Is(err, errBoom) {
				t.Fatalf("error changed: %v", err)
			}
			wantKind(t, err, k)
			if len(d.deliveries()) != 0 {
				t.Error("Redeliver handed a failure to the Deferrer")
			}
			if o := outcomes(f.cap); o["beta"] == "deferred" || o["beta"] == "ok" {
				t.Errorf("outcomes = %v", o)
			}
		})
	}
}

func TestRedeliver_HandlerPanics_ReturnsInternal(t *testing.T) {
	f := redeliverFixture(t, func(context.Context, signals.Signal) error { panic("kaboom") })
	err := f.disp.Redeliver(context.Background(), delivery(1))
	wantKind(t, err, errs.Internal)
	if o := outcomes(f.cap); o["beta"] != "panicked" {
		t.Errorf("outcomes = %v", o)
	}
}

func TestRedeliver_AfterCloseOrCtxDone_ReturnsKind(t *testing.T) {
	f := redeliverFixture(t, fail(nil))
	cctx, cancel := context.WithCancel(context.Background())
	cancel()
	wantKind(t, f.disp.Redeliver(cctx, delivery(1)), errs.Canceled)

	noErr(t, f.disp.Close(context.Background()))
	wantKind(t, f.disp.Redeliver(context.Background(), delivery(1)), errs.Unavailable)
}

func TestRedeliver_HonoursCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var seen error
	f := redeliverFixture(t, func(hctx context.Context, _ signals.Signal) error {
		cancel()
		seen = hctx.Err()
		return hctx.Err() //nolint:wrapcheck // test
	})
	err := f.disp.Redeliver(ctx, delivery(1))
	if seen == nil {
		t.Error("handler ctx did not follow the caller's cancellation")
	}
	if err == nil {
		t.Error("handler error not returned")
	}
}

func TestRedeliver_AttemptOnSpanAndLog(t *testing.T) {
	f := redeliverFixture(t, fail(errs.E("b", errs.Internal, errBoom)))
	wantKind(t, f.disp.Redeliver(context.Background(), delivery(3)), errs.Internal)
	spans := spansNamed(f.cap, "deliver:alpha.x")
	if len(spans) != 1 || spans[0].Attrs["attempt"] != "3" {
		t.Errorf("spans = %+v", spans)
	}
	found := false
	for _, r := range recordsAt(f.cap, slog.LevelError) {
		if r.Attrs["attempt"] == "3" {
			found = true
		}
	}
	if !found {
		t.Errorf("no error record with attempt 3: %v", f.cap.Records())
	}
}

func TestRedeliver_AfterClose_ReturnsUnavailableAndNeverCallsHandler(t *testing.T) {
	var called atomic.Bool
	f := redeliverFixture(t, func(context.Context, signals.Signal) error { called.Store(true); return nil })
	noErr(t, f.disp.Close(context.Background()))
	wantKind(t, f.disp.Redeliver(context.Background(), delivery(1)), errs.Unavailable)
	if called.Load() {
		t.Error("handler ran after Close")
	}
	if n := len(delivered(f.cap)); n != 0 {
		t.Errorf("delivered counted after Close: %d", n)
	}
}
