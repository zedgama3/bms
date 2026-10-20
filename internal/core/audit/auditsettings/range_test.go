package auditsettings_test

import (
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/audit/auditsettings"
	"github.com/zedgama3/bms/internal/lib/validate"
)

func TestRetentionCheck_OutOfRange_ReadsInPlainUnits(t *testing.T) {
	for _, d := range auditsettings.Settings {
		if d.Key != "retention" {
			continue
		}
		fes := validate.Fields(d.Check("1h"))
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
		return
	}
	t.Fatal("no retention setting")
}
