package regional

import (
	"testing"

	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// Moved verbatim from settings' internal tests: the colour rules are regional's now (the
// stylesheet and public URL tests are theme_internal_test.go and url_internal_test.go).

func TestInternalFormatRatio_RoundsDown(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{4.4966, "4.49"},
		{4.4999, "4.49"},
		{4.5, "4.50"},
		{4.56, "4.56"},
		{3.0, "3.00"},
		{1.999, "1.99"},
	}
	for _, tc := range tests {
		if got := formatRatio(tc.in); got != tc.want {
			t.Errorf("formatRatio(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestInternalCheckColor_FailingContrastNeverShowsTheMinimum(t *testing.T) {
	// Every grey from #000000 up is checked: a ratio under 4.5 must never print as 4.50.
	check := checkColor(lightBackground)
	for g := 0; g < 256; g++ {
		c := rgb{uint8(g), uint8(g), uint8(g)}
		err := check(c.hex())
		for _, fe := range validate.Fields(err) {
			if fe.Params["ratio"] == "4.50" {
				t.Fatalf("%s: ratio shown as 4.50 although it fails", c.hex())
			}
		}
	}
}

func TestInternalRegionalArea_Validates(t *testing.T) {
	a := settings.Area{Name: Area, Core: true, Decls: Settings}
	if err := a.Validate(); err != nil {
		t.Fatalf("regional area: %v", err)
	}
}
