package timeutil_test

import (
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/timeutil"
)

func utc(y int, mo time.Month, d, h, mi, s int) time.Time {
	return time.Date(y, mo, d, h, mi, s, 0, time.UTC)
}

func TestCheckBucket_NonDivisor_ReturnsInvalid(t *testing.T) {
	tests := []struct {
		d       time.Duration
		invalid bool
	}{
		{0, true}, {-time.Second, true}, {7 * time.Hour, true}, {7 * time.Minute, true},
		{25 * time.Hour, true}, {48 * time.Hour, true}, {5*time.Hour + 1, true}, {time.Nanosecond * 7, true},
		{time.Second, false}, {5 * time.Second, false}, {time.Minute, false}, {15 * time.Minute, false},
		{time.Hour, false}, {6 * time.Hour, false}, {24 * time.Hour, false}, {time.Nanosecond, false},
	}
	for _, tc := range tests {
		t.Run(tc.d.String(), func(t *testing.T) {
			err := timeutil.CheckBucket(tc.d)
			if tc.invalid != errs.Is(err, errs.Invalid) || (!tc.invalid && err != nil) {
				t.Errorf("CheckBucket(%v) = %v, invalid want %v", tc.d, err, tc.invalid)
			}
		})
	}
}

func TestCheckRange_ZeroOrInverted_ReturnsInvalid(t *testing.T) {
	a, b := utc(2026, 1, 1, 0, 0, 0), utc(2026, 1, 2, 0, 0, 0)
	tests := []struct {
		name       string
		start, end time.Time
		invalid    bool
	}{
		{"valid", a, b, false},
		{"one nanosecond", a, a.Add(1), false},
		{"zero start", time.Time{}, b, true},
		{"zero end", a, time.Time{}, true},
		{"both zero", time.Time{}, time.Time{}, true},
		{"equal", a, a, true},
		{"inverted", b, a, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := timeutil.CheckRange(tc.start, tc.end)
			if tc.invalid != errs.Is(err, errs.Invalid) || (!tc.invalid && err != nil) {
				t.Errorf("err = %v, invalid want %v", err, tc.invalid)
			}
		})
	}
}

func TestTruncate_AlignsToMidnightUTC(t *testing.T) {
	tests := []struct {
		name   string
		in     time.Time
		bucket time.Duration
		want   time.Time
	}{
		{"1m", utc(2026, 3, 4, 5, 6, 7), time.Minute, utc(2026, 3, 4, 5, 6, 0)},
		{"5m", utc(2026, 3, 4, 5, 9, 59), 5 * time.Minute, utc(2026, 3, 4, 5, 5, 0)},
		{"1h", utc(2026, 3, 4, 5, 59, 59), time.Hour, utc(2026, 3, 4, 5, 0, 0)},
		{"6h", utc(2026, 3, 4, 17, 0, 0), 6 * time.Hour, utc(2026, 3, 4, 12, 0, 0)},
		{"24h", utc(2026, 3, 4, 23, 59, 59), 24 * time.Hour, utc(2026, 3, 4, 0, 0, 0)},
		{"on boundary", utc(2026, 3, 4, 5, 0, 0), time.Hour, utc(2026, 3, 4, 5, 0, 0)},
		{"30s", utc(2026, 3, 4, 5, 0, 45), 30 * time.Second, utc(2026, 3, 4, 5, 0, 30)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := timeutil.Truncate(tc.in, tc.bucket)
			if err != nil {
				t.Fatal(err)
			}
			if !got.Equal(tc.want) || got.Location() != time.UTC {
				t.Errorf("got %v (%v), want %v UTC", got, got.Location(), tc.want)
			}
		})
	}
}

func TestTruncate_NonUTCInputReturnsUTC(t *testing.T) {
	zone := time.FixedZone("X", 5*3600+30*60)      // +05:30
	in := time.Date(2026, 3, 4, 1, 10, 0, 0, zone) // 2026-03-03 19:40 UTC
	got, err := timeutil.Truncate(in, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if want := utc(2026, 3, 3, 0, 0, 0); !got.Equal(want) || got.Location() != time.UTC {
		t.Errorf("got %v (%v), want %v", got, got.Location(), want)
	}
	got, err = timeutil.Truncate(in, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if want := utc(2026, 3, 3, 19, 0, 0); !got.Equal(want) || got.Location() != time.UTC {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestTruncate_BeforeEpochFloors(t *testing.T) {
	in := utc(1969, 12, 31, 23, 59, 30)
	got, err := timeutil.Truncate(in, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if want := utc(1969, 12, 31, 23, 59, 0); !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
	got, err = timeutil.Truncate(in, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if want := utc(1969, 12, 31, 0, 0, 0); !got.Equal(want) {
		t.Errorf("got %v, want %v (floor, not toward zero)", got, want)
	}
}

func TestTruncate_BadBucket_ReturnsInvalid(t *testing.T) {
	for _, d := range []time.Duration{0, -1, 7 * time.Hour} {
		if _, err := timeutil.Truncate(utc(2026, 1, 1, 0, 0, 0), d); !errs.Is(err, errs.Invalid) {
			t.Errorf("Truncate bucket %v: err = %v, want Invalid", d, err)
		}
	}
}

func TestAlign_EndOnBoundaryUnchanged(t *testing.T) {
	tests := []struct {
		name       string
		start, end time.Time
		ws, we     time.Time
	}{
		{"expands both", utc(2026, 1, 1, 5, 7, 0), utc(2026, 1, 1, 6, 1, 0), utc(2026, 1, 1, 5, 0, 0), utc(2026, 1, 1, 7, 0, 0)},
		{"end on boundary", utc(2026, 1, 1, 5, 7, 0), utc(2026, 1, 1, 6, 0, 0), utc(2026, 1, 1, 5, 0, 0), utc(2026, 1, 1, 6, 0, 0)},
		{"both on boundary", utc(2026, 1, 1, 5, 0, 0), utc(2026, 1, 1, 6, 0, 0), utc(2026, 1, 1, 5, 0, 0), utc(2026, 1, 1, 6, 0, 0)},
		{"inside one bucket", utc(2026, 1, 1, 5, 10, 0), utc(2026, 1, 1, 5, 20, 0), utc(2026, 1, 1, 5, 0, 0), utc(2026, 1, 1, 6, 0, 0)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, e, err := timeutil.Align(tc.start, tc.end, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			if !s.Equal(tc.ws) || !e.Equal(tc.we) || s.Location() != time.UTC || e.Location() != time.UTC {
				t.Errorf("got [%v,%v), want [%v,%v) UTC", s, e, tc.ws, tc.we)
			}
		})
	}
}

func TestAlign_Errors(t *testing.T) {
	a, b := utc(2026, 1, 1, 0, 0, 0), utc(2026, 1, 2, 0, 0, 0)
	if _, _, err := timeutil.Align(b, a, time.Hour); !errs.Is(err, errs.Invalid) {
		t.Errorf("inverted: %v", err)
	}
	if _, _, err := timeutil.Align(a, b, 7*time.Hour); !errs.Is(err, errs.Invalid) {
		t.Errorf("bad bucket: %v", err)
	}
}

func TestBuckets_CoversHalfOpenRange(t *testing.T) {
	tests := []struct {
		name       string
		start, end time.Time
		bucket     time.Duration
		first      time.Time
		last       time.Time
		n          int
	}{
		{"aligned", utc(2026, 1, 1, 0, 0, 0), utc(2026, 1, 1, 3, 0, 0), time.Hour, utc(2026, 1, 1, 0, 0, 0), utc(2026, 1, 1, 2, 0, 0), 3},
		{"unaligned start and end", utc(2026, 1, 1, 0, 30, 0), utc(2026, 1, 1, 2, 30, 0), time.Hour, utc(2026, 1, 1, 0, 0, 0), utc(2026, 1, 1, 2, 0, 0), 3},
		{"single", utc(2026, 1, 1, 0, 10, 0), utc(2026, 1, 1, 0, 20, 0), time.Hour, utc(2026, 1, 1, 0, 0, 0), utc(2026, 1, 1, 0, 0, 0), 1},
		{"end just after boundary", utc(2026, 1, 1, 0, 0, 0), utc(2026, 1, 1, 1, 0, 1), time.Hour, utc(2026, 1, 1, 0, 0, 0), utc(2026, 1, 1, 1, 0, 0), 2},
		{"day", utc(2026, 1, 1, 0, 0, 0), utc(2026, 1, 2, 0, 0, 0), 24 * time.Hour, utc(2026, 1, 1, 0, 0, 0), utc(2026, 1, 1, 0, 0, 0), 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := timeutil.Buckets(tc.start, tc.end, tc.bucket)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != tc.n {
				t.Fatalf("len = %d, want %d", len(got), tc.n)
			}
			if !got[0].Equal(tc.first) || !got[len(got)-1].Equal(tc.last) {
				t.Errorf("first/last = %v / %v, want %v / %v", got[0], got[len(got)-1], tc.first, tc.last)
			}
			for i, g := range got {
				if g.Location() != time.UTC {
					t.Errorf("[%d] not UTC", i)
				}
				if i > 0 && g.Sub(got[i-1]) != tc.bucket {
					t.Errorf("[%d] step = %v", i, g.Sub(got[i-1]))
				}
			}
		})
	}
}

func TestBuckets_NonUTCInput(t *testing.T) {
	zone := time.FixedZone("X", -7*3600)
	s := time.Date(2026, 1, 1, 17, 0, 0, 0, zone) // 00:00 UTC Jan 2
	e := time.Date(2026, 1, 1, 19, 0, 0, 0, zone) // 02:00 UTC
	got, err := timeutil.Buckets(s, e, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !got[0].Equal(utc(2026, 1, 2, 0, 0, 0)) || got[0].Location() != time.UTC {
		t.Errorf("got %v", got)
	}
}

func TestBuckets_TooMany_ReturnsInvalid(t *testing.T) {
	s := utc(2026, 1, 1, 0, 0, 0)
	// Exactly MaxBuckets is allowed; one more is not.
	if got, err := timeutil.Buckets(s, s.Add(timeutil.MaxBuckets*time.Second), time.Second); err != nil || len(got) != timeutil.MaxBuckets {
		t.Errorf("exactly MaxBuckets: len=%d err=%v", len(got), err)
	}
	got, err := timeutil.Buckets(s, s.Add((timeutil.MaxBuckets+1)*time.Second), time.Second)
	if !errs.Is(err, errs.Invalid) {
		t.Fatalf("err = %v, want Invalid", err)
	}
	if got != nil {
		t.Error("slice returned with error")
	}
}

func TestBuckets_Errors(t *testing.T) {
	a, b := utc(2026, 1, 1, 0, 0, 0), utc(2026, 1, 2, 0, 0, 0)
	if _, err := timeutil.Buckets(b, a, time.Hour); !errs.Is(err, errs.Invalid) {
		t.Errorf("inverted: %v", err)
	}
	if _, err := timeutil.Buckets(time.Time{}, b, time.Hour); !errs.Is(err, errs.Invalid) {
		t.Errorf("zero start: %v", err)
	}
	if _, err := timeutil.Buckets(a, b, 7*time.Hour); !errs.Is(err, errs.Invalid) {
		t.Errorf("bad bucket: %v", err)
	}
}

func TestBucketFor_PicksSmallestFitting(t *testing.T) {
	ladder := []time.Duration{
		time.Second, 5 * time.Second, 10 * time.Second, 15 * time.Second, 30 * time.Second,
		time.Minute, 5 * time.Minute, 10 * time.Minute, 15 * time.Minute, 30 * time.Minute,
		time.Hour, 2 * time.Hour, 3 * time.Hour, 6 * time.Hour, 12 * time.Hour, 24 * time.Hour,
	}
	start := utc(2026, 1, 1, 0, 0, 0) // aligned to every rung
	// A range of exactly k buckets of rung r with budget k must choose r (and not smaller).
	for i, r := range ladder {
		t.Run(r.String(), func(t *testing.T) {
			const k = 10
			end := start.Add(k * r)
			got, err := timeutil.BucketFor(start, end, k)
			if err != nil {
				t.Fatal(err)
			}
			// A smaller rung could fit if it produced <= k points; it cannot, since each
			// smaller rung gives strictly more than k buckets over this range.
			if got != r {
				t.Errorf("BucketFor = %v, want %v (index %d)", got, r, i)
			}
		})
	}
	t.Run("tiny range gets 1s", func(t *testing.T) {
		got, err := timeutil.BucketFor(start, start.Add(10*time.Second), 100)
		if err != nil || got != time.Second {
			t.Errorf("got %v, %v", got, err)
		}
	})
	t.Run("one day at 300 points", func(t *testing.T) {
		got, err := timeutil.BucketFor(start, start.Add(24*time.Hour), 300)
		if err != nil || got != 5*time.Minute { // 288 buckets
			t.Errorf("got %v, %v want 5m", got, err)
		}
	})
	t.Run("unaligned range counts the truncated bucket", func(t *testing.T) {
		// [00:00:30, 00:02:30) with 1m buckets covers 3 buckets (00:00, 00:01, 00:02).
		s, e := start.Add(30*time.Second), start.Add(150*time.Second)
		got, err := timeutil.BucketFor(s, e, 2)
		if err != nil {
			t.Fatal(err)
		}
		n, err := timeutil.Buckets(s, e, got)
		if err != nil {
			t.Fatal(err)
		}
		if len(n) > 2 {
			t.Errorf("chosen bucket %v yields %d points > 2", got, len(n))
		}
		if got == time.Minute {
			t.Error("picked 1m although it yields 3 points for budget 2")
		}
	})
}

func TestBucketFor_NoBudget_ReturnsInvalid(t *testing.T) {
	s := utc(2026, 1, 1, 0, 0, 0)
	for _, n := range []int{0, -1, -100} {
		if _, err := timeutil.BucketFor(s, s.Add(time.Hour), n); !errs.Is(err, errs.Invalid) {
			t.Errorf("maxPoints %d: err = %v, want Invalid", n, err)
		}
	}
}

func TestBucketFor_RangeTooLong_ReturnsInvalid(t *testing.T) {
	s := utc(2024, 1, 1, 0, 0, 0)
	_, err := timeutil.BucketFor(s, s.AddDate(2, 0, 0), 300)
	if !errs.Is(err, errs.Invalid) {
		t.Fatalf("err = %v, want Invalid", err)
	}
	if got, err := timeutil.BucketFor(s, s.Add(24*time.Hour), 1); err != nil || got != 24*time.Hour {
		t.Errorf("exact fit at 24h: %v, %v", got, err)
	}
}

func TestBucketFor_BadRange_ReturnsInvalid(t *testing.T) {
	s := utc(2026, 1, 1, 0, 0, 0)
	if _, err := timeutil.BucketFor(s, s, 10); !errs.Is(err, errs.Invalid) {
		t.Errorf("empty range: %v", err)
	}
	if _, err := timeutil.BucketFor(time.Time{}, s, 10); !errs.Is(err, errs.Invalid) {
		t.Errorf("zero start: %v", err)
	}
}

func TestDay_Boundaries(t *testing.T) {
	zone := time.FixedZone("X", 5*3600)
	tests := []struct {
		name   string
		in     time.Time
		ws, we time.Time
	}{
		{"midday", utc(2026, 3, 4, 12, 0, 0), utc(2026, 3, 4, 0, 0, 0), utc(2026, 3, 5, 0, 0, 0)},
		{"midnight", utc(2026, 3, 4, 0, 0, 0), utc(2026, 3, 4, 0, 0, 0), utc(2026, 3, 5, 0, 0, 0)},
		{"last nanosecond", utc(2026, 3, 4, 23, 59, 59).Add(999999999), utc(2026, 3, 4, 0, 0, 0), utc(2026, 3, 5, 0, 0, 0)},
		{"month end", utc(2026, 1, 31, 10, 0, 0), utc(2026, 1, 31, 0, 0, 0), utc(2026, 2, 1, 0, 0, 0)},
		{"year end", utc(2026, 12, 31, 10, 0, 0), utc(2026, 12, 31, 0, 0, 0), utc(2027, 1, 1, 0, 0, 0)},
		{"non-UTC maps to UTC day", time.Date(2026, 3, 4, 2, 0, 0, 0, zone), utc(2026, 3, 3, 0, 0, 0), utc(2026, 3, 4, 0, 0, 0)},
		{"before epoch", utc(1969, 12, 31, 12, 0, 0), utc(1969, 12, 31, 0, 0, 0), utc(1970, 1, 1, 0, 0, 0)},
		{"zero time", time.Time{}, utc(1, 1, 1, 0, 0, 0), utc(1, 1, 2, 0, 0, 0)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, e := timeutil.Day(tc.in)
			if !s.Equal(tc.ws) || !e.Equal(tc.we) || s.Location() != time.UTC || e.Location() != time.UTC {
				t.Errorf("Day = [%v,%v), want [%v,%v) UTC", s, e, tc.ws, tc.we)
			}
		})
	}
}

func TestBucketFor_HugeRange_ReturnsInvalid(t *testing.T) {
	// Longer than the 292 years a time.Duration can hold: must fail, not saturate to a bucket.
	got, err := timeutil.BucketFor(utc(2, 1, 1, 0, 0, 0), utc(9999, 1, 1, 0, 0, 0), 1_000_000)
	if !errs.Is(err, errs.Invalid) {
		t.Fatalf("got %v, %v; want Invalid", got, err)
	}
}
