package app

import (
	"context"
	"testing"
	"time"
)

func TestSettingsRunnable_StartThenStop_Returns(t *testing.T) {
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
