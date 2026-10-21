package config_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/config"
	"github.com/zedgama3/bms/internal/lib/errs"
)

func snake(s string) string {
	var b strings.Builder
	rs := []rune(s)
	for i, r := range rs {
		if i > 0 && r >= 'A' && r <= 'Z' && (rs[i-1] >= 'a' && rs[i-1] <= 'z' || rs[i-1] >= '0' && rs[i-1] <= '9') {
			b.WriteByte('_')
		}
		b.WriteString(strings.ToLower(string(r)))
	}
	return b.String()
}

func TestKeys_CoversEveryConfigField(t *testing.T) {
	want := map[string]bool{}
	ct := reflect.TypeOf(config.Config{})
	for i := 0; i < ct.NumField(); i++ {
		sec := ct.Field(i)
		for j := 0; j < sec.Type.NumField(); j++ {
			want[snake(sec.Name)+"."+snake(sec.Type.Field(j).Name)] = true
		}
	}
	keys := config.Keys()
	if len(keys) != len(want) {
		t.Errorf("Keys() has %d rows, Config has %d leaf fields", len(keys), len(want))
	}
	seen := map[string]bool{}
	for _, k := range keys {
		if seen[k.Name] {
			t.Errorf("duplicate key %s", k.Name)
		}
		seen[k.Name] = true
		if !want[k.Name] {
			t.Errorf("key %s matches no Config field", k.Name)
		}
		if k.Doc == "" {
			t.Errorf("key %s has no Doc", k.Name)
		}
	}
	for n := range want {
		if !seen[n] {
			t.Errorf("Config field %s has no key", n)
		}
	}
}

func TestKeys_EnvDerivedFromName(t *testing.T) {
	for _, k := range config.Keys() {
		want := config.Prefix + strings.ToUpper(strings.ReplaceAll(k.Name, ".", "_"))
		if k.Env != want {
			t.Errorf("key %s: Env = %q, want %q", k.Name, k.Env, want)
		}
		if k.Required && k.Default != "" {
			t.Errorf("key %s: required key has a default", k.Name)
		}
		if k.Generated && k.Default != "" {
			t.Errorf("key %s: generated key has a default", k.Name)
		}
	}
}

func TestKeys_ReturnsCopy(t *testing.T) {
	a := config.Keys()
	a[0].Name = "mutated"
	if config.Keys()[0].Name == "mutated" {
		t.Fatal("Keys() shares its backing array")
	}
}

func TestKeys_DefaultsParseAsTheirType(t *testing.T) {
	base := mustLoad(t, env()...)
	for _, k := range config.Keys() {
		if k.Required || k.Generated || strings.Contains(k.Default, "<") {
			continue
		}
		t.Run(k.Name, func(t *testing.T) {
			got := mustLoad(t, env(k.Env+"="+k.Default)...)
			if !reflect.DeepEqual(got, base) {
				t.Errorf("setting %s to its default %q changed the Config", k.Env, k.Default)
			}
		})
	}
}

func TestKeys_RequiredAndSecretFlags(t *testing.T) {
	by := map[string]config.Key{}
	for _, k := range config.Keys() {
		by[k.Name] = k
	}
	if !by["db.url"].Required || !by["db.url"].Secret {
		t.Errorf("db.url must be required and secret: %+v", by["db.url"])
	}
	if !by["session.key"].Secret || !by["session.key"].Generated || by["session.key"].Required {
		t.Errorf("session.key must be secret and generated, not required: %+v", by["session.key"])
	}
	for _, k := range config.Keys() {
		if k.Required && k.Name != "db.url" {
			t.Errorf("unexpected required key %s", k.Name)
		}
	}
}

var specRow = regexp.MustCompile("^\\| `([a-z0-9_.]+)` \\| `(BMS_[A-Z0-9_]+)`")

func TestKeys_DocsInSync(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "plan", "design", "config.md"))
	if err != nil {
		t.Fatalf("spec not readable from the package directory: %v", err)
	}
	spec := map[string]string{}
	var order []string
	inCat := false
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "### The catalogue") {
			inCat = true
			continue
		}
		if inCat && strings.HasPrefix(line, "## ") || inCat && strings.HasPrefix(line, "### ") {
			break
		}
		if m := specRow.FindStringSubmatch(line); inCat && m != nil {
			spec[m[1]] = m[2]
			order = append(order, m[1])
		}
	}
	keys := config.Keys()
	if len(keys) != len(order) {
		t.Fatalf("Keys() has %d rows, spec catalogue %d", len(keys), len(order))
	}
	for i, k := range keys {
		if order[i] != k.Name {
			t.Errorf("row %d: Keys() has %s, spec has %s", i, k.Name, order[i])
		}
		if spec[k.Name] != k.Env {
			t.Errorf("%s: Env %s, spec %s", k.Name, k.Env, spec[k.Name])
		}
	}
}

func TestDefault_WithRequiredSet_Validates(t *testing.T) {
	if err := valid().Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestDefault_Validate_FailsOnlyOnRequired(t *testing.T) {
	err := config.Default().Validate()
	requireInvalidOne(t, err, "BMS_DB_URL")
}

func TestDefault_HTTPAddrIsLoopback(t *testing.T) {
	d := config.Default()
	if d.HTTP.Addr != "127.0.0.1:8080" {
		t.Errorf("Addr = %q", d.HTTP.Addr)
	}
	if len(d.HTTP.TrustedProxies) != 0 || d.HTTP.AllowPlaintext {
		t.Error("default must trust no proxy and not allow plaintext")
	}
}

func TestDefault_AppliesCatalogueDefaults(t *testing.T) {
	d := config.Default()
	if d.Instance.Name != "main" || d.DB.MaxConns != 10 || d.DB.ConnectTimeout != 30*time.Second ||
		d.DB.QueryTimeout != 10*time.Second || d.HTTP.RequestTimeout != 30*time.Second ||
		d.HTTP.ShutdownTimeout != 15*time.Second || d.HTTP.MaxBodyBytes != 1048576 ||
		d.Log.Level != slog.LevelInfo || d.Log.Format != config.LogJSON || d.Data.Dir != "data" ||
		len(d.Modules.Disabled) != 0 || d.DB.URL.IsSet() || d.Session.Key.IsSet() {
		t.Errorf("unexpected defaults: %+v", d)
	}
}

func TestLoad_MinimalEnviron_AppliesDefaults(t *testing.T) {
	cfg := mustLoad(t, env()...)
	if cfg.DB.URL.Value() != dbURL {
		t.Error("DB.URL not read")
	}
	if cfg.Session.Key.IsSet() {
		t.Error("Session.Key must be unset")
	}
	if cfg.Secrets.File != filepath.Join("data", "secrets.json") {
		t.Errorf("Secrets.File = %q", cfg.Secrets.File)
	}
	want := config.Default()
	want.DB.URL = cfg.DB.URL
	want.Secrets.File = cfg.Secrets.File
	// Load makes files.dir absolute (config.md); the default is data/files under the cwd.
	if abs, err := filepath.Abs(want.Files.Dir); err == nil {
		want.Files.Dir = abs
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Errorf("Load minimal differs from Default: %+v", cfg)
	}
}

func TestLoad_EveryKeySet_RoundTrips(t *testing.T) {
	tmp := t.TempDir()
	sf := filepath.Join(tmp, "elsewhere.json")
	cfg := mustLoad(t, env(
		"BMS_INSTANCE_NAME=site-a1",
		"BMS_DB_MAX_CONNS=25",
		"BMS_DB_CONNECT_TIMEOUT=45s",
		"BMS_DB_QUERY_TIMEOUT=2s",
		"BMS_HTTP_ADDR=:9090",
		"BMS_HTTP_REQUEST_TIMEOUT=20s",
		"BMS_HTTP_SHUTDOWN_TIMEOUT=5s",
		"BMS_HTTP_MAX_BODY_BYTES=2048",
		"BMS_HTTP_TRUSTED_PROXIES=10.0.0.0/8",
		"BMS_HTTP_ALLOW_PLAINTEXT=true",
		"BMS_SESSION_KEY="+sessionKey,
		"BMS_SECRETS_FILE="+sf,
		"BMS_LOG_LEVEL=debug",
		"BMS_LOG_FORMAT=text",
		"BMS_DATA_DIR="+tmp,
		"BMS_MODULES_DISABLED=alpha,beta",
	)...)
	checks := []struct {
		name      string
		got, want any
	}{
		{"instance", cfg.Instance.Name, "site-a1"},
		{"maxconns", cfg.DB.MaxConns, 25},
		{"connect", cfg.DB.ConnectTimeout, 45 * time.Second},
		{"query", cfg.DB.QueryTimeout, 2 * time.Second},
		{"addr", cfg.HTTP.Addr, ":9090"},
		{"req", cfg.HTTP.RequestTimeout, 20 * time.Second},
		{"shutdown", cfg.HTTP.ShutdownTimeout, 5 * time.Second},
		{"body", cfg.HTTP.MaxBodyBytes, int64(2048)},
		{"proxies", cfg.HTTP.TrustedProxies, []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}},
		{"plaintext", cfg.HTTP.AllowPlaintext, true},
		{"key", cfg.Session.Key.Value(), sessionKey},
		{"secrets", cfg.Secrets.File, sf},
		{"level", cfg.Log.Level, slog.LevelDebug},
		{"format", cfg.Log.Format, config.LogText},
		{"datadir", cfg.Data.Dir, tmp},
		{"disabled", cfg.Modules.Disabled, []string{"alpha", "beta"}},
	}
	for _, c := range checks {
		if !reflect.DeepEqual(c.got, c.want) {
			t.Errorf("%s: got %#v, want %#v", c.name, c.got, c.want)
		}
	}
}

func TestLoad_EmptyValueMeansUnset(t *testing.T) {
	cfg := mustLoad(t, env(
		"BMS_MODULES_DISABLED=", "BMS_DB_MAX_CONNS=", "BMS_SESSION_KEY=", "BMS_HTTP_ADDR=",
		"BMS_LOG_LEVEL=", "BMS_HTTP_TRUSTED_PROXIES=", "BMS_HTTP_ALLOW_PLAINTEXT=",
	)...)
	d := config.Default()
	if len(cfg.Modules.Disabled) != 0 || cfg.DB.MaxConns != d.DB.MaxConns ||
		cfg.Session.Key.IsSet() || cfg.HTTP.Addr != d.HTTP.Addr || cfg.Log.Level != d.Log.Level {
		t.Errorf("empty values were not treated as unset: %+v", cfg)
	}
}

func TestLoad_EmptyRequiredIsMissing(t *testing.T) {
	_, err := load(t, "BMS_DB_URL=")
	requireInvalidOne(t, err, "BMS_DB_URL")
}

func TestLoad_TrimsNonSecretsOnly(t *testing.T) {
	padded := "  " + sessionKey + "  "
	cfg := mustLoad(t, env("BMS_HTTP_ADDR= 127.0.0.1:8080 ", "BMS_SESSION_KEY="+padded)...)
	if cfg.HTTP.Addr != "127.0.0.1:8080" {
		t.Errorf("Addr = %q", cfg.HTTP.Addr)
	}
	if cfg.Session.Key.Value() != padded {
		t.Errorf("env secret was trimmed: %q", cfg.Session.Key.Value())
	}
}

func TestLoad_SecretFromFile_TrimsTrailingNewlinesOnly(t *testing.T) {
	cases := []struct{ name, content, want string }{
		{"crlf", sessionKey + "\r\n", sessionKey},
		{"double lf", sessionKey + "\n\n", sessionKey},
		{"spaces kept", " " + sessionKey + " \n", " " + sessionKey + " "},
		{"none", sessionKey, sessionKey},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := writeFile(t, t.TempDir(), "k", tc.content, 0o644) // *_FILE has no mode rule
			cfg := mustLoad(t, env("BMS_SESSION_KEY_FILE="+p)...)
			if cfg.Session.Key.Value() != tc.want {
				t.Errorf("got %q, want %q", cfg.Session.Key.Value(), tc.want)
			}
		})
	}
}

func TestLoad_DBURLFromFile(t *testing.T) {
	p := writeFile(t, t.TempDir(), "u", dbURL+"\n", 0o600)
	cfg := mustLoad(t, "BMS_DB_URL_FILE="+p)
	if cfg.DB.URL.Value() != dbURL {
		t.Error("DB.URL not read from file")
	}
}

func TestLoad_LaterDuplicateEntryWins(t *testing.T) {
	cfg := mustLoad(t, env("BMS_DB_MAX_CONNS=5", "BMS_DB_MAX_CONNS=7")...)
	if cfg.DB.MaxConns != 7 {
		t.Errorf("MaxConns = %d, want 7", cfg.DB.MaxConns)
	}
}

func TestLoad_IgnoresTestPrefixAndOtherVariables(t *testing.T) {
	mustLoad(t, env(
		"BMS_TEST_DB_DSN=postgres://x", "OTEL_EXPORTER_OTLP_ENDPOINT=http://collector.example:4318",
		"PATH=/usr/bin", "PGPASSWORD=whatever", "HOME=/home/nobody", "BMSX=1", "bms_test_other=1",
	)...)
}

func TestLoad_TrustedProxies_PrefixesAndBareAddresses(t *testing.T) {
	cfg := mustLoad(t, env("BMS_HTTP_TRUSTED_PROXIES=10.0.0.0/8, 192.0.2.1, 2001:db8::/32")...)
	want := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("192.0.2.1/32"),
		netip.MustParsePrefix("2001:db8::/32"),
	}
	if !reflect.DeepEqual(cfg.HTTP.TrustedProxies, want) {
		t.Errorf("got %v, want %v", cfg.HTTP.TrustedProxies, want)
	}
	cfg = mustLoad(t, env("BMS_HTTP_TRUSTED_PROXIES=2001:db8::1")...)
	if len(cfg.HTTP.TrustedProxies) != 1 || cfg.HTTP.TrustedProxies[0].Bits() != 128 {
		t.Errorf("bare IPv6 should be /128: %v", cfg.HTTP.TrustedProxies)
	}
}

func TestLoad_TrustedProxies_Exactly64Accepted(t *testing.T) {
	cfg := mustLoad(t, env("BMS_HTTP_TRUSTED_PROXIES="+proxyList(64))...)
	if len(cfg.HTTP.TrustedProxies) != 64 {
		t.Errorf("got %d", len(cfg.HTTP.TrustedProxies))
	}
}

func proxyList(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = fmt.Sprintf("10.0.%d.1", i)
	}
	return strings.Join(parts, ",")
}

func itemList(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = fmt.Sprintf("m%d", i)
	}
	return strings.Join(parts, ",")
}

func TestLoad_ModulesDisabled_List(t *testing.T) {
	cfg := mustLoad(t, env("BMS_MODULES_DISABLED=volunteers, incidents")...)
	if !reflect.DeepEqual(cfg.Modules.Disabled, []string{"volunteers", "incidents"}) {
		t.Errorf("got %q", cfg.Modules.Disabled)
	}
	cfg = mustLoad(t, env("BMS_MODULES_DISABLED="+itemList(64))...)
	if len(cfg.Modules.Disabled) != 64 {
		t.Errorf("64 items should be accepted, got %d", len(cfg.Modules.Disabled))
	}
}

func TestLoad_LogLevelAndFormat_Table(t *testing.T) {
	for in, want := range map[string]slog.Level{
		"debug": slog.LevelDebug, "info": slog.LevelInfo, "warn": slog.LevelWarn, "error": slog.LevelError,
	} {
		if got := mustLoad(t, env("BMS_LOG_LEVEL="+in)...).Log.Level; got != want {
			t.Errorf("level %s: got %v", in, got)
		}
	}
	for in, want := range map[string]config.LogFormat{"json": config.LogJSON, "text": config.LogText} {
		if got := mustLoad(t, env("BMS_LOG_FORMAT="+in)...).Log.Format; got != want {
			t.Errorf("format %s: got %v", in, got)
		}
	}
}

func TestLoad_BoolGrammar_Table(t *testing.T) {
	// Non-loopback bind so the value is observable only through AllowPlaintext.
	for in, want := range map[string]bool{"true": true, "false": false} {
		cfg := mustLoad(t, env("BMS_HTTP_ALLOW_PLAINTEXT="+in)...)
		if cfg.HTTP.AllowPlaintext != want {
			t.Errorf("%s: got %v", in, cfg.HTTP.AllowPlaintext)
		}
	}
}

func TestLoad_IntBoundariesAndSigns(t *testing.T) {
	if got := mustLoad(t, env("BMS_DB_MAX_CONNS=+5")...).DB.MaxConns; got != 5 {
		t.Errorf("+5 should parse, got %d", got)
	}
	if got := mustLoad(t, env("BMS_DB_CONNECT_TIMEOUT=1m30s")...).DB.ConnectTimeout; got != 90*time.Second {
		t.Errorf("got %v", got)
	}
}

func TestConfig_ConcurrentReads(t *testing.T) {
	cfg := mustLoad(t, env("BMS_HTTP_TRUSTED_PROXIES=10.0.0.0/8", "BMS_MODULES_DISABLED=a,b",
		"BMS_SESSION_KEY="+sessionKey)...)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = fmt.Sprintf("%v %v %v", cfg.DB.URL.Value(), cfg.Session.Key.Value(), cfg.Instance)
			_ = cfg.HTTP.TrustedProxies[0].Bits()
			_ = cfg.Modules.Disabled[1]
			_ = cfg.Validate() //nolint:errcheck // concurrent read only; the result is checked elsewhere
			_ = cfg.Log.Level
		}()
	}
	wg.Wait()
}

func TestLoad_ConcurrentCalls(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := config.Load(context.Background(), env("BMS_DB_MAX_CONNS=3")); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}

func TestLoad_NilEnvironOnlyRequiredMissing(t *testing.T) {
	_, err := config.Load(context.Background(), nil)
	requireInvalidOne(t, err, "BMS_DB_URL")
}

func TestLoad_ErrorReturnsZeroConfig(t *testing.T) {
	cfg, err := load(t, env("BMS_DB_MAX_CONNS=abc")...)
	if !errs.Is(err, errs.Invalid) {
		t.Fatalf("want Invalid, got %v", err)
	}
	if !reflect.DeepEqual(cfg, config.Config{}) {
		t.Error("Load must return the zero Config on error")
	}
}
