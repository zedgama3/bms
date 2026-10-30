package config

import (
	"reflect"
	"strings"
	"testing"
)

// TestCatalogue_DefaultMatchesKeyDefaults pins that Default() agrees with the text defaults
// in the catalogue: parsing every non-empty default over a zero Config yields Default().
func TestCatalogue_DefaultMatchesKeyDefaults(t *testing.T) {
	var got Config
	for _, e := range catalogue {
		if e.Default == "" || e.Name == "secrets.file" || e.Name == "files.dir" {
			continue
		}
		if code, reason := e.parse(&got, e.Default); code != "" {
			t.Fatalf("%s: default does not parse: %s", e.Name, reason)
		}
	}
	got.Secrets.File = Default().Secrets.File
	got.Files.Dir = Default().Files.Dir
	if !reflect.DeepEqual(got, Default()) {
		t.Fatalf("catalogue defaults differ from Default():\n got %+v\nwant %+v", got, Default())
	}
}

// TestCatalogue_EnvOfKnowsEveryUsedName pins that validate.go looks up names that exist.
func TestCatalogue_EnvOfKnowsEveryUsedName(t *testing.T) {
	for _, e := range catalogue {
		if envOf(e.Name) != e.Env {
			t.Fatalf("%s: envOf = %q, want %q", e.Name, envOf(e.Name), e.Env)
		}
	}
}

func TestIsLoopbackBind_Table(t *testing.T) {
	for _, tc := range []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:80", true}, {"[::1]:80", true}, {"localhost:80", true}, {"LOCALHOST:80", true},
		{"127.9.9.9:80", true}, {":80", false}, {"0.0.0.0:80", false}, {"10.0.0.1:80", false},
		{"[::]:80", false}, {"garbage", false},
	} {
		t.Run(tc.addr, func(t *testing.T) {
			if got := isLoopbackBind(tc.addr); got != tc.want {
				t.Fatalf("isLoopbackBind(%q) = %v, want %v", tc.addr, got, tc.want)
			}
		})
	}
}

func TestDemoURLProblem_Table(t *testing.T) {
	for _, tc := range []struct {
		url string
		ok  bool
	}{
		{"https://demo.example.org", true},
		{"http://demo.example.org:8080", true},
		{"https://example.org/bms", true},
		{"https://demo.example.org/", false},
		{"HTTPS://demo.example.org", false},
		{"https://Demo.example.org", false},
		{"https://demo.example.org:0", false},
		{"https://demo.example.org:99999", false},
		{"https://u@demo.example.org", false},
		{"https://demo.example.org?a=b", false},
		{"https://demo.example.org#f", false},
		{"ftp://demo.example.org", false},
		{"demo.example.org", false},
		{" https://demo.example.org", false},
		{"https://demo example.org", false},
	} {
		t.Run(tc.url, func(t *testing.T) {
			if got := demoURLProblem(tc.url) == ""; got != tc.ok {
				t.Fatalf("demoURLProblem(%q) ok = %v, want %v", tc.url, got, tc.ok)
			}
		})
	}
}

func TestTelemetryUsage_StrictBool(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
		ok   bool
	}{{"true", true, true}, {"false", false, true}, {"TRUE", false, false}, {"1", false, false}} {
		var c Config
		var e entry
		for _, x := range catalogue {
			if x.Name == "telemetry.usage" {
				e = x
			}
		}
		code, _ := e.parse(&c, tc.in)
		if (code == "") != tc.ok || (tc.ok && c.Telemetry.Usage != tc.want) {
			t.Errorf("%q: code %q usage %v", tc.in, code, c.Telemetry.Usage)
		}
	}
	if !e2(t).strict || !Default().Telemetry.Usage {
		t.Error("telemetry.usage must default to true and keep untrimmed text")
	}
}

func e2(t *testing.T) entry {
	t.Helper()
	for _, x := range catalogue {
		if x.Name == "telemetry.usage" {
			return x
		}
	}
	t.Fatal("telemetry.usage missing")
	return entry{}
}

func TestCanonicalDemoURL_Table(t *testing.T) {
	for _, tc := range []struct {
		in, want string
	}{
		{"https://X.Example/", "https://x.example"},
		{"HTTP://Demo.Example.ORG", "http://demo.example.org"},
		{"https://x.example///", "https://x.example"},
		{"https://x.example:443", "https://x.example"},
		{"http://x.example:80/", "http://x.example"},
		{"https://x.example:8443/", "https://x.example:8443"},
		{"http://x.example:443", "http://x.example:443"},
		{"https://x.example/Bms/", "https://x.example/Bms"},
		{"http://[::1]:8080/", "http://[::1]:8080"},
		{"", ""},
	} {
		t.Run(tc.in, func(t *testing.T) {
			if tc.in == "" {
				return
			}
			got, reason := canonicalDemoURL(tc.in)
			if reason != "" || got != tc.want {
				t.Fatalf("canonicalDemoURL(%q) = %q, %q; want %q", tc.in, got, reason, tc.want)
			}
		})
	}
	for _, bad := range []string{
		"ftp://x.example", "x.example", "https://", "https://u:p@x.example", "https://x.example?a=1",
		"https://x.example#f", "https://x.example:0", "https://x.example:70000", " https://x.example",
		"https://x.example/" + strings.Repeat("a", 260),
	} {
		if got, reason := canonicalDemoURL(bad); reason == "" {
			t.Errorf("canonicalDemoURL(%q) accepted as %q", bad, got)
		}
	}
}

func TestLoad_DemoPublicURL_IsStoredCanonical(t *testing.T) {
	cfg, err := Load(t.Context(), []string{
		"BMS_DB_URL=postgres://u:p@localhost/db", "BMS_DEMO_ENABLED=true", "BMS_DEMO_PUBLIC_URL=https://X.Example/",
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Demo.PublicURL != "https://x.example" {
		t.Fatalf("PublicURL = %q", cfg.Demo.PublicURL)
	}
}
