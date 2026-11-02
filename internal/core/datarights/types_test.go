package datarights_test

import (
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/datarights"
)

func TestConstants_MatchTheSpec(t *testing.T) {
	ints := []struct {
		name      string
		got, want int
	}{
		{"MaxNoteBytes", datarights.MaxNoteBytes, 500},
		{"MaxTrackedExports", datarights.MaxTrackedExports, 10000},
		{"ExportFormat", datarights.ExportFormat, 1},
	}
	for _, tc := range ints {
		if tc.got != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, tc.got, tc.want)
		}
	}
	if datarights.ExportCooldown != 10*time.Minute {
		t.Errorf("ExportCooldown = %v, want 10m", datarights.ExportCooldown)
	}
}

func TestRequestStatus_ValuesAreDistinctAndNonZero(t *testing.T) {
	seen := map[datarights.RequestStatus]bool{}
	for _, s := range []datarights.RequestStatus{datarights.RequestOpen, datarights.RequestWithdrawn, datarights.RequestDeclined} {
		if s == 0 {
			t.Error("a status is the zero value")
		}
		if seen[s] {
			t.Errorf("status %d repeated", s)
		}
		seen[s] = true
	}
}
