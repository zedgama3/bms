package timeutil_test

import (
	"fmt"
	"time"

	"github.com/zedgama3/bms/internal/lib/timeutil"
)

func ExampleBucketFor() {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	b, err := timeutil.BucketFor(start, start.Add(6*time.Hour), 300)
	fmt.Println(b, err)
	// Output: 5m0s <nil>
}

func ExampleAlign() {
	s := time.Date(2026, 1, 1, 10, 7, 30, 0, time.UTC)
	e := time.Date(2026, 1, 1, 10, 31, 0, 0, time.UTC)
	as, ae, err := timeutil.Align(s, e, 15*time.Minute)
	fmt.Println(as.Format(time.TimeOnly), ae.Format(time.TimeOnly), err)
	// Output: 10:00:00 10:45:00 <nil>
}
