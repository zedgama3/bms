package signals_test

import (
	"context"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
)

func TestPublish_AfterClose_ReturnsUnavailable(t *testing.T) {
	f := newFixture(t)
	var l log
	a := pair(t, f, l.mark("beta"))
	noErr(t, f.disp.Close(context.Background()))
	wantKind(t, a.Publish(context.Background(), nameX, nil), errs.Unavailable)
	err := f.tx(context.Background(), func(ctx context.Context) error { return a.Publish(ctx, nameX, nil) })
	wantKind(t, err, errs.Unavailable)
	if len(l.all()) != 0 {
		t.Error("delivered after Close")
	}
}

func TestClose_Twice_IsNil(t *testing.T) {
	f := newFixture(t)
	noErr(t, f.disp.Close(context.Background()))
	noErr(t, f.disp.Close(context.Background()))
}

func TestClose_InFlight_ReturnsTimeout(t *testing.T) {
	f := newFixture(t)
	started := make(chan struct{})
	release := make(chan struct{})
	a := pair(t, f, func(context.Context, signals.Signal) error {
		close(started)
		<-release
		return nil
	})
	pubDone := make(chan error, 1)
	go func() { pubDone <- a.Publish(context.Background(), nameX, nil) }()

	select {
	case <-started:
	case <-time.After(testTimeout):
		t.Fatal("handler never started")
	}

	dctx, dcancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer dcancel()
	wantKind(t, f.disp.Close(dctx), errs.Timeout)

	cctx, cancel := context.WithCancel(context.Background())
	cancel()
	wantKind(t, f.disp.Close(cctx), errs.Canceled)

	// Close refused new publishes from the first call on
	wantKind(t, a.Publish(context.Background(), nameX, nil), errs.Unavailable)

	close(release)
	select {
	case err := <-pubDone:
		noErr(t, err)
	case <-time.After(testTimeout):
		t.Fatal("publish never returned")
	}
	// the in-flight delivery finished on its own and was counted
	if outcomes(f.cap)["beta"] != "ok" {
		t.Errorf("outcomes = %v", outcomes(f.cap))
	}
	ctx, cancel2 := context.WithTimeout(context.Background(), testTimeout)
	defer cancel2()
	noErr(t, f.disp.Close(ctx))
}

func TestClose_WaitsForInFlight(t *testing.T) {
	f := newFixture(t)
	started := make(chan struct{})
	release := make(chan struct{})
	a := pair(t, f, func(context.Context, signals.Signal) error {
		close(started)
		<-release
		return nil
	})
	pubDone := make(chan error, 1)
	go func() { pubDone <- a.Publish(context.Background(), nameX, nil) }()
	<-started

	closed := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()
		closed <- f.disp.Close(ctx)
	}()
	select {
	case err := <-closed:
		t.Fatalf("Close returned %v while a delivery was in flight", err)
	default:
	}
	close(release)
	noErr(t, <-pubDone)
	noErr(t, <-closed)
}
