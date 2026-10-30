package volunteers

import (
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/volunteers/vol"
)

// pageRig builds n fetched rows one hour apart, and summaries for the ones in keep (an
// eligible summary each), in order.
func pageRig(n int, keep map[int]bool) ([]vol.ShiftSummary, []vol.ShiftRow) {
	base := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	fetched := make([]vol.ShiftRow, n)
	var out []vol.ShiftSummary
	for i := range fetched {
		fetched[i].ID = model.NewID[vol.ShiftID]()
		fetched[i].Starts = base.Add(time.Duration(i) * time.Hour)
		if keep[i] {
			out = append(out, vol.ShiftSummary{Shift: fetched[i].Shift, Eligibility: vol.Eligibility{Eligible: true}})
		}
	}
	return out, fetched
}

// TestPageEligible_NextIsLastConsumedRow walks the three cases of the paging rule (V4.3.4).
func TestPageEligible_NextIsLastConsumedRow(t *testing.T) {
	all := func(n int) map[int]bool {
		m := make(map[int]bool, n)
		for i := range n {
			m[i] = true
		}
		return m
	}
	tests := []struct {
		name     string
		fetched  int
		keep     map[int]bool
		size     int
		wantLen  int
		wantNext int // index of the row whose cursor is Next; -1 for none
	}{
		{"limit reached with rows beyond", 10, all(10), 3, 3, 2},
		{"limit reached at the very last fetched row", 3, all(3), 3, 3, -1},
		{"limit reached, only dropped rows beyond", 5, map[int]bool{0: true, 1: true}, 2, 2, 1},
		{"short page, nothing beyond", 5, map[int]bool{1: true}, 3, 1, -1},
		{"nothing passes, nothing beyond", 4, nil, 3, 0, -1},
		{"short page, scan bound hit", vol.MaxList + 1, map[int]bool{4: true}, 3, 1, vol.MaxList - 1},
		{"nothing passes, scan bound hit", vol.MaxList + 1, nil, 3, 0, vol.MaxList - 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, fetched := pageRig(tc.fetched, tc.keep)
			got, next := pageEligible(out, fetched, tc.size)
			if len(got) != tc.wantLen {
				t.Fatalf("returned %d rows, want %d", len(got), tc.wantLen)
			}
			want := vol.Cursor("")
			if tc.wantNext >= 0 {
				want = vol.NewCursor(fetched[tc.wantNext].Starts, fetched[tc.wantNext].ID)
			}
			if next != want {
				t.Errorf("Next = %q, want the cursor of fetched[%d]", next, tc.wantNext)
			}
		})
	}
}

// TestPageEligible_KeepsMineWhenNotEligible: a booking the actor holds stays, whatever its
// eligibility now says.
func TestPageEligible_KeepsMineWhenNotEligible(t *testing.T) {
	out, fetched := pageRig(3, nil)
	out = append(out,
		vol.ShiftSummary{Shift: fetched[1].Shift, Mine: model.NewID[vol.SignupID](), Eligibility: vol.Eligibility{Reason: vol.ReasonFull}},
		vol.ShiftSummary{Shift: fetched[2].Shift, Eligibility: vol.Eligibility{Reason: vol.ReasonFull}},
	)
	got, next := pageEligible(out, fetched, 10)
	if len(got) != 1 || got[0].ID != fetched[1].ID {
		t.Fatalf("returned %v, want only the booked shift", got)
	}
	if next != "" {
		t.Errorf("Next = %q, want none", next)
	}
}
