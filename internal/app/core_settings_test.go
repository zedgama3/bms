package app

import (
	"context"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/lib/errs"
)

func TestRefresher_StartThenStop_Returns(t *testing.T) {
	a := builtCore(t)
	var rn *runnable
	for i := range a.runnables {
		if a.runnables[i].name == "settings" {
			rn = &a.runnables[i]
		}
	}
	if rn == nil {
		t.Fatal("the settings part registered no runnable")
	}
	if err := rn.start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rn.stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}
}

func TestRefresher_StopBeforeStart_IsNil(t *testing.T) {
	if err := (&refresher{}).stop(context.Background()); err != nil {
		t.Errorf("stop before start: %v", err)
	}
}

// Run is still running when the stop deadline passes: stop reports Timeout and does not hang.
func TestRefresher_StopWithExpiredContext_ReturnsTimeout(t *testing.T) {
	r := &refresher{cancel: func() {}, done: make(chan struct{})} // a Run that never finishes
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if err := r.stop(ctx); !errs.Is(err, errs.Timeout) {
		t.Errorf("err = %v, want Timeout", err)
	}
}
