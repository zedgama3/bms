package config_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/config"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

func TestLoad_RequiredMissing_ReturnsInvalidNamingVariable(t *testing.T) {
	_, err := load(t)
	f := wantInvalidOne(t, err, "BMS_DB_URL")
	if f.Code != validate.CodeRequired {
		t.Errorf("Code = %q, want %q", f.Code, validate.CodeRequired)
	}
}

func TestLoad_BadInt_ReturnsInvalid(t *testing.T) {
	for _, v := range []string{"10.0", "ten", "1_000", "0x10", "1e3", "5 5", marker} {
		t.Run(v, func(t *testing.T) {
			_, err := load(t, env("BMS_DB_MAX_CONNS="+v)...)
			requireInvalidOne(t, err, "BMS_DB_MAX_CONNS")
			noLeak(t, err, marker)
		})
	}
	_, err := load(t, env("BMS_HTTP_MAX_BODY_BYTES=1MiB")...)
	requireInvalidOne(t, err, "BMS_HTTP_MAX_BODY_BYTES")
	_, err = load(t, env("BMS_DB_MAX_CONNS=-1")...) // parses, then range
	requireInvalidOne(t, err, "BMS_DB_MAX_CONNS")
}

func TestLoad_BadDuration_ReturnsInvalid(t *testing.T) {
	for _, v := range []string{"30", "abc", "1.5x", "s", "30 s"} {
		t.Run(v, func(t *testing.T) {
			_, err := load(t, env("BMS_HTTP_REQUEST_TIMEOUT="+v)...)
			requireInvalidOne(t, err, "BMS_HTTP_REQUEST_TIMEOUT")
		})
	}
}

func TestLoad_BadEnum_ReturnsInvalidListingOptions(t *testing.T) {
	cases := []struct {
		name, variable string
		options        []string
	}{
		{"level", "BMS_LOG_LEVEL", []string{"debug", "info", "warn", "error"}},
		{"format", "BMS_LOG_FORMAT", []string{"json", "text"}},
	}
	for _, c := range cases {
		for _, v := range []string{"Info", "INFO", "JSON", "verbose", "yaml", marker} {
			if c.name == "level" && (v == "yaml" || v == "JSON") || c.name == "format" && (v == "Info" || v == "INFO" || v == "verbose") {
				continue
			}
			t.Run(c.name+"/"+v, func(t *testing.T) {
				_, err := load(t, env(c.variable+"="+v)...)
				f := wantInvalidOne(t, err, c.variable)
				for _, o := range c.options {
					if !strings.Contains(f.Reason, o) {
						t.Errorf("reason %q does not list option %q", f.Reason, o)
					}
				}
				noLeak(t, err, v)
			})
		}
	}
}

func TestLoad_BadList_ReturnsInvalid(t *testing.T) {
	for name, v := range map[string]string{
		"empty middle": "a,,b", "trailing": "a,", "leading": ",a", "duplicate": "a,b,a",
		"blank item": "a, ,b", "65 items": itemList(65),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := load(t, env("BMS_MODULES_DISABLED="+v)...)
			requireInvalidOne(t, err, "BMS_MODULES_DISABLED")
		})
	}
}

func TestLoad_BadCIDR_ReturnsInvalid(t *testing.T) {
	for _, v := range []string{"10.0.0.0/33", "notanip", "10.0.0.0/8/9", "10.0.0.0/8,,10.1.0.0/16", "10.0.0.0/8,", "::1/129", marker} {
		t.Run(v, func(t *testing.T) {
			_, err := load(t, env("BMS_HTTP_TRUSTED_PROXIES="+v)...)
			requireInvalidOne(t, err, "BMS_HTTP_TRUSTED_PROXIES")
			noLeak(t, err, marker)
		})
	}
}

func TestLoad_ZeroBitTrustedProxy_ReturnsInvalid(t *testing.T) {
	for _, v := range []string{"0.0.0.0/0", "::/0", "10.0.0.0/8,0.0.0.0/0"} {
		_, err := load(t, env("BMS_HTTP_TRUSTED_PROXIES="+v)...)
		f := wantInvalidOne(t, err, "BMS_HTTP_TRUSTED_PROXIES")
		if !strings.Contains(f.Reason, "trusts every peer") {
			t.Errorf("%s: reason %q", v, f.Reason)
		}
	}
}

func TestLoad_TooManyTrustedProxies_ReturnsInvalid(t *testing.T) {
	_, err := load(t, env("BMS_HTTP_TRUSTED_PROXIES="+proxyList(65))...)
	requireInvalidOne(t, err, "BMS_HTTP_TRUSTED_PROXIES")
}

func TestLoad_BadBool_ReturnsInvalid(t *testing.T) {
	for _, v := range []string{"1", "0", "yes", "no", "True", "TRUE", "False", "t", marker} {
		t.Run(v, func(t *testing.T) {
			_, err := load(t, env("BMS_HTTP_ALLOW_PLAINTEXT="+v)...)
			requireInvalidOne(t, err, "BMS_HTTP_ALLOW_PLAINTEXT")
			noLeak(t, err, marker)
		})
	}
}

func TestLoad_SecretAndFileBothSet_ReturnsInvalid(t *testing.T) {
	p := writeFile(t, t.TempDir(), "k", sessionKey, 0o600)
	_, err := load(t, env("BMS_SESSION_KEY="+sessionKey, "BMS_SESSION_KEY_FILE="+p)...)
	f := wantInvalidOne(t, err, "BMS_SESSION_KEY_FILE")
	if !strings.Contains(f.Reason, "BMS_SESSION_KEY") {
		t.Errorf("reason should name both variables: %q", f.Reason)
	}
	noLeak(t, err, sessionKey)

	up := writeFile(t, t.TempDir(), "u", dbURL, 0o600)
	_, err = load(t, "BMS_DB_URL="+dbURL, "BMS_DB_URL_FILE="+up)
	requireInvalidOne(t, err, "BMS_DB_URL_FILE")
	noLeak(t, err, marker)
}

func TestLoad_SecretFileUnreadable_ReturnsInvalid(t *testing.T) {
	dir := t.TempDir()
	for name, p := range map[string]string{
		"missing":   filepath.Join(dir, "nope"),
		"directory": dir,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := load(t, env("BMS_SESSION_KEY_FILE="+p)...)
			if !errs.Is(err, errs.Invalid) {
				t.Fatalf("want Invalid, got %v", err)
			}
			if n := fieldNames(err); len(n) != 1 || n[0] != "BMS_SESSION_KEY_FILE" && n[0] != "BMS_SESSION_KEY" {
				t.Errorf("want one problem under the key's variable, got %v", n)
			}
		})
	}
	// A missing required _FILE must not also report "required".
	_, err := load(t, "BMS_DB_URL_FILE="+filepath.Join(dir, "nope"))
	if n := fieldNames(err); len(n) != 1 {
		t.Errorf("want one problem for the key, got %v", n)
	}
}

func TestLoad_SecretFileTooLarge_ReturnsInvalid(t *testing.T) {
	p := writeFile(t, t.TempDir(), "big", strings.Repeat("a", config.MaxSecretFileBytes+1), 0o600)
	_, err := load(t, env("BMS_SESSION_KEY_FILE="+p)...)
	if !errs.Is(err, errs.Invalid) {
		t.Fatalf("want Invalid, got %v", err)
	}
	if n := fieldNames(err); len(n) != 1 {
		t.Errorf("want one problem, got %v", n)
	}
}

func TestLoad_SecretFileEmpty_IsUnset(t *testing.T) {
	p := writeFile(t, t.TempDir(), "e", "\r\n\n", 0o600)
	cfg := mustLoad(t, env("BMS_SESSION_KEY_FILE="+p)...)
	if cfg.Session.Key.IsSet() {
		t.Error("empty file should leave the key unset")
	}
	_, err := load(t, "BMS_DB_URL_FILE="+p)
	f := wantInvalidOne(t, err, "BMS_DB_URL")
	if f.Code != validate.CodeRequired {
		t.Errorf("Code = %q", f.Code)
	}
}

func TestLoad_UnknownVariable_ReturnsInvalid(t *testing.T) {
	for _, name := range []string{"BMS_DB_MAXCONNS", "BMS_TYPO", "BMS_DB_MAX_CONNS_FILE", "BMS_LOG_LEVEL_FILE", "BMS_", "BMS_TESTING"} {
		t.Run(name, func(t *testing.T) {
			_, err := load(t, env(name+"="+marker)...)
			f := wantInvalidOne(t, err, name)
			if !strings.Contains(f.Reason, "unknown configuration variable") {
				t.Errorf("reason %q", f.Reason)
			}
			noLeak(t, err, marker)
		})
	}
	t.Run("lowercase prefix", func(t *testing.T) {
		_, err := load(t, env("bms_db_maxconns=5")...)
		if !errs.Is(err, errs.Invalid) || len(fieldNames(err)) != 1 {
			t.Errorf("want one Invalid problem, got %v", err)
		}
	})
	t.Run("lowercase known name", func(t *testing.T) {
		_, err := load(t, "bms_db_url="+dbURL)
		if !errs.Is(err, errs.Invalid) {
			t.Errorf("a case-variant of a known name must be refused, got %v", err)
		}
		noLeak(t, err, marker)
	})
}

func TestLoad_MalformedEnviron_ReturnsInternal(t *testing.T) {
	_, err := load(t, env("NOEQUALSSIGN")...)
	if !errs.Is(err, errs.Internal) {
		t.Fatalf("want Internal, got kind %v: %v", errs.KindOf(err), err)
	}
	if errs.Is(err, errs.Invalid) {
		t.Error("must not be Invalid")
	}
}

func TestLoad_ContextCanceled_ReturnsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cfg, err := config.Load(ctx, env())
	if !errs.Is(err, errs.Canceled) {
		t.Fatalf("want Canceled, got kind %v: %v", errs.KindOf(err), err)
	}
	if !reflect.DeepEqual(cfg, config.Config{}) {
		t.Error("want zero Config")
	}
}

func TestLoad_DeadlinePassed_ReturnsTimeout(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	_, err := config.Load(ctx, env())
	if !errs.Is(err, errs.Timeout) {
		t.Fatalf("want Timeout, got kind %v: %v", errs.KindOf(err), err)
	}
}

func TestLoad_ManyProblems_ReportsEachOnce(t *testing.T) {
	skipWindows(t)
	dir := t.TempDir()
	open := writeFile(t, dir, "secrets.json", `{}`, 0o644)
	kf := writeFile(t, dir, "k", sessionKey, 0o600)
	_, err := load(t,
		"BMS_BOGUS=1",
		"BMS_LOG_LEVEL=Info",
		"BMS_SECRETS_FILE="+open,
		"BMS_SESSION_KEY="+sessionKey,
		"BMS_SESSION_KEY_FILE="+kf,
		"BMS_HTTP_ADDR=:8080",
		"BMS_DB_MAX_CONNS=abc",
	)
	if !errs.Is(err, errs.Invalid) {
		t.Fatalf("want Invalid, got %v", err)
	}
	want := []string{"BMS_DB_URL", "BMS_DB_MAX_CONNS", "BMS_HTTP_ADDR", "BMS_SESSION_KEY_FILE", "BMS_SECRETS_FILE", "BMS_LOG_LEVEL", "BMS_BOGUS"}
	if got := fieldNames(err); !reflect.DeepEqual(got, want) {
		t.Errorf("fields = %v, want %v", got, want)
	}
	noLeak(t, err, sessionKey)
}

func TestLoad_ErrorsNeverEchoSecrets(t *testing.T) {
	cases := map[string][]string{
		"bad url":      {"BMS_DB_URL=mysql://u:" + marker + "@h/x"},
		"url no host":  {"BMS_DB_URL=postgres://:" + marker + "@/x"},
		"short key":    env("BMS_SESSION_KEY=" + marker),
		"long key":     env("BMS_SESSION_KEY=" + strings.Repeat(marker, 120)),
		"nul in key":   env("BMS_SESSION_KEY=" + sessionKey + "\x00"),
		"url too long": {"BMS_DB_URL=postgres://u:" + marker + "@h/" + strings.Repeat("a", 4100)},
		"nul in url":   {"BMS_DB_URL=postgres://u:" + marker + "@h/x\x00"},
	}
	for name, e := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := load(t, e...)
			if !errs.Is(err, errs.Invalid) {
				t.Fatalf("want Invalid, got %v", err)
			}
			noLeak(t, err, marker)
		})
	}
}

func TestLoad_CallerEnvironNotModified(t *testing.T) {
	e := env("BMS_DB_MAX_CONNS=5")
	cp := append([]string(nil), e...)
	mustLoad(t, e...)
	if !reflect.DeepEqual(e, cp) {
		t.Error("Load modified environ")
	}
	_ = os.Getenv // keep os imported for platform-specific helpers
}

func TestLoad_SecretFileReadError_ReasonNeverContainsPath(t *testing.T) {
	// The "path" is a pasted secret: neither it nor any directory part may be echoed.
	pasted := "postgres://bms:" + marker + "@db.example/bms"
	base := t.TempDir()

	mkdirs := func(rel string) string {
		p := filepath.Join(base, rel)
		if err := os.MkdirAll(p, 0o700); err != nil {
			t.Fatal(err)
		}
		return p
	}
	denied := mkdirs("locked")
	deniedFile := writeFile(t, denied, marker, dbURL, 0o600)
	if err := os.Chmod(denied, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(denied, 0o700); err != nil { //nolint:gosec // a directory needs the execute bit to be removed
			t.Logf("chmod: %v", err)
		}
	})

	cases := map[string]string{
		"not found (relative pasted value)": pasted,
		"not found (absolute)":              filepath.Join(base, marker, "nope"),
		"directory":                         mkdirs(marker + "dir"),
	}
	if os.Geteuid() != 0 {
		cases["permission denied"] = deniedFile
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := load(t, "BMS_DB_URL_FILE="+p)
			if !errs.Is(err, errs.Invalid) {
				t.Fatalf("want Invalid, got %v", err)
			}
			noLeak(t, err, marker)
			noLeak(t, err, base)
			noLeak(t, err, "db.example")
			if f := findField(err, "BMS_DB_URL_FILE"); f != nil && strings.Contains(f.Reason, "open: open") {
				t.Errorf("doubled text in reason: %q", f.Reason)
			}
			if strings.Contains(err.Error(), "open: open") {
				t.Errorf("doubled text in error: %q", err.Error())
			}
		})
	}
}
