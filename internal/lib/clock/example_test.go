package clock_test

import (
	"context"
	"fmt"
	"time"

	"github.com/zedgama3/bms/internal/lib/clock"
)

func ExampleFake() {
	f := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	timer := f.NewTimer(time.Minute)
	if err := f.Advance(time.Minute); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(<-timer.C())
	// Output: 2026-01-01 00:01:00 +0000 UTC
}

func ExampleWait() {
	f := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	done := make(chan error, 1)
	go func() { done <- clock.Wait(context.Background(), f, time.Second) }()
	if err := f.BlockUntil(context.Background(), 1); err != nil {
		fmt.Println(err)
		return
	}
	if err := f.Advance(time.Second); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(<-done)
	// Output: <nil>
}
