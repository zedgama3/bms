package model_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

var t0 = time.Date(2026, 8, 1, 8, 0, 0, 0, time.UTC)

func at(h int) time.Time { return t0.Add(time.Duration(h) * time.Hour) }

func rng(a, b int) model.TimeRange { return model.TimeRange{Start: at(a), End: at(b)} }

func fieldNames(err error) map[string]bool {
	m := map[string]bool{}
	for _, fe := range validate.Fields(err) {
		m[fe.Field] = true
	}
	return m
}

func TestTimeRangeValidate_Valid(t *testing.T) {
	if err := rng(0, 4).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestTimeRangeValidate_ZeroStartOrEnd_ReturnsInvalidPerField(t *testing.T) {
	tests := []struct {
		name string
		r    model.TimeRange
		want []string
	}{
		{"zero start", model.TimeRange{End: at(1)}, []string{"start"}},
		{"zero end", model.TimeRange{Start: at(1)}, []string{"end"}},
		{"both zero", model.TimeRange{}, []string{"start", "end"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.r.Validate()
			if !errs.Is(err, errs.Invalid) {
				t.Fatalf("got %v", err)
			}
			got := fieldNames(err)
			if len(got) != len(tc.want) {
				t.Fatalf("fields %v want %v", got, tc.want)
			}
			for _, f := range tc.want {
				if !got[f] {
					t.Errorf("missing field %s", f)
				}
			}
		})
	}
}

func TestTimeRangeValidate_EndNotAfterStart_ReturnsInvalid(t *testing.T) {
	for name, r := range map[string]model.TimeRange{"equal": rng(2, 2), "before": rng(3, 1)} {
		t.Run(name, func(t *testing.T) {
			err := r.Validate()
			if !errs.Is(err, errs.Invalid) {
				t.Fatalf("got %v", err)
			}
			got := fieldNames(err)
			if !got["end"] || got["start"] {
				t.Errorf("fields %v want only end", got)
			}
		})
	}
}

func TestTimeRange_ContainsHalfOpen(t *testing.T) {
	r := rng(1, 3)
	tests := []struct {
		name string
		t    time.Time
		want bool
	}{
		{"start in", at(1), true},
		{"middle", at(2), true},
		{"just before end", at(3).Add(-time.Nanosecond), true},
		{"end out", at(3), false},
		{"before", at(0), false},
		{"after", at(5), false},
		{"other zone same instant as start", at(1).In(time.FixedZone("x", 5*3600)), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := r.Contains(tc.t); got != tc.want {
				t.Errorf("got %v", got)
			}
		})
	}
}

func TestTimeRange_OverlapsTable(t *testing.T) {
	zone := time.FixedZone("plus7", 7*3600)
	inZone := func(r model.TimeRange) model.TimeRange {
		return model.TimeRange{Start: r.Start.In(zone), End: r.End.In(zone)}
	}
	tests := []struct {
		name string
		a, b model.TimeRange
		want bool
	}{
		{"disjoint", rng(0, 1), rng(2, 3), false},
		{"touching", rng(0, 2), rng(2, 4), false},
		{"partial", rng(0, 3), rng(2, 5), true},
		{"nested", rng(0, 6), rng(2, 3), true},
		{"equal", rng(1, 3), rng(1, 3), true},
		{"mixed zones overlapping", rng(0, 3), inZone(rng(2, 5)), true},
		{"mixed zones touching", rng(0, 2), inZone(rng(2, 4)), false},
		{"mixed zones disjoint", inZone(rng(0, 1)), rng(5, 6), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.a.Overlaps(tc.b); got != tc.want {
				t.Errorf("a.Overlaps(b) = %v", got)
			}
			if got := tc.b.Overlaps(tc.a); got != tc.want {
				t.Errorf("b.Overlaps(a) = %v", got)
			}
		})
	}
}

func TestTimeRange_Duration(t *testing.T) {
	if got := rng(1, 5).Duration(); got != 4*time.Hour {
		t.Error(got)
	}
	z := time.FixedZone("x", -8*3600)
	r := model.TimeRange{Start: at(0).In(z), End: at(2)}
	if r.Duration() != 2*time.Hour {
		t.Error(r.Duration())
	}
}

func TestTimeRangeJSON_RoundTrip(t *testing.T) {
	r := model.TimeRange{Start: t0, End: t0.Add(90 * time.Minute)}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if len(m) != 2 || m["start"] != "2026-08-01T08:00:00Z" || m["end"] != "2026-08-01T09:30:00Z" {
		t.Fatalf("wire form %s", b)
	}
	var out model.TimeRange
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if !out.Start.Equal(r.Start) || !out.End.Equal(r.End) {
		t.Fatalf("round trip %+v", out)
	}
}
