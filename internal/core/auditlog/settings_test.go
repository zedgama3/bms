package auditlog_test

import (
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/auditlog"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// These are auditsettings' tests, moved with the declarations. The package's Area and
// MinRetention/MaxRetention constants are not in the boundaries Interface, so the area name
// and the bounds are written out here.

func TestSettings_DeclarationsAreValid(t *testing.T) {
	a := settings.Area{Name: "audit", Core: true, Decls: auditlog.Settings}
	if err := a.Validate(); err != nil {
		t.Fatalf("Area.Validate: %v", err)
	}
	keys := map[string]bool{}
	for _, d := range auditlog.Settings {
		keys[d.Key] = true
	}
	if len(keys) != 2 || !keys["retention"] || !keys["self_view"] {
		t.Errorf("keys = %v, want retention and self_view", keys)
	}
}

func retentionCheck(t *testing.T) func(string) error {
	t.Helper()
	for _, d := range auditlog.Settings {
		if d.Key == "retention" {
			return d.Check
		}
	}
	t.Fatal("no retention setting")
	return nil
}

func TestRetention_Check_Table(t *testing.T) {
	check := retentionCheck(t)
	const minRetention, maxRetention = 8760 * time.Hour, 876000 * time.Hour
	tests := []struct {
		value string
		ok    bool
	}{
		{"0", true}, // keep forever (D30)
		{"0s", true},
		{"8760h", true}, // 365 days
		{"876000h", true},
		{(minRetention - time.Hour).String(), false},
		{(maxRetention + time.Hour).String(), false},
		{"720h", false},
	}
	for _, tc := range tests {
		if err := check(tc.value); (err == nil) != tc.ok {
			t.Errorf("Check(%q) = %v, want ok=%v", tc.value, err, tc.ok)
		}
	}
}

func TestRetentionCheck_OutOfRange_ReadsInPlainUnits(t *testing.T) {
	fes := validate.Fields(retentionCheck(t)("1h"))
	if len(fes) != 1 || fes[0].Code != validate.CodeRange {
		t.Fatalf("fields = %+v", fes)
	}
	fe := fes[0]
	if fe.Params["min"] != "8760h" || fe.Params["max"] != "876000h" {
		t.Errorf("params = %v, want 8760h and 876000h", fe.Params)
	}
	if strings.Contains(fe.Reason, "0m0s") || strings.Contains(fe.Params["min"]+fe.Params["max"], "m0s") {
		t.Errorf("a Go duration string reached the message: %q %v", fe.Reason, fe.Params)
	}
}

func TestSettings_Defaults(t *testing.T) {
	for _, d := range auditlog.Settings {
		switch d.Key {
		case "retention":
			if d.Default != "0" {
				t.Errorf("retention default = %q, want 0 (keep forever)", d.Default)
			}
		case "self_view":
			if d.Default != "off" {
				t.Errorf("self_view default = %q, want off", d.Default)
			}
		}
	}
}
