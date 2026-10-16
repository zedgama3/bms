package retry_test

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/retry"
)

func ExamplePolicy_Delay() {
	p := retry.Default()
	for attempt := 1; attempt <= 8; attempt++ {
		fmt.Println(attempt, p.Delay(attempt, 1))
	}
	// Output:
	// 1 0s
	// 2 500ms
	// 3 1s
	// 4 2s
	// 5 4s
	// 6 8s
	// 7 16s
	// 8 30s
}

func ExampleDo() {
	f := clock.NewFake(time.Unix(0, 0))
	calls := 0
	done := make(chan error, 1)
	go func() {
		done <- retry.Do(context.Background(), f, retry.Default(), func() float64 { return 1 },
			func(context.Context) error {
				calls++
				if calls < 2 {
					return errs.E("example", errs.Unavailable, errors.New("down"))
				}
				return nil
			})
	}()
	if err := f.BlockUntil(context.Background(), 1); err != nil {
		fmt.Println(err)
		return
	}
	if err := f.Advance(500 * time.Millisecond); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(<-done, calls)
	// Output: <nil> 2
}
