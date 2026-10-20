package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/config"
	"github.com/zedgama3/bms/internal/lib/errs"
)

func skeyJSON(v string) string {
	b, err := json.Marshal(map[string]string{config.SecretsFieldSessionKey: v})
	if err != nil {
		panic(err) // a string map always marshals
	}
	return string(b)
}

func TestLoad_SecretsFileAbsent_LeavesSessionKeyUnset(t *testing.T) {
	dir := t.TempDir() // no secrets.json inside
	cfg := mustLoad(t, env("BMS_DATA_DIR="+dir)...)
	if cfg.Session.Key.IsSet() {
		t.Error("key must be unset")
	}
	if cfg.Secrets.File != filepath.Join(dir, config.SecretsFileName) {
		t.Errorf("Secrets.File = %q", cfg.Secrets.File)
	}
	// An explicit path that does not exist is also not an error.
	mustLoad(t, env("BMS_SECRETS_FILE="+filepath.Join(dir, "none.json"))...)
}

func TestLoad_SecretsFile_ProvidesSessionKey(t *testing.T) {
	dir, path := secretsFile(t, skeyJSON(sessionKey))
	cfg := mustLoad(t, env("BMS_DATA_DIR="+dir)...)
	if cfg.Session.Key.Value() != sessionKey {
		t.Error("key not byte-for-byte")
	}
	if cfg.Secrets.File != path {
		t.Errorf("Secrets.File = %q, want %q", cfg.Secrets.File, path)
	}
}

func TestLoad_SecretsFile_ExplicitPathWins(t *testing.T) {
	_, path := secretsFile(t, skeyJSON(sessionKey))
	cfg := mustLoad(t, env("BMS_DATA_DIR="+t.TempDir(), "BMS_SECRETS_FILE="+path)...)
	if cfg.Session.Key.Value() != sessionKey || cfg.Secrets.File != path {
		t.Error("explicit BMS_SECRETS_FILE not used")
	}
}

func TestLoad_SecretsFile_UnknownFieldsIgnored(t *testing.T) {
	dir, _ := secretsFile(t, `{"session_key":"`+sessionKey+`","future_key":"abc","n":5,"o":{"x":1}}`)
	cfg := mustLoad(t, env("BMS_DATA_DIR="+dir)...)
	if cfg.Session.Key.Value() != sessionKey {
		t.Error("key not read")
	}
}

func TestLoad_SecretsFile_EmptySessionKeyIsUnset(t *testing.T) {
	for _, js := range []string{`{}`, `{"session_key":""}`, `{"other":"x"}`} {
		dir, _ := secretsFile(t, js)
		cfg := mustLoad(t, env("BMS_DATA_DIR="+dir)...)
		if cfg.Session.Key.IsSet() {
			t.Errorf("%s: key should be unset", js)
		}
	}
}

func TestLoad_SessionKeyFromEnvWithoutSecretsFile(t *testing.T) {
	cfg := mustLoad(t, env("BMS_DATA_DIR="+t.TempDir(), "BMS_SESSION_KEY="+sessionKey)...)
	if cfg.Session.Key.Value() != sessionKey {
		t.Error("key not read from env")
	}
}

func TestLoad_SecretsFileUnreadable_ReturnsInvalid(t *testing.T) {
	skipWindows(t)
	dir := t.TempDir()
	t.Run("directory", func(t *testing.T) {
		_, err := load(t, env("BMS_SECRETS_FILE="+dir)...)
		requireInvalidOne(t, err, "BMS_SECRETS_FILE")
	})
	t.Run("dangling symlink target is a directory", func(t *testing.T) {
		sub := filepath.Join(dir, "sub")
		if err := os.Mkdir(sub, 0o700); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(dir, "link.json")
		if err := os.Symlink(sub, link); err != nil {
			t.Skip("symlinks unavailable")
		}
		_, err := load(t, env("BMS_SECRETS_FILE="+link)...)
		requireInvalidOne(t, err, "BMS_SECRETS_FILE")
	})
	t.Run("no read permission", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root reads anything")
		}
		p := writeFile(t, t.TempDir(), "s.json", skeyJSON(sessionKey), 0o000)
		_, err := load(t, env("BMS_SECRETS_FILE="+p)...)
		requireInvalidOne(t, err, "BMS_SECRETS_FILE")
		noLeak(t, err, sessionKey)
	})
}

func TestLoad_SecretsFilePermissionsTooOpen_ReturnsInvalid(t *testing.T) {
	skipWindows(t)
	for _, mode := range []os.FileMode{0o644, 0o660, 0o604, 0o640, 0o666, 0o601} {
		t.Run(mode.String(), func(t *testing.T) {
			p := writeFile(t, t.TempDir(), "s.json", skeyJSON(sessionKey), mode)
			_, err := load(t, env("BMS_SECRETS_FILE="+p)...)
			f := wantInvalidOne(t, err, "BMS_SECRETS_FILE")
			if !strings.Contains(f.Reason, "0600") {
				t.Errorf("reason %q should tell the operator to chmod 0600", f.Reason)
			}
			noLeak(t, err, sessionKey)
		})
	}
	for _, mode := range []os.FileMode{0o600, 0o400} {
		p := writeFile(t, t.TempDir(), "s.json", skeyJSON(sessionKey), mode)
		mustLoad(t, env("BMS_SECRETS_FILE="+p)...)
	}
}

func TestLoad_SecretsFileMalformed_ReturnsInvalidWithoutEcho(t *testing.T) {
	cases := map[string]string{
		"not json":         "this is not json " + marker,
		"array":            `["` + marker + `"]`,
		"string":           `"` + marker + `"`,
		"number key":       `{"session_key": 12345, "x": "` + marker + `"}`,
		"object key":       `{"session_key": {"a":"` + marker + `"}}`,
		"truncated":        `{"session_key": "` + marker + marker,
		"empty file":       ``,
		"trailing garbage": `{"session_key":"` + sessionKey + `"} ` + marker,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			_, path := secretsFile(t, content)
			_, err := load(t, env("BMS_SECRETS_FILE="+path)...)
			if !errs.Is(err, errs.Invalid) {
				t.Fatalf("want Invalid, got %v", err)
			}
			if n := fieldNames(err); len(n) != 1 || n[0] != "BMS_SECRETS_FILE" {
				t.Errorf("fields = %v", n)
			}
			noLeak(t, err, marker)
			noLeak(t, err, sessionKey)
		})
	}
}

func TestLoad_SecretsFileTooLarge_ReturnsInvalid(t *testing.T) {
	pad := strings.Repeat("a", config.MaxSecretFileBytes)
	_, path := secretsFile(t, `{"pad":"`+pad+`"}`)
	_, err := load(t, env("BMS_SECRETS_FILE="+path)...)
	requireInvalidOne(t, err, "BMS_SECRETS_FILE")
}

func TestLoad_SecretsFileShortKey_ReturnsInvalid(t *testing.T) {
	cases := map[string]string{
		"short":    marker,
		"31 bytes": sessionKey[:31],
		"1025":     strings.Repeat("k", 1025),
		"NUL":      sessionKey + "\x00",
	}
	for name, key := range cases {
		t.Run(name, func(t *testing.T) {
			_, path := secretsFile(t, skeyJSON(key))
			_, err := load(t, env("BMS_SECRETS_FILE="+path)...)
			requireInvalidOne(t, err, "BMS_SESSION_KEY")
			noLeak(t, err, marker)
		})
	}
	for _, n := range []int{32, 1024} {
		_, path := secretsFile(t, skeyJSON(strings.Repeat("k", n)))
		cfg := mustLoad(t, env("BMS_SECRETS_FILE="+path)...)
		if len(cfg.Session.Key.Value()) != n {
			t.Errorf("%d-byte key should be accepted", n)
		}
	}
}

func TestLoad_SessionKeyInEnvAndSecretsFile_ReturnsInvalid(t *testing.T) {
	_, path := secretsFile(t, skeyJSON(sessionKey))
	kf := writeFile(t, t.TempDir(), "k", sessionKey, 0o600)
	for name, e := range map[string][]string{
		"env":  {"BMS_SESSION_KEY=" + sessionKey},
		"file": {"BMS_SESSION_KEY_FILE=" + kf},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := load(t, env(append(e, "BMS_SECRETS_FILE="+path)...)...)
			requireInvalidOne(t, err, "BMS_SESSION_KEY")
			noLeak(t, err, sessionKey)
		})
	}
}

func TestLoad_SecretsFileEmptyKeyAndEnvKey_NoConflict(t *testing.T) {
	_, path := secretsFile(t, skeyJSON(""))
	cfg := mustLoad(t, env("BMS_SESSION_KEY="+sessionKey, "BMS_SECRETS_FILE="+path)...)
	if cfg.Session.Key.Value() != sessionKey {
		t.Error("env key should be used")
	}
}

func TestLoad_NeverWritesSecretsFile(t *testing.T) {
	dir := t.TempDir()
	mustLoad(t, env("BMS_DATA_DIR="+dir)...)
	ents, err := os.ReadDir(dir)
	if err != nil || len(ents) != 0 {
		t.Errorf("Load created files in the data dir: %v %v", ents, err)
	}
}

const sealingKey = "settings-sealing-key-0123456789abcdef-test"

func TestLoad_SecretsFile_ProvidesBothKeys(t *testing.T) {
	dir, _ := secretsFile(t, `{"session_key":"`+sessionKey+`","settings_sealing_key":"`+sealingKey+`"}`)
	cfg := mustLoad(t, env("BMS_DATA_DIR="+dir)...)
	if cfg.Session.Key.Value() != sessionKey || cfg.Settings.SealingKey.Value() != sealingKey {
		t.Error("a key was not read byte for byte")
	}
}

func TestLoad_SealingKeyFromEnvOrFile(t *testing.T) {
	cfg := mustLoad(t, env("BMS_DATA_DIR="+t.TempDir(), "BMS_SETTINGS_SEALING_KEY="+sealingKey)...)
	if cfg.Settings.SealingKey.Value() != sealingKey {
		t.Error("key not read from the environment")
	}
	keyFile := writeFile(t, t.TempDir(), "sealing", sealingKey+"\n", 0o600)
	cfg = mustLoad(t, env("BMS_DATA_DIR="+t.TempDir(), "BMS_SETTINGS_SEALING_KEY_FILE="+keyFile)...)
	if cfg.Settings.SealingKey.Value() != sealingKey {
		t.Error("key not read from the _FILE variant")
	}
}

func TestLoad_SealingKeyUnset_IsNotAnError(t *testing.T) {
	dir, _ := secretsFile(t, skeyJSON(sessionKey))
	cfg := mustLoad(t, env("BMS_DATA_DIR="+dir)...)
	if cfg.Settings.SealingKey.IsSet() {
		t.Error("the sealing key must be unset when no source has it")
	}
}

func TestLoad_SealingKeyInEnvAndSecretsFile_ReturnsInvalid(t *testing.T) {
	dir, _ := secretsFile(t, `{"settings_sealing_key":"`+sealingKey+`"}`)
	_, err := load(t, env("BMS_DATA_DIR="+dir, "BMS_SETTINGS_SEALING_KEY="+sealingKey)...)
	if !errs.Is(err, errs.Invalid) || findField(err, "BMS_SETTINGS_SEALING_KEY") == nil {
		t.Fatalf("err = %v, want Invalid under BMS_SETTINGS_SEALING_KEY", err)
	}
	noLeak(t, err, sealingKey)
}

func TestLoad_ShortSealingKey_ReturnsInvalidWithoutEcho(t *testing.T) {
	_, err := load(t, env("BMS_DATA_DIR="+t.TempDir(), "BMS_SETTINGS_SEALING_KEY="+strings.Repeat("z", 8))...)
	if fe := wantInvalidOne(t, err, "BMS_SETTINGS_SEALING_KEY"); fe.Code != "length" {
		t.Errorf("code = %q, want length", fe.Code)
	}
	noLeak(t, err, strings.Repeat("z", 8))
}

// F33
func TestLoad_SecretsFileBadSealingKey_ReturnsInvalid(t *testing.T) {
	cases := map[string]string{
		"short":    marker,
		"31 bytes": sealingKey[:31],
		"1025":     strings.Repeat("s", 1025),
		"NUL":      sealingKey + "\x00",
	}
	for name, key := range cases {
		t.Run(name, func(t *testing.T) {
			b, err := json.Marshal(map[string]string{config.SecretsFieldSettingsSealingKey: key})
			if err != nil {
				t.Fatal(err)
			}
			_, path := secretsFile(t, string(b))
			_, err = load(t, env("BMS_SECRETS_FILE="+path)...)
			requireInvalidOne(t, err, "BMS_SETTINGS_SEALING_KEY")
			noLeak(t, err, marker)
		})
	}
	// Not a string: the file is malformed, reported under the file's variable.
	for name, js := range map[string]string{
		"number": `{"settings_sealing_key": 12345, "x": "` + marker + `"}`,
		"object": `{"settings_sealing_key": {"k": "` + marker + `"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, path := secretsFile(t, js)
			_, err := load(t, env("BMS_SECRETS_FILE="+path)...)
			requireInvalidOne(t, err, "BMS_SECRETS_FILE")
			noLeak(t, err, marker)
		})
	}
}

// F32, both environment forms set together.
func TestLoad_SealingKeyEnvAndFileVariant_ReturnsInvalid(t *testing.T) {
	kf := writeFile(t, t.TempDir(), "k", sealingKey, 0o600)
	_, err := load(t, env("BMS_DATA_DIR="+t.TempDir(), "BMS_SETTINGS_SEALING_KEY="+sealingKey, "BMS_SETTINGS_SEALING_KEY_FILE="+kf)...)
	if !errs.Is(err, errs.Invalid) || findField(err, "BMS_SETTINGS_SEALING_KEY_FILE") == nil {
		t.Fatalf("err = %v, want Invalid under BMS_SETTINGS_SEALING_KEY_FILE", err)
	}
	noLeak(t, err, sealingKey)
}
