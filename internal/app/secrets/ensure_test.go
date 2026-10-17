package secrets_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/app/secrets"
	"github.com/zedgama3/bms/internal/config"
	"github.com/zedgama3/bms/internal/lib/errs"
)

const marker = "ZQ9MARKER7"

var sessionField = []string{config.SecretsFieldSessionKey}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // test reads its own temp file
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("%s is not a JSON object: %v", path, err)
	}
	return m
}

func write(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod: %v", err)
	}
}

func TestEnsure_CreatesFile0600WithKey(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "a", "b")
	path := filepath.Join(dir, "secrets.json")
	created, err := secrets.Ensure(t.Context(), path, sessionField)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if !slices.Equal(created, sessionField) {
		t.Errorf("created = %v, want %v", created, sessionField)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %v, want 0600", fi.Mode().Perm())
	}
	for _, d := range []string{dir, filepath.Join(root, "a")} {
		di, err := os.Stat(d)
		if err != nil {
			t.Fatal(err)
		}
		if di.Mode().Perm() != 0o700 {
			t.Errorf("dir %s mode = %v, want 0700", d, di.Mode().Perm())
		}
	}
	v := str(readJSON(t, path)[config.SecretsFieldSessionKey])
	raw, err := base64.StdEncoding.DecodeString(v)
	if err != nil {
		t.Fatalf("the key is not standard base64: %v", err)
	}
	if len(raw) != secrets.FieldBytes || secrets.FieldBytes != 64 {
		t.Errorf("key is %d bytes (FieldBytes %d), want 64", len(raw), secrets.FieldBytes)
	}
}

func TestEnsure_TwoFields_DifferentValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	created, err := secrets.Ensure(t.Context(), path, []string{"alpha", "beta"})
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if !slices.Equal(created, []string{"alpha", "beta"}) {
		t.Errorf("created = %v", created)
	}
	m := readJSON(t, path)
	if m["alpha"] == "" || m["alpha"] == m["beta"] {
		t.Errorf("values must be non-empty and independent: %v", m)
	}
}

func TestEnsure_ExistingValueNeverOverwritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.json")
	content := "{\n  \"session_key\": \"" + marker + "keep\"\n}\n"
	write(t, path, content, 0o600)
	created, err := secrets.Ensure(t.Context(), path, sessionField)
	if err != nil || created != nil {
		t.Fatalf("Ensure = (%v, %v), want (nil, nil)", created, err)
	}
	got, err := os.ReadFile(path) //nolint:gosec // test reads its own temp file
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != content {
		t.Errorf("file changed: %q", got)
	}
}

func TestEnsure_AddsMissingFieldPreservingOthers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.json")
	write(t, path, `{"future_field":"keep-me","count":7,"nested":{"a":[1,2]},"alpha":"have"}`, 0o600)
	created, err := secrets.Ensure(t.Context(), path, []string{"alpha", "beta"})
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if !slices.Equal(created, []string{"beta"}) {
		t.Errorf("created = %v, want [beta]", created)
	}
	m := readJSON(t, path)
	if m["future_field"] != "keep-me" || m["count"] != float64(7) || m["alpha"] != "have" {
		t.Errorf("existing fields not preserved: %v", m)
	}
	if n, ok := m["nested"].(map[string]any); !ok || len(asSlice(n["a"])) != 2 {
		t.Errorf("nested field not preserved: %v", m["nested"])
	}
	if str(m["beta"]) == "" {
		t.Errorf("beta not generated: %v", m)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("mode after replace = %v (%v), want 0600", fi.Mode().Perm(), err)
	}
}

func TestEnsure_EmptyValueIsGenerated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.json")
	write(t, path, `{"session_key":""}`, 0o600)
	created, err := secrets.Ensure(t.Context(), path, sessionField)
	if err != nil || !slices.Equal(created, sessionField) {
		t.Fatalf("Ensure = (%v, %v)", created, err)
	}
	if str(readJSON(t, path)["session_key"]) == "" {
		t.Error("empty value was not replaced")
	}
}

func TestEnsure_RepeatedOrEmptyField_ReturnsInvalid(t *testing.T) {
	good := filepath.Join(t.TempDir(), "x.json")
	for name, tc := range map[string]struct {
		path   string
		fields []string
	}{
		"repeated":    {good, []string{"a", "a"}},
		"empty name":  {good, []string{"a", ""}},
		"no fields":   {good, nil},
		"empty slice": {good, []string{}},
		"empty path":  {"", sessionField},
	} {
		t.Run(name, func(t *testing.T) {
			created, err := secrets.Ensure(t.Context(), tc.path, tc.fields)
			if !errs.Is(err, errs.Invalid) {
				t.Fatalf("err = %v, want Invalid", err)
			}
			if created != nil {
				t.Errorf("created = %v, want nil", created)
			}
			if _, statErr := os.Stat(good); statErr == nil {
				t.Error("a file was created despite invalid arguments")
			}
		})
	}
}

// F22
func TestEnsure_DirectoryUnwritable_ReturnsInvalid(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	write(t, blocker, "x", 0o600)
	_, err := secrets.Ensure(t.Context(), filepath.Join(blocker, "sub", "secrets.json"), sessionField)
	if !errs.Is(err, errs.Invalid) {
		t.Fatalf("err = %v, want Invalid", err)
	}
}

// F22: the content is never echoed.
func TestEnsure_MalformedExisting_ReturnsInvalidWithoutEcho(t *testing.T) {
	for name, content := range map[string]string{
		"not json":         "this is " + marker + " not json",
		"array":            `["` + marker + `"]`,
		"known not string": `{"session_key": 12345, "x":"` + marker + `"}`,
		"known null":       `{"session_key": null}`,
		"trailing junk":    `{"session_key":"` + marker + `"} garbage`,
		"truncated":        `{"session_key":"` + marker,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "secrets.json")
			write(t, path, content, 0o600)
			_, err := secrets.Ensure(t.Context(), path, sessionField)
			if !errs.Is(err, errs.Invalid) {
				t.Fatalf("err = %v, want Invalid", err)
			}
			if strings.Contains(err.Error(), marker) {
				t.Errorf("the error echoes the file's content: %v", err)
			}
			got, rerr := os.ReadFile(path) //nolint:gosec // test reads its own temp file
			if rerr != nil || string(got) != content {
				t.Errorf("a malformed file must be left untouched (err %v)", rerr)
			}
		})
	}
}

func TestEnsure_NotRegularFile_ReturnsInvalid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.json")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := secrets.Ensure(t.Context(), path, sessionField); !errs.Is(err, errs.Invalid) {
		t.Fatalf("err = %v, want Invalid", err)
	}
}

func TestEnsure_FileOverSizeLimit_ReturnsInvalid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.json")
	big := `{"pad":"` + strings.Repeat("a", config.MaxSecretFileBytes) + `"}`
	write(t, path, big, 0o600)
	if _, err := secrets.Ensure(t.Context(), path, sessionField); !errs.Is(err, errs.Invalid) {
		t.Fatalf("err = %v, want Invalid", err)
	}
}

func TestEnsure_AtomicReplace_NoTempLeft(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.json")
	write(t, path, `{"other":"v"}`, 0o600)
	if _, err := secrets.Ensure(t.Context(), path, sessionField); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	assertOnly(t, dir, "secrets.json")

	// A failure leaves no temporary file either.
	bad := filepath.Join(dir, "bad.json")
	write(t, bad, `{"session_key": 5}`, 0o600)
	if _, err := secrets.Ensure(t.Context(), bad, sessionField); err == nil {
		t.Fatal("want an error")
	}
	assertOnly(t, dir, "bad.json", "secrets.json")
}

func assertOnly(t *testing.T, dir string, names ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	slices.Sort(got)
	if !slices.Equal(got, names) {
		t.Errorf("directory holds %v, want %v", got, names)
	}
}

func TestEnsure_ContextCanceled_ReturnsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "secrets.json")
	if _, err := secrets.Ensure(ctx, path, sessionField); !errs.Is(err, errs.Canceled) {
		t.Fatalf("err = %v, want Canceled", err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("the file was created under a canceled context")
	}
}

func TestEnsure_DeadlinePassed_ReturnsTimeout(t *testing.T) {
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	path := filepath.Join(t.TempDir(), "secrets.json")
	if _, err := secrets.Ensure(ctx, path, sessionField); !errs.Is(err, errs.Timeout) {
		t.Fatalf("err = %v, want Timeout", err)
	}
}

// Config reads what Ensure wrote (config.md decision 7).
func TestEnsure_ThenConfigLoadReadsKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.json")
	if _, err := secrets.Ensure(t.Context(), path, sessionField); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(t.Context(), []string{"BMS_DB_URL=postgres://u@127.0.0.1:1/d", "BMS_DATA_DIR=" + dir})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Session.Key.IsSet() {
		t.Error("config did not pick up the generated key")
	}
}

func TestEnsure_SecondCallLeavesBytesIdentical(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.json")
	if _, err := secrets.Ensure(t.Context(), path, sessionField); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path) //nolint:gosec // test reads its own temp file
	if err != nil {
		t.Fatal(err)
	}
	created, err := secrets.Ensure(t.Context(), path, sessionField)
	if err != nil || created != nil {
		t.Fatalf("second Ensure = (%v, %v)", created, err)
	}
	second, err := os.ReadFile(path) //nolint:gosec // test reads its own temp file
	if err != nil || !bytes.Equal(first, second) {
		t.Errorf("file changed on the second call (%v)", err)
	}
}

// str returns v when it is a string, else "".
func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func asSlice(v any) []any {
	if s, ok := v.([]any); ok {
		return s
	}
	return nil
}
