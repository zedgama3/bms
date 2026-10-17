package signalstest_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/signals/signalstest"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

const (
	nameX = signals.Name("alpha.x")
	nameY = signals.Name("alpha.y")
	nameZ = signals.Name("beta.z")
)

var errBoom = errors.New("boom")

func wantKind(t *testing.T, err error, k errs.Kind) {
	t.Helper()
	if err == nil || !errs.Is(err, k) {
		t.Fatalf("want kind %v, got %v", k, err)
	}
}

func noErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func newFake(t *testing.T, pub, sub []signals.Name) *signalstest.Fake {
	t.Helper()
	f, err := signalstest.NewFake("alpha", pub, sub)
	if err != nil {
		t.Fatalf("NewFake: %v", err)
	}
	return f
}

func payload() signals.Payload {
	return signals.NewPayload(signals.Ref("person", model.NewID[model.PersonID]()))
}

func validSignal(n signals.Name) signals.Signal {
	return signals.Signal{ID: model.NewID[signals.SignalID](), Name: n, At: time.Unix(1_700_000_000, 0).UTC(), Payload: payload()}
}

func nop(context.Context, signals.Signal) error { return nil }

func TestFake_NewFake_BadOwnerOrLists_ReturnsInvalid(t *testing.T) {
	tests := []struct {
		name     string
		owner    string
		pub, sub []signals.Name
	}{
		{"empty owner", "", nil, nil},
		{"short owner", "a", nil, nil},
		{"upper owner", "Alpha", nil, nil},
		{"publishes other publisher", "alpha", []signals.Name{nameZ}, nil},
		{"publishes invalid", "alpha", []signals.Name{"alpha"}, nil},
		{"publishes repeated", "alpha", []signals.Name{nameX, nameX}, nil},
		{"subscribes invalid", "alpha", nil, []signals.Name{"x"}},
		{"subscribes repeated", "alpha", nil, []signals.Name{nameZ, nameZ}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, err := signalstest.NewFake(tc.owner, tc.pub, tc.sub)
			wantKind(t, err, errs.Invalid)
			if f != nil {
				t.Error("fake returned with error")
			}
		})
	}
}

func TestFake_SameScopingAsDispatcher(t *testing.T) {
	f := newFake(t, []signals.Name{nameX}, []signals.Name{nameZ})
	ctx := context.Background()
	// F16
	wantKind(t, f.Publish(ctx, nameY, nil), errs.Invalid)
	// F17
	wantKind(t, f.Publish(ctx, nameX, signals.Payload{"person": {}}), errs.Invalid)
	// F12, F13, F14
	wantKind(t, f.Subscribe(nameY, nop), errs.Invalid)
	wantKind(t, f.Subscribe(nameZ, nil), errs.Invalid)
	noErr(t, f.Subscribe(nameZ, nop))
	wantKind(t, f.Subscribe(nameZ, nop), errs.Conflict)
	if got := f.Published(); len(got) != 0 {
		t.Errorf("refused publishes were recorded: %v", got)
	}
}

func TestFake_PublishRecordsAfterCommitOnly(t *testing.T) {
	f := newFake(t, []signals.Name{nameX}, nil)
	d := dbtest.NewFake()
	ctx := context.Background()

	err := d.Transact(ctx, db.Write, func(ctx context.Context) error {
		noErr(t, f.Publish(ctx, nameX, payload()))
		if len(f.Published()) != 0 {
			t.Error("recorded before commit")
		}
		return errBoom
	})
	if err == nil {
		t.Fatal("expected rollback")
	}
	if got := f.Published(); len(got) != 0 {
		t.Fatalf("rollback recorded %v", got)
	}

	noErr(t, d.Transact(ctx, db.Write, func(ctx context.Context) error {
		return f.Publish(ctx, nameX, payload())
	}))
	got := f.Published()
	if len(got) != 1 {
		t.Fatalf("published = %v", got)
	}
	if got[0].ID.IsZero() || got[0].At.IsZero() || got[0].Name != nameX {
		t.Errorf("signal = %+v", got[0])
	}
	noErr(t, got[0].Validate())

	// outside a transaction the hook runs at once
	noErr(t, f.Publish(ctx, nameX, nil))
	if len(f.Published()) != 2 {
		t.Errorf("published = %v", f.Published())
	}
}

func TestFake_PublishNeverDelivers(t *testing.T) {
	f := newFake(t, []signals.Name{nameX}, []signals.Name{nameX})
	called := false
	noErr(t, f.Subscribe(nameX, func(context.Context, signals.Signal) error { called = true; return nil }))
	noErr(t, f.Publish(context.Background(), nameX, nil))
	if called {
		t.Error("Publish delivered")
	}
}

func TestFake_PublishedReturnsCopy(t *testing.T) {
	f := newFake(t, []signals.Name{nameX}, nil)
	noErr(t, f.Publish(context.Background(), nameX, nil))
	got := f.Published()
	got[0].Name = "zzz.zzz"
	got = append(got, signals.Signal{})
	_ = got
	if f.Published()[0].Name != nameX || len(f.Published()) != 1 {
		t.Error("Published exposes internal state")
	}
}

func TestFake_DeliverReturnsHandlerErrorUnchanged(t *testing.T) {
	f := newFake(t, nil, []signals.Name{nameZ})
	want := errs.E("alpha.h", errs.Unavailable, errBoom)
	var inDelivery, hasDL bool
	var got signals.Signal
	noErr(t, f.Subscribe(nameZ, func(ctx context.Context, s signals.Signal) error {
		inDelivery = signals.InDelivery(ctx)
		_, hasDL = ctx.Deadline()
		got = s
		return want
	}))
	s := validSignal(nameZ)
	err := f.Deliver(context.Background(), s)
	if !errors.Is(err, want) {
		t.Fatalf("error changed: %v", err)
	}
	wantKind(t, err, errs.Unavailable)
	if !inDelivery {
		t.Error("InDelivery false in handler")
	}
	if hasDL {
		t.Error("Deliver added a deadline")
	}
	if got.ID != s.ID {
		t.Error("handler got another signal")
	}
}

func TestFakeDeliver_Table(t *testing.T) {
	f := newFake(t, nil, []signals.Name{nameZ, nameY})
	noErr(t, f.Subscribe(nameZ, func(context.Context, signals.Signal) error { panic("kaboom") }))
	tests := []struct {
		name string
		s    signals.Signal
		want errs.Kind
	}{
		{"invalid signal", signals.Signal{}, errs.Invalid},
		{"no handler", validSignal(nameY), errs.NotFound},
		{"unsubscribed name", validSignal("gamma.q"), errs.NotFound},
		{"panic", validSignal(nameZ), errs.Internal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wantKind(t, f.Deliver(context.Background(), tc.s), tc.want)
		})
	}
}

func TestFake_DeliverNilHandlerErrorIsNil(t *testing.T) {
	f := newFake(t, nil, []signals.Name{nameZ})
	noErr(t, f.Subscribe(nameZ, nop))
	noErr(t, f.Deliver(context.Background(), validSignal(nameZ)))
}

func TestFake_HandlersSorted(t *testing.T) {
	f := newFake(t, nil, []signals.Name{"c.z", "a.b", "b.a", "a.a"})
	if len(f.Handlers()) != 0 {
		t.Error("handlers before Subscribe")
	}
	for _, n := range []signals.Name{"c.z", "a.b", "b.a"} {
		noErr(t, f.Subscribe(n, nop))
	}
	got := f.Handlers()
	want := []signals.Name{"a.b", "b.a", "c.z"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestFake_SafeForConcurrentUse(t *testing.T) {
	f := newFake(t, []signals.Name{nameX}, []signals.Name{nameZ})
	noErr(t, f.Subscribe(nameZ, nop))
	var wg sync.WaitGroup
	const workers, each = 8, 20
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < each; i++ {
				if err := f.Publish(context.Background(), nameX, payload()); err != nil {
					t.Errorf("publish: %v", err)
				}
				_ = f.Published()
				_ = f.Handlers()
				if err := f.Deliver(context.Background(), validSignal(nameZ)); err != nil {
					t.Errorf("deliver: %v", err)
				}
			}
		}()
	}
	wg.Wait()
	if n := len(f.Published()); n != workers*each {
		t.Errorf("published %d, want %d", n, workers*each)
	}
}
