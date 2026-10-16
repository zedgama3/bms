package timeutil

import (
	"testing"
	"time"
)

func TestInternalLadderDividesDay(t *testing.T) {
	for _, b := range ladder {
		if err := CheckBucket(b); err != nil {
			t.Errorf("ladder entry %s: %v", b, err)
		}
	}
}

func TestInternalTruncateZeroAndPreEpoch(t *testing.T) {
	z, err := Truncate(time.Time{}, time.Hour)
	if err != nil || !z.Equal(time.Time{}) {
		t.Errorf("zero time: got %v, %v", z, err)
	}
	pre := time.Date(1969, 12, 31, 23, 59, 59, 5, time.UTC)
	got, err := Truncate(pre, time.Minute)
	if want := time.Date(1969, 12, 31, 23, 59, 0, 0, time.UTC); err != nil || !got.Equal(want) {
		t.Errorf("pre-epoch: got %v, %v", got, err)
	}
}

func TestInternalCountHugeRangeDoesNotSaturateLow(t *testing.T) {
	s := time.Date(2, 1, 1, 0, 0, 0, 0, time.UTC)
	e := time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := BucketFor(s, e, 1_000_000); err == nil {
		t.Error("BucketFor over ~10k years with 1M points: want error")
	}
	if got := count(s, e, time.Nanosecond); got < MaxBuckets {
		t.Errorf("count at 1ns = %d, want huge", got)
	}
	if got := count(time.Date(2026, 1, 1, 10, 7, 0, 0, time.UTC), time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), time.Hour); got != 14 {
		t.Errorf("count = %d, want 14", got)
	}
}
