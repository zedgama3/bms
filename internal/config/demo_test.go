package config_test

import (
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/config"
)

// userinfoMarker is an invented value that must never be echoed in an error.
var userinfoMarker = strings.Repeat("ZQ9", 2) + "NOECHO"

// F36 (D53).
func TestLoad_DemoEnabled_Malformed_ReturnsInvalid(t *testing.T) {
	for _, v := range []string{"yes", "1", "TRUE", "on", " true"} {
		t.Run("value="+v, func(t *testing.T) {
			_, err := load(t, env("BMS_DEMO_ENABLED="+v)...)
			f := wantInvalidOne(t, err, "BMS_DEMO_ENABLED")
			if f != nil && strings.Contains(err.Error(), "BMS_DEMO_ENABLED="+v) {
				t.Fatalf("error echoes the value")
			}
		})
	}
}

func TestLoad_DemoEnabled_StrictBool_Accepted(t *testing.T) {
	if got := mustLoad(t, env("BMS_DEMO_ENABLED=true")...).Demo.Enabled; !got {
		t.Fatal("Demo.Enabled = false, want true")
	}
	if got := mustLoad(t, env("BMS_DEMO_ENABLED=false")...).Demo.Enabled; got {
		t.Fatal("Demo.Enabled = true, want false")
	}
}

func TestLoad_DemoDefault_Off(t *testing.T) {
	cfg := mustLoad(t, env()...)
	if cfg.Demo.Enabled || cfg.Demo.PublicURL != "" {
		t.Fatalf("default demo = %+v, want off and empty", cfg.Demo)
	}
}

// F37 (D53).
func TestLoad_DemoPublicURL_WithoutDemo_ReturnsInvalid(t *testing.T) {
	for _, extra := range [][]string{
		{"BMS_DEMO_PUBLIC_URL=https://demo.example.org"},
		{"BMS_DEMO_ENABLED=false", "BMS_DEMO_PUBLIC_URL=https://demo.example.org"},
	} {
		_, err := load(t, env(extra...)...)
		if wantInvalidOne(t, err, "BMS_DEMO_PUBLIC_URL") == nil {
			t.Fatal("no field error")
		}
	}
}

// F38 (D53).
func TestLoad_DemoPublicURL_Malformed_ReturnsInvalid(t *testing.T) {
	cases := map[string]string{
		"no scheme": "demo.example.org",
		"ftp":       "ftp://demo.example.org",
		"no host":   "https://",
		"query":     "https://demo.example.org/?a=b",
		"fragment":  "https://demo.example.org/#x",
		"userinfo":  "https://user:" + userinfoMarker + "@demo.example.org",
		"too long":  "https://demo.example.org/" + strings.Repeat("a", 260),
		"not a url": "http://[::1",
	}
	for name, v := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := load(t, env("BMS_DEMO_ENABLED=true", "BMS_DEMO_PUBLIC_URL="+v)...)
			if wantInvalidOne(t, err, "BMS_DEMO_PUBLIC_URL") == nil {
				t.Fatal("no field error")
			}
			if strings.Contains(err.Error(), userinfoMarker) {
				t.Fatal("error echoes the userinfo")
			}
		})
	}
}

func TestLoad_DemoPublicURL_Valid_Accepted(t *testing.T) {
	cfg := mustLoad(t, env("BMS_DEMO_ENABLED=true", "BMS_DEMO_PUBLIC_URL=https://demo.example.org")...)
	if !cfg.Demo.Enabled || cfg.Demo.PublicURL != "https://demo.example.org" {
		t.Fatalf("demo = %+v", cfg.Demo)
	}
	// Empty allowed with demo on.
	mustLoad(t, env("BMS_DEMO_ENABLED=true")...)
}

// F37/F38 through Validate on a hand-built Config.
func TestValidate_DemoPublicURL_HandBuilt(t *testing.T) {
	cfg := mustLoad(t, env()...)
	cfg.Demo = config.Demo{Enabled: false, PublicURL: "https://demo.example.org"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate accepted a public URL with demo off")
	}
	cfg.Demo = config.Demo{Enabled: true, PublicURL: "https://demo.example.org?x=1"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate accepted a URL with a query")
	}
}

func TestKeys_ListDemoKeys(t *testing.T) {
	seen := map[string]bool{}
	for _, k := range config.Keys() {
		seen[k.Env] = true
	}
	for _, want := range []string{"BMS_DEMO_ENABLED", "BMS_DEMO_PUBLIC_URL"} {
		if !seen[want] {
			t.Errorf("Keys() lacks %s", want)
		}
	}
}

// Load canonicalises a valid URL (lower-case host, no trailing slash) rather than rejecting it.
func TestLoad_DemoPublicURL_Canonicalised(t *testing.T) {
	cfg := mustLoad(t, env("BMS_DEMO_ENABLED=true", "BMS_DEMO_PUBLIC_URL=https://X.Example/")...)
	if cfg.Demo.PublicURL != "https://x.example" {
		t.Fatalf("PublicURL = %q, want https://x.example", cfg.Demo.PublicURL)
	}
}
