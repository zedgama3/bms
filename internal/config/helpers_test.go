package config_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/config"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// Invented credential markers: they cannot occur by accident.
const (
	marker = "ZQ9MARKER7"
	dbURL  = "postgres://bms:" + marker + "@db.example:5432/bms"
)

// sessionKey is 48 bytes of repeated marker text.
var sessionKey = strings.Repeat(marker, 5)[:48]

// env returns an environment holding a valid BMS_DB_URL plus extra entries.
func env(extra ...string) []string {
	return append([]string{"BMS_DB_URL=" + dbURL}, extra...)
}

// envNoDB returns extra entries only.
func load(t *testing.T, environ ...string) (config.Config, error) {
	t.Helper()
	return config.Load(context.Background(), environ) //nolint:wrapcheck // test helper passes the error through for assertions
}

func mustLoad(t *testing.T, environ ...string) config.Config {
	t.Helper()
	cfg, err := load(t, environ...)
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}
	return cfg
}

// fieldNames lists the Field of every FieldError in err, in order.
func fieldNames(err error) []string {
	out := []string{}
	for _, f := range validate.Fields(err) {
		out = append(out, f.Field)
	}
	return out
}

func findField(err error, name string) *validate.FieldError {
	for _, f := range validate.Fields(err) {
		if f.Field == name {
			return f
		}
	}
	return nil
}

// wantInvalid asserts err is Invalid and names exactly the one variable.
func wantInvalidOne(t *testing.T, err error, name string) *validate.FieldError {
	t.Helper()
	if err == nil {
		t.Fatalf("want Invalid under %s, got nil", name)
	}
	if !errs.Is(err, errs.Invalid) {
		t.Fatalf("want errs.Invalid, got kind %v: %v", errs.KindOf(err), err)
	}
	got := fieldNames(err)
	if len(got) != 1 || got[0] != name {
		t.Fatalf("want exactly one problem under %s, got %v", name, got)
	}
	return findField(err, name)
}

// noLeak asserts neither the error text nor any Reason/Param contains s.
func noLeak(t *testing.T, err error, s string) {
	t.Helper()
	if err == nil {
		return
	}
	if strings.Contains(err.Error(), s) {
		t.Errorf("error text leaks %q", s)
	}
	for _, f := range validate.Fields(err) {
		if strings.Contains(f.Reason, s) || strings.Contains(f.Field, s) {
			t.Errorf("FieldError for %s leaks %q", f.Field, s)
		}
		for _, v := range f.Params {
			if strings.Contains(v, s) {
				t.Errorf("FieldError params for %s leak %q", f.Field, s)
			}
		}
	}
}

func writeFile(t *testing.T, dir, name, content string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), mode); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	if err := os.Chmod(p, mode); err != nil { // umask may have narrowed it
		t.Fatalf("chmod %s: %v", p, err)
	}
	return p
}

// secretsFile writes a 0600 secrets.json holding the given JSON text into a new temp dir.
func secretsFile(t *testing.T, json string) (dir, path string) {
	t.Helper()
	dir = t.TempDir()
	path = writeFile(t, dir, config.SecretsFileName, json, 0o600)
	return dir, path
}

func skipWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are the operator's concern on Windows")
	}
}

// valid returns a Default() with the required value set.
func valid() config.Config {
	c := config.Default()
	c.DB.URL = secret.New(dbURL)
	return c
}

// requireInvalidOne is wantInvalidOne for callers that don't need the FieldError.
func requireInvalidOne(t *testing.T, err error, name string) {
	t.Helper()
	if fe := wantInvalidOne(t, err, name); fe == nil {
		t.Fatalf("want a FieldError under %s", name)
	}
}
