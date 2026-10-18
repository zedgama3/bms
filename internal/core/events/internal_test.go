package events

import (
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/lib/errs"
)

func TestKindRef_IsStablePerKey(t *testing.T) {
	a, b, c := kindRef("burn"), kindRef("burn"), kindRef("other")
	if a != b {
		t.Errorf("kindRef(burn) differs between calls: %v vs %v", a, b)
	}
	if a == c {
		t.Errorf("kindRef(burn) == kindRef(other)")
	}
	if err := a.Validate(); err != nil {
		t.Errorf("kindRef(burn) is not a valid audit reference: %v", err)
	}
}

func TestInstant_TruncatesToMicrosecondsInUTC(t *testing.T) {
	zone := time.FixedZone("x", 3600)
	in := time.Date(2026, 8, 1, 12, 0, 0, 1999, zone)
	got := instant(in)
	if got.Location() != time.UTC || got.Nanosecond() != 1000 {
		t.Errorf("instant = %v (%d ns), want UTC at microsecond precision", got, got.Nanosecond())
	}
	if !instant(time.Time{}).IsZero() {
		t.Error("instant(zero) is not zero")
	}
}

func TestParseStatus_AcceptsOnlyTheThreeWords(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Status
		bad  bool
	}{
		{"any", StatusAny, false}, {"open", StatusOpen, false}, {"closed", StatusClosed, false},
		{"", 0, true}, {"Open", 0, true}, {"all", 0, true},
	} {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseStatus(tc.in)
			if tc.bad {
				if !errs.Is(err, errs.Invalid) {
					t.Fatalf("err = %v, want invalid", err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Errorf("ParseStatus(%q) = %v, %v; want %v", tc.in, got, err, tc.want)
			}
		})
	}
}
