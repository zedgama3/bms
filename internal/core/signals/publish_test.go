package signals_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// pair wires alpha (publisher of x, y) to a subscriber beta.
func pair(t *testing.T, f *fixture, h signals.Handler) signals.Bus {
	t.Helper()
	a := f.scope("alpha", []signals.Name{nameX, nameY}, nil)
	b := f.scope("beta", nil, []signals.Name{nameX, nameY})
	subscribe(t, b, nameX, h)
	return a
}

func TestPublish_Undeclared_ReturnsInvalidAndNothingDelivered(t *testing.T) {
	f := newFixture(t)
	var l log
	a := pair(t, f, l.mark("beta"))
	ctx := context.Background()

	wantKind(t, a.Publish(ctx, nameOther, nil), errs.Invalid)

	// inside a transaction the error fails the use case: nothing is delivered
	err := f.tx(ctx, func(ctx context.Context) error { return a.Publish(ctx, nameOther, nil) })
	wantKind(t, err, errs.Invalid)
	if got := l.all(); len(got) != 0 {
		t.Errorf("delivered %v", got)
	}
	if n := len(measures(f.cap, "bms.signals.published")); n != 0 {
		t.Errorf("published counted %d times for refused publishes", n)
	}
}

func TestPublish_InvalidPayload_ReturnsInvalid(t *testing.T) {
	f := newFixture(t)
	var l log
	a := pair(t, f, l.mark("beta"))
	bad := []signals.Payload{
		{"Bad": model.NewID[signals.RecordID]()},
		{"person": {}},
	}
	for _, p := range bad {
		wantKind(t, a.Publish(context.Background(), nameX, p), errs.Invalid)
	}
	if len(l.all()) != 0 {
		t.Error("an invalid payload was delivered")
	}
}

func TestPublish_ContextDone_ReturnsKind(t *testing.T) {
	f := newFixture(t)
	var l log
	a := pair(t, f, l.mark("beta"))

	cctx, cancel := context.WithCancel(context.Background())
	cancel()
	wantKind(t, a.Publish(cctx, nameX, nil), errs.Canceled)

	dctx, dcancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer dcancel()
	wantKind(t, a.Publish(dctx, nameX, nil), errs.Timeout)

	if len(l.all()) != 0 {
		t.Error("delivered despite a done context")
	}
}

func TestPublish_NoSubscribers_IsNotAnError(t *testing.T) {
	f := newFixture(t)
	a := f.scope("alpha", []signals.Name{nameX}, nil)
	noErr(t, a.Publish(context.Background(), nameX, person()))
	if n := len(measures(f.cap, "bms.signals.published")); n != 1 {
		t.Errorf("published measurements = %d, want 1", n)
	}
	if n := len(delivered(f.cap)); n != 0 {
		t.Errorf("delivered measurements = %d, want 0", n)
	}
}

func TestPublish_OutsideTransaction_DeliversAtOnce(t *testing.T) {
	f := newFixture(t)
	var ran atomic.Bool
	a := pair(t, f, func(context.Context, signals.Signal) error { ran.Store(true); return nil })
	noErr(t, a.Publish(context.Background(), nameX, person()))
	if !ran.Load() {
		t.Error("handler had not run when Publish returned")
	}
}

func TestPublish_TransactionRollsBack_DeliversNothing(t *testing.T) {
	f := newFixture(t)
	var l log
	a := pair(t, f, l.mark("beta"))
	err := f.tx(context.Background(), func(ctx context.Context) error {
		noErr(t, a.Publish(ctx, nameX, person()))
		return errBoom
	})
	if err == nil {
		t.Fatal("transaction should have failed")
	}
	if len(l.all()) != 0 {
		t.Errorf("delivered after rollback: %v", l.all())
	}
	if n := len(delivered(f.cap)); n != 0 {
		t.Errorf("delivered counted after rollback: %d", n)
	}
	if n := len(measures(f.cap, "bms.signals.published")); n != 0 {
		t.Errorf("published counted after rollback: %d", n)
	}
}

func TestPublish_SeveralInOneTransaction_DeliveredInOrderAfterCommit(t *testing.T) {
	f := newFixture(t)
	var l log
	a := f.scope("alpha", []signals.Name{nameX, nameY}, nil)
	b := f.scope("beta", nil, []signals.Name{nameX, nameY})
	subscribe(t, b, nameX, l.mark("x1"))
	subscribe(t, b, nameY, l.mark("y"))
	err := f.tx(context.Background(), func(ctx context.Context) error {
		noErr(t, a.Publish(ctx, nameX, nil))
		noErr(t, a.Publish(ctx, nameY, nil))
		if len(l.all()) != 0 {
			t.Error("delivered before commit")
		}
		return a.Publish(ctx, nameX, nil)
	})
	noErr(t, err)
	got := l.all()
	want := []string{"x1", "y", "x1"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestPublish_InsideReadTransaction_DeliveredAfterCommit(t *testing.T) {
	f := newFixture(t)
	var l log
	a := pair(t, f, l.mark("beta"))
	err := f.fdb.Transact(context.Background(), db.Read, func(ctx context.Context) error {
		if err := a.Publish(ctx, nameX, nil); err != nil {
			return err //nolint:wrapcheck // test
		}
		if len(l.all()) != 0 {
			t.Error("delivered before commit")
		}
		return nil
	})
	noErr(t, err)
	if len(l.all()) != 1 {
		t.Errorf("got %v", l.all())
	}
}

func TestPublish_FillsIDAndAtFromClock(t *testing.T) {
	f := newFixture(t)
	var mu sync.Mutex
	var got []signals.Signal
	a := pair(t, f, func(_ context.Context, s signals.Signal) error {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, s)
		return nil
	})
	publishedAt := f.clk.Now()
	err := f.tx(context.Background(), func(ctx context.Context) error {
		noErr(t, a.Publish(ctx, nameX, person()))
		return f.clk.Advance(time.Hour) // commit happens later than publish
	})
	noErr(t, err)
	noErr(t, a.Publish(context.Background(), nameX, person()))

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("got %d signals", len(got))
	}
	if !got[0].At.Equal(publishedAt) {
		t.Errorf("At = %v, want publish time %v", got[0].At, publishedAt)
	}
	if !got[1].At.Equal(publishedAt.Add(time.Hour)) {
		t.Errorf("second At = %v", got[1].At)
	}
	if got[0].ID.IsZero() || got[1].ID.IsZero() || got[0].ID == got[1].ID {
		t.Errorf("IDs not distinct non-zero: %v %v", got[0].ID, got[1].ID)
	}
	if got[0].Name != nameX {
		t.Errorf("Name = %v", got[0].Name)
	}
	noErr(t, got[0].Validate())
}

func TestPublish_PayloadMutationAfterPublishIsHarmless(t *testing.T) {
	f := newFixture(t)
	var got signals.Signal
	a := pair(t, f, func(_ context.Context, s signals.Signal) error { got = s; return nil })
	id := model.NewID[model.PersonID]()
	p := signals.NewPayload(signals.Ref("person", id))
	err := f.tx(context.Background(), func(ctx context.Context) error {
		if err := a.Publish(ctx, nameX, p); err != nil {
			return err //nolint:wrapcheck // test
		}
		p["person"] = model.NewID[signals.RecordID]()
		p["extra"] = model.NewID[signals.RecordID]()
		return nil
	})
	noErr(t, err)
	gp, gerr := signals.Get[model.PersonID](got.Payload, "person")
	noErr(t, gerr)
	if gp != id || len(got.Payload) != 1 {
		t.Errorf("handler saw the mutated payload: %v", got.Payload)
	}
}

func TestSubscribe_OrderIsSubscriptionOrder(t *testing.T) {
	f := newFixture(t)
	var l log
	a := f.scope("alpha", []signals.Name{nameX}, nil)
	for _, o := range []string{"delta", "beta", "gamma", "epsilon"} {
		b := f.scope(o, nil, []signals.Name{nameX})
		subscribe(t, b, nameX, l.mark(o))
	}
	noErr(t, a.Publish(context.Background(), nameX, nil))
	got := l.all()
	want := []string{"delta", "beta", "gamma", "epsilon"}
	if len(got) != 4 {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestPublish_NestedBeyondMaxDepth_ReturnsInvalid(t *testing.T) {
	f := newFixture(t)
	a := f.scope("alpha", []signals.Name{nameX}, []signals.Name{nameX})
	var mu sync.Mutex
	calls := 0
	var lastErr error
	subscribe(t, a, nameX, func(ctx context.Context, _ signals.Signal) error {
		mu.Lock()
		calls++
		mu.Unlock()
		err := a.Publish(ctx, nameX, nil)
		if err != nil {
			mu.Lock()
			lastErr = err
			mu.Unlock()
		}
		return err //nolint:wrapcheck // test passes the Publish error on unchanged
	})
	noErr(t, a.Publish(context.Background(), nameX, nil))
	mu.Lock()
	defer mu.Unlock()
	if calls != signals.MaxDepth {
		t.Errorf("handler ran %d times, want MaxDepth=%d", calls, signals.MaxDepth)
	}
	wantKind(t, lastErr, errs.Invalid)
}

func TestPublish_NestedFromHandler_DeliveredAfterInnerCommit(t *testing.T) {
	f := newFixture(t)
	var l log
	a := f.scope("alpha", []signals.Name{nameX}, nil)
	b := f.scope("beta", []signals.Name{nameOther}, []signals.Name{nameX})
	g := f.scope("gamma", nil, []signals.Name{nameOther})
	subscribe(t, g, nameOther, l.mark("gamma-delivered"))
	subscribe(t, b, nameX, func(ctx context.Context, _ signals.Signal) error {
		return f.tx(ctx, func(ctx context.Context) error {
			if err := b.Publish(ctx, nameOther, person()); err != nil {
				return err //nolint:wrapcheck // test
			}
			l.add("inner-fn-done")
			return nil
		})
	})
	err := f.tx(context.Background(), func(ctx context.Context) error {
		return a.Publish(ctx, nameX, nil)
	})
	noErr(t, err)
	got := l.all()
	if len(got) != 2 || got[0] != "inner-fn-done" || got[1] != "gamma-delivered" {
		t.Errorf("order = %v", got)
	}
	outer, inner := spansNamed(f.cap, "deliver:alpha.x"), spansNamed(f.cap, "deliver:beta.other")
	if len(outer) != 1 || len(inner) != 1 {
		t.Fatalf("spans: %v", f.cap.Spans())
	}
	if outer[0].Attrs["depth"] != "1" || inner[0].Attrs["depth"] != "2" {
		t.Errorf("depth outer=%q inner=%q, want 1 and 2", outer[0].Attrs["depth"], inner[0].Attrs["depth"])
	}
}

func TestPublish_ConcurrentPublishers(t *testing.T) {
	f := newFixture(t)
	var n atomic.Int64
	a := pair(t, f, func(context.Context, signals.Signal) error { n.Add(1); return nil })
	const workers, each = 8, 25
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < each; i++ {
				if err := f.tx(context.Background(), func(ctx context.Context) error {
					return a.Publish(ctx, nameX, person())
				}); err != nil {
					t.Errorf("publish: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
	if n.Load() != workers*each {
		t.Errorf("handled %d, want %d", n.Load(), workers*each)
	}
}

func TestPublish_PublishedCounterAndProducerSpan(t *testing.T) {
	f := newFixture(t)
	a := pair(t, f, func(context.Context, signals.Signal) error { return nil })
	noErr(t, a.Publish(context.Background(), nameX, person()))
	ms := measures(f.cap, "bms.signals.published")
	if len(ms) != 1 || ms[0].Attrs["signal"] != string(nameX) || ms[0].Attrs["module"] != "alpha" || ms[0].Value != 1 {
		t.Errorf("published measurements = %+v", ms)
	}
	sp := spansNamed(f.cap, "signal:alpha.x")
	if len(sp) != 1 || sp[0].Kind != telemetry.SpanProducer {
		t.Errorf("producer spans = %+v", sp)
	}
}
