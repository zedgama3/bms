package app_test

// D56 review follow-ups of plan/design/demo.md: F42.

import (
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/app"
)

// F42: with demo mode off every `bms demo <x>` is refused right after the config loads, exit
// 2, naming BMS_DEMO_ENABLED. The database URL points at a closed port, so touching the
// database would give a different status.
func TestDemoCommand_Disabled_Exit2_NamesKey(t *testing.T) {
	tests := []struct {
		name string
		args []string
		env  []string
	}{
		{"seed, key unset", []string{"demo", "seed"}, nil},
		{"reset, key unset", []string{"demo", "reset"}, nil},
		{"seed, key false", []string{"demo", "seed"}, []string{"BMS_DEMO_ENABLED=false"}},
		{"reset, key false", []string{"demo", "reset"}, []string{"BMS_DEMO_ENABLED=false"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := baseEnv(t, closedDBURL(t), tc.env...)
			r := run(t.Context(), t, env, tc.args...)
			wantCode(t, r, app.ExitUsage)
			if !strings.Contains(r.errOut+r.out, "BMS_DEMO_ENABLED") {
				t.Errorf("output does not name BMS_DEMO_ENABLED:\nstdout:\n%s\nstderr:\n%s", r.out, r.errOut)
			}
			noMarker(t, r)
		})
	}
}
