// Package timeutil does bucket math in UTC: aligning times to buckets, enumerating
// buckets, choosing a bucket for a point budget, and day boundaries. Buckets must divide
// 24h, so they tile every UTC day exactly and never straddle a day boundary. Local time,
// DST and calendar months are out of scope.
// It depends only on the standard library and lib/errs.
// Ported from Sojourn (D11).
package timeutil

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/zedgama3/bms/internal/lib/errs"
)

// MaxBuckets bounds the slices Buckets returns.
const MaxBuckets = 100_000

const day = 24 * time.Hour

// ladder is the standard set of buckets BucketFor chooses from, smallest first. Every
// entry divides 24h.
var ladder = []time.Duration{
	time.Second, 5 * time.Second, 10 * time.Second, 15 * time.Second, 30 * time.Second,
	time.Minute, 5 * time.Minute, 10 * time.Minute, 15 * time.Minute, 30 * time.Minute,
	time.Hour, 2 * time.Hour, 3 * time.Hour, 6 * time.Hour, 12 * time.Hour, day,
}

// CheckBucket fails with errs.Invalid unless 0 < d and 24h is a whole multiple of d.
func CheckBucket(d time.Duration) error {
	if d <= 0 || day%d != 0 {
		return errs.E("timeutil.CheckBucket", errs.Invalid,
			fmt.Errorf("bucket must be positive and divide 24h, got %s", d))
	}
	return nil
}

// CheckRange fails with errs.Invalid if start or end is the zero time, or !start.Before(end).
func CheckRange(start, end time.Time) error {
	const op = "timeutil.CheckRange"
	if start.IsZero() || end.IsZero() {
		return errs.E(op, errs.Invalid, errors.New("start and end must be set"))
	}
	if !start.Before(end) {
		return errs.E(op, errs.Invalid, errors.New("start must be before end"))
	}
	return nil
}

// truncate rounds t down to a multiple of bucket since midnight UTC. bucket must be valid.
// Working from the day's midnight avoids UnixNano overflow and floors correctly before the
// epoch and for the zero time.
func truncate(t time.Time, bucket time.Duration) time.Time {
	t = t.UTC()
	y, m, d := t.Date()
	midnight := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	off := t.Sub(midnight)
	return midnight.Add(off - off%bucket)
}

// Truncate returns t rounded down to a bucket boundary, in UTC. Boundaries are aligned to
// midnight UTC, so every valid bucket tiles the day exactly. Fails per CheckBucket.
func Truncate(t time.Time, bucket time.Duration) (time.Time, error) {
	if err := CheckBucket(bucket); err != nil {
		return time.Time{}, fmt.Errorf("timeutil.Truncate: %w", err)
	}
	return truncate(t, bucket), nil
}

func alignedEnd(end time.Time, bucket time.Duration) time.Time {
	e := truncate(end, bucket)
	if e.Before(end.UTC()) {
		e = e.Add(bucket)
	}
	return e
}

// Align expands [start, end) outward to bucket boundaries: start rounded down, end rounded
// up (an end already on a boundary is unchanged). Fails per CheckRange and CheckBucket.
func Align(start, end time.Time, bucket time.Duration) (time.Time, time.Time, error) {
	if err := check(start, end, bucket); err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("timeutil.Align: %w", err)
	}
	return truncate(start, bucket), alignedEnd(end, bucket), nil
}

func check(start, end time.Time, bucket time.Duration) error {
	if err := CheckRange(start, end); err != nil {
		return err
	}
	return CheckBucket(bucket)
}

// splitDay returns the Unix day number of t's UTC date and t's offset into that day.
// t must already be UTC. Unix seconds of a midnight are an exact multiple of 86400.
func splitDay(t time.Time) (dayNum int64, off time.Duration) {
	y, m, d := t.Date()
	midnight := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return midnight.Unix() / 86400, t.Sub(midnight)
}

// count returns how many buckets cover [start, end). It works in whole days plus an offset
// so it cannot overflow Duration for long ranges; if the true count would not fit in an
// int64 it returns a saturated, still enormous, value.
func count(start, end time.Time, bucket time.Duration) int64 {
	ds, os := splitDay(truncate(start, bucket))
	de, oe := splitDay(alignedEnd(end, bucket))
	perDay := int64(day / bucket)
	days := de - ds
	if days > 0 && perDay > math.MaxInt64/2/days {
		return math.MaxInt64 / 2
	}
	return days*perDay + int64((oe-os)/bucket)
}

// Buckets returns the bucket start times covering [start, end): Truncate(start) stepping
// by bucket while < end. Fails per CheckRange and CheckBucket, and with errs.Invalid if
// more than MaxBuckets would be returned.
func Buckets(start, end time.Time, bucket time.Duration) ([]time.Time, error) {
	const op = "timeutil.Buckets"
	if err := check(start, end, bucket); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	n := count(start, end, bucket)
	if n > MaxBuckets {
		return nil, errs.E(op, errs.Invalid,
			fmt.Errorf("range needs more than %d buckets of %s; use a larger bucket", MaxBuckets, bucket))
	}
	first := truncate(start, bucket)
	out := make([]time.Time, 0, n)
	for i := range n {
		out = append(out, first.Add(time.Duration(i)*bucket))
	}
	return out, nil
}

// BucketFor returns the smallest bucket on the standard ladder (1s, 5s, 10s, 15s, 30s, 1m,
// 5m, 10m, 15m, 30m, 1h, 2h, 3h, 6h, 12h, 24h) for which
// len(Buckets(start, end, bucket)) <= maxPoints. Fails per CheckRange, with errs.Invalid if
// maxPoints < 1, and with errs.Invalid if even 24h exceeds maxPoints.
func BucketFor(start, end time.Time, maxPoints int) (time.Duration, error) {
	const op = "timeutil.BucketFor"
	if err := CheckRange(start, end); err != nil {
		return 0, fmt.Errorf("%s: %w", op, err)
	}
	if maxPoints < 1 {
		return 0, errs.E(op, errs.Invalid, fmt.Errorf("maxPoints must be at least 1, got %d", maxPoints))
	}
	for _, b := range ladder {
		if count(start, end, b) <= int64(maxPoints) {
			return b, nil
		}
	}
	return 0, errs.E(op, errs.Invalid,
		fmt.Errorf("range needs more than %d points even at 24h buckets; shorten the range or raise the budget", maxPoints))
}

// Day returns the UTC day containing t as [start, end). Cannot fail: 24h is always a valid
// bucket and the zero time has a day.
func Day(t time.Time) (start, end time.Time) {
	start = truncate(t, day)
	return start, start.Add(day)
}
