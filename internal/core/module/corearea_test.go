package module_test

import (
	"testing"

	"github.com/zedgama3/bms/internal/core/module"
)

func TestIsCoreArea_Table(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"core", true}, {"people", true}, {"accounts", true}, {"access", true}, {"identity", true},
		{"authz", true}, {"departments", true}, {"events", true}, {"signals", true},
		{"notify", true}, {"audit", true}, {"files", true}, {"jobs", true}, {"settings", true},
		{"telemetry", true}, {"db", true}, {"module", true}, {"modules", true},
		{"hello", false}, {"", false}, {"People", false}, {"peoples", false}, {" people", false},
		{"api", false}, {"app", false}, {"pgcore", false}, {"volunteers", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := module.IsCoreArea(tc.name); got != tc.want {
				t.Fatalf("IsCoreArea(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}
