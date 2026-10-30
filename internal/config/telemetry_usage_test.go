package config_test

import (
	"strings"
	"testing"
)

// Amendment T1 (D54), config.md F39.

func TestLoad_TelemetryUsage_Malformed_ReturnsInvalid(t *testing.T) {
	for _, v := range []string{"yes", "1", "0", "TRUE", "False", "on", " true", "true ", "ZQ9MARKER7"} {
		t.Run("value="+v, func(t *testing.T) {
			_, err := load(t, env("BMS_TELEMETRY_USAGE="+v)...)
			f := wantInvalidOne(t, err, "BMS_TELEMETRY_USAGE")
			if f != nil && strings.Contains(err.Error(), "BMS_TELEMETRY_USAGE="+v) {
				t.Fatalf("error echoes the value")
			}
		})
	}
}

func TestLoad_TelemetryUsage_StrictBool_Accepted(t *testing.T) {
	if got := mustLoad(t, env("BMS_TELEMETRY_USAGE=true")...).Telemetry.Usage; !got {
		t.Fatal("Telemetry.Usage = false, want true")
	}
	if got := mustLoad(t, env("BMS_TELEMETRY_USAGE=false")...).Telemetry.Usage; got {
		t.Fatal("Telemetry.Usage = true, want false")
	}
}

func TestLoad_TelemetryUsage_DefaultOn(t *testing.T) {
	if got := mustLoad(t, env()...).Telemetry.Usage; !got {
		t.Fatal("Telemetry.Usage default = false, want true (D54)")
	}
}
