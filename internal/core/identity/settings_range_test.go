package identity_test

import (
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/lib/validate"
)

func TestDurationRange_OutOfRange_ReadsInPlainUnits(t *testing.T) {
	tests := []struct {
		key, value, min, max string
	}{
		{"session_lifetime", "30m", "1h", "8760h"},
		{"superuser_session_lifetime", "1m", "10m", "24h"},
		{"login_failure_window", "30s", "1m", "24h"},
	}
	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			for _, d := range identity.Settings {
				if d.Key != tc.key {
					continue
				}
				fes := validate.Fields(d.Check(tc.value))
				if len(fes) != 1 || fes[0].Code != validate.CodeRange {
					t.Fatalf("fields = %+v", fes)
				}
				fe := fes[0]
				if fe.Params["min"] != tc.min || fe.Params["max"] != tc.max {
					t.Errorf("params = %v, want %s and %s", fe.Params, tc.min, tc.max)
				}
				if strings.Contains(fe.Reason, "0s") && strings.Contains(fe.Reason, "m0s") {
					t.Errorf("a Go duration string reached the reason: %q", fe.Reason)
				}
				return
			}
			t.Fatalf("no %s setting", tc.key)
		})
	}
}
