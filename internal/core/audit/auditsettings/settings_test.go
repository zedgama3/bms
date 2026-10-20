package auditsettings_test

import (
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit/auditsettings"
	"github.com/zedgama3/bms/internal/core/settings"
)

func TestSettings_DeclarationsAreValid(t *testing.T) {
	a := settings.Area{Name: auditsettings.Area, Core: true, Decls: auditsettings.Settings}
	if err := a.Validate(); err != nil {
		t.Fatalf("Area.Validate: %v", err)
	}
	if auditsettings.Area != "audit" {
		t.Errorf("Area = %q, want audit", auditsettings.Area)
	}
}

func TestRetention_Check_Table(t *testing.T) {
	var check func(string) error
	for _, d := range auditsettings.Settings {
		if d.Key == "retention" {
			check = d.Check
		}
	}
	if check == nil {
		t.Fatal("no retention setting")
	}
	tests := []struct {
		value string
		ok    bool
	}{
		{"0", true}, // keep forever (D30)
		{"0s", true},
		{"8760h", true}, // 365 days
		{"876000h", true},
		{(auditsettings.MinRetention - time.Hour).String(), false},
		{(auditsettings.MaxRetention + time.Hour).String(), false},
		{"720h", false},
	}
	for _, tc := range tests {
		if err := check(tc.value); (err == nil) != tc.ok {
			t.Errorf("Check(%q) = %v, want ok=%v", tc.value, err, tc.ok)
		}
	}
}
