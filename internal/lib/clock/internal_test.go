package clock

import (
	"context"
	"testing"
	"time"
)

func TestInternalZeroFakeIsUsable(t *testing.T) {
	var f Fake
	timer := f.NewTimer(time.Second) // notifies with no waiter on BlockUntil
	if err := f.BlockUntil(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	if err := f.Advance(time.Second); err != nil {
		t.Fatal(err)
	}
	select {
	case <-timer.C():
	default:
		t.Error("timer did not fire")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := f.BlockUntil(ctx, 3); err == nil {
		t.Error("want error from canceled ctx")
	}
}
