package config

import (
	"reflect"
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
