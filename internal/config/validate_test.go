package config_test

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/config"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/lib/validate"
)

func TestValidate_RangesTable(t *testing.T) {
	type mut func(*config.Config, int64)
	s := func(d time.Duration) int64 { return int64(d) }
	cases := []struct {
		variable string
		set      mut
		lo, hi   int64
	}{
		{"BMS_DB_MAX_CONNS", func(c *config.Config, v int64) { c.DB.MaxConns = int(v) }, 1, 1000},
		{"BMS_DB_CONNECT_TIMEOUT", func(c *config.Config, v int64) { c.DB.ConnectTimeout = time.Duration(v) }, s(time.Second), s(10 * time.Minute)},
		{"BMS_DB_QUERY_TIMEOUT", func(c *config.Config, v int64) { c.DB.QueryTimeout = time.Duration(v) }, s(100 * time.Millisecond), s(10 * time.Minute)},
		{"BMS_HTTP_REQUEST_TIMEOUT", func(c *config.Config, v int64) { c.HTTP.RequestTimeout = time.Duration(v) }, s(time.Second), s(10 * time.Minute)},
		{"BMS_HTTP_SHUTDOWN_TIMEOUT", func(c *config.Config, v int64) { c.HTTP.ShutdownTimeout = time.Duration(v) }, s(time.Second), s(5 * time.Minute)},
		{"BMS_HTTP_MAX_BODY_BYTES", func(c *config.Config, v int64) { c.HTTP.MaxBodyBytes = v }, 1024, 1073741824},
	}
	for _, tc := range cases {
		t.Run(tc.variable, func(t *testing.T) {
			for _, v := range []int64{tc.lo, tc.hi} {
				c := valid()
				tc.set(&c, v)
				if err := c.Validate(); err != nil {
					t.Errorf("boundary %d should pass: %v", v, err)
				}
			}
			for _, v := range []int64{tc.lo - 1, tc.hi + 1, 0, -1} {
				c := valid()
				tc.set(&c, v)
				err := c.Validate()
				f := wantInvalidOne(t, err, tc.variable)
				if f.Code != validate.CodeRange {
					t.Errorf("value %d: Code = %q", v, f.Code)
				}
				if f.Params["min"] == "" || f.Params["max"] == "" {
					t.Errorf("Params = %v", f.Params)
				}
			}
		})
	}
}

func TestValidate_RangesViaLoad(t *testing.T) {
	for _, kv := range []string{
		"BMS_DB_MAX_CONNS=0", "BMS_DB_MAX_CONNS=1001", "BMS_DB_CONNECT_TIMEOUT=999ms",
		"BMS_DB_QUERY_TIMEOUT=99ms", "BMS_HTTP_SHUTDOWN_TIMEOUT=6m", "BMS_HTTP_MAX_BODY_BYTES=1023",
		"BMS_HTTP_MAX_BODY_BYTES=1073741825", "BMS_HTTP_REQUEST_TIMEOUT=11m",
	} {
		_, err := load(t, env(kv)...)
		requireInvalidOne(t, err, strings.SplitN(kv, "=", 2)[0])
	}
}

func TestValidate_DBURL_ReturnsInvalidWithoutEcho(t *testing.T) {
	long := "postgres://u:" + marker + "@h/" + strings.Repeat("a", 4096)
	cases := map[string]string{
		"wrong scheme":   "mysql://u:" + marker + "@h/x",
		"no host":        "postgres://u:" + marker + "@/x",
		"relative":       "u:" + marker + "/x",
		"not a url":      marker,
		"too long":       long,
		"nul":            "postgres://u:" + marker + "@h/x\x00",
		"bad percent":    "postgres://u:%zz" + marker + "@h/x",
		"scheme only":    "postgres://",
		"file scheme":    "file:///" + marker,
		"http scheme":    "http://u:" + marker + "@h/x",
		"whitespace-led": " " + marker,
	}
	for name, u := range cases {
		t.Run(name, func(t *testing.T) {
			c := valid()
			c.DB.URL = secret.New(u)
			err := c.Validate()
			requireInvalidOne(t, err, "BMS_DB_URL")
			noLeak(t, err, marker)
		})
	}
	for _, ok := range []string{"postgres://h/db", "postgresql://u:p@h:5432/db?sslmode=disable", "postgres://[::1]:5432/db"} {
		c := valid()
		c.DB.URL = secret.New(ok)
		if err := c.Validate(); err != nil {
			t.Errorf("%s should pass: %v", ok, err)
		}
	}
	c := valid()
	c.DB.URL = secret.New("")
	f := wantInvalidOne(t, c.Validate(), "BMS_DB_URL")
	if f.Code != validate.CodeRequired {
		t.Errorf("Code = %q", f.Code)
	}
}

func TestValidate_SessionKey_ReturnsInvalidWithoutEcho(t *testing.T) {
	for name, k := range map[string]string{
		"short": marker, "31": sessionKey[:31], "1025": strings.Repeat(marker, 103)[:1025], "nul": sessionKey + "\x00",
	} {
		t.Run(name, func(t *testing.T) {
			c := valid()
			c.Session.Key = secret.New(k)
			err := c.Validate()
			requireInvalidOne(t, err, "BMS_SESSION_KEY")
			noLeak(t, err, marker)
		})
	}
	for _, n := range []int{32, 1024} {
		c := valid()
		c.Session.Key = secret.New(strings.Repeat("k", n))
		if err := c.Validate(); err != nil {
			t.Errorf("%d bytes should pass: %v", n, err)
		}
	}
	c := valid() // unset passes
	c.Session.Key = secret.Secret{}
	if err := c.Validate(); err != nil {
		t.Errorf("unset key should pass: %v", err)
	}
}

func TestValidate_InstanceName_ReturnsInvalid(t *testing.T) {
	bad := []string{"", "Main", "a_b", "-a", "a-", strings.Repeat("a", 65), "a b", "a.b", " a", "é"}
	for _, n := range bad {
		c := valid()
		c.Instance.Name = n
		requireInvalidOne(t, c.Validate(), "BMS_INSTANCE_NAME")
	}
	good := []string{"main", "a", "0", "a-b", "a--b", "site-1", strings.Repeat("a", 64), "9a9"}
	for _, n := range good {
		c := valid()
		c.Instance.Name = n
		if err := c.Validate(); err != nil {
			t.Errorf("%q should pass: %v", n, err)
		}
	}
}

func TestValidate_HTTPAddr_ReturnsInvalid(t *testing.T) {
	for _, a := range []string{"", "8080", "127.0.0.1", "127.0.0.1:0", "127.0.0.1:70000", "127.0.0.1:abc", "127.0.0.1:-1"} {
		c := valid()
		c.HTTP.Addr = a
		requireInvalidOne(t, c.Validate(), "BMS_HTTP_ADDR")
	}
}

func TestValidate_TooManyTrustedProxies_ReturnsInvalid(t *testing.T) {
	mk := func(n int) []netip.Prefix {
		out := make([]netip.Prefix, n)
		for i := range out {
			out[i] = netip.MustParsePrefix(fmt.Sprintf("10.0.%d.1/32", i))
		}
		return out
	}
	c := valid()
	c.HTTP.TrustedProxies = mk(64)
	if err := c.Validate(); err != nil {
		t.Errorf("64 should pass: %v", err)
	}
	c.HTTP.TrustedProxies = mk(65)
	requireInvalidOne(t, c.Validate(), "BMS_HTTP_TRUSTED_PROXIES")
}

func TestValidate_InvalidHandBuiltPrefix_ReturnsInvalid(t *testing.T) {
	c := valid()
	c.HTTP.TrustedProxies = []netip.Prefix{{}}
	requireInvalidOne(t, c.Validate(), "BMS_HTTP_TRUSTED_PROXIES")
}

func TestValidate_ZeroBitTrustedProxy_ReturnsInvalid(t *testing.T) {
	for _, p := range []string{"0.0.0.0/0", "::/0"} {
		c := valid()
		c.HTTP.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix(p)}
		f := wantInvalidOne(t, c.Validate(), "BMS_HTTP_TRUSTED_PROXIES")
		if !strings.Contains(f.Reason, "trusts every peer") {
			t.Errorf("reason %q", f.Reason)
		}
	}
	c := valid()
	c.HTTP.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("0.0.0.0/1")}
	if err := c.Validate(); err != nil {
		t.Errorf("a one-bit prefix is allowed: %v", err)
	}
}

func TestValidate_DataDir_ReturnsInvalid(t *testing.T) {
	for _, d := range []string{"", "   ", "\t\n", "a\x00b"} {
		c := valid()
		c.Data.Dir = d
		requireInvalidOne(t, c.Validate(), "BMS_DATA_DIR")
	}
	for _, d := range []string{"", "  ", "a\x00b"} {
		c := valid()
		c.Secrets.File = d
		requireInvalidOne(t, c.Validate(), "BMS_SECRETS_FILE")
	}
}

func TestValidate_LogEnums_ReturnsInvalid(t *testing.T) {
	c := valid()
	c.Log.Level = slog.Level(3)
	f := wantInvalidOne(t, c.Validate(), "BMS_LOG_LEVEL")
	if f.Code != validate.CodeOneOf {
		t.Errorf("Code = %q", f.Code)
	}
	c = valid()
	c.Log.Format = "yaml"
	requireInvalidOne(t, c.Validate(), "BMS_LOG_FORMAT")
	c.Log.Format = ""
	requireInvalidOne(t, c.Validate(), "BMS_LOG_FORMAT")
	for _, l := range []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError} {
		c := valid()
		c.Log.Level = l
		if err := c.Validate(); err != nil {
			t.Errorf("%v should pass: %v", l, err)
		}
	}
}

func TestValidate_Modules_ReturnsInvalid(t *testing.T) {
	for name, items := range map[string][]string{
		"empty item": {"a", ""}, "blank item": {" "}, "duplicate": {"a", "b", "a"},
	} {
		t.Run(name, func(t *testing.T) {
			c := valid()
			c.Modules.Disabled = items
			requireInvalidOne(t, c.Validate(), "BMS_MODULES_DISABLED")
		})
	}
	c := valid()
	c.Modules.Disabled = strings.Split(itemList(65), ",")
	requireInvalidOne(t, c.Validate(), "BMS_MODULES_DISABLED")
	c.Modules.Disabled = nil
	if err := c.Validate(); err != nil {
		t.Error(err)
	}
}

func TestValidate_NonLoopbackBindWithoutProxy_ReturnsInvalid(t *testing.T) {
	for _, a := range []string{":8080", "0.0.0.0:8080", "[::]:8080", "192.0.2.10:8080", "[2001:db8::1]:8080", "example.invalid:8080", "128.0.0.1:80"} {
		t.Run(a, func(t *testing.T) {
			c := valid()
			c.HTTP.Addr = a
			f := wantInvalidOne(t, c.Validate(), "BMS_HTTP_ADDR")
			for _, w := range []string{"BMS_HTTP_TRUSTED_PROXIES", "BMS_HTTP_ALLOW_PLAINTEXT"} {
				if !strings.Contains(f.Reason, w) {
					t.Errorf("reason %q should name %s", f.Reason, w)
				}
			}
		})
	}
}

func TestValidate_LoopbackBinds_Pass(t *testing.T) {
	for _, a := range []string{"127.0.0.1:8080", "[::1]:8080", "localhost:8080", "127.0.0.5:9000"} {
		c := valid()
		c.HTTP.Addr = a
		if err := c.Validate(); err != nil {
			t.Errorf("%s should pass: %v", a, err)
		}
	}
}

func TestValidate_NonLoopbackBindWithTrustedProxy_Passes(t *testing.T) {
	c := valid()
	c.HTTP.Addr = ":8080"
	c.HTTP.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	if err := c.Validate(); err != nil {
		t.Error(err)
	}
}

func TestValidate_NonLoopbackBindWithAllowPlaintext_Passes(t *testing.T) {
	c := valid()
	c.HTTP.Addr = "0.0.0.0:8080"
	c.HTTP.AllowPlaintext = true
	if err := c.Validate(); err != nil {
		t.Error(err)
	}
}

func TestValidate_ReportsEveryProblemOnce(t *testing.T) {
	c := config.Default() // db.url unset
	c.DB.MaxConns = 0
	c.Instance.Name = "BAD"
	c.Log.Format = "x"
	err := c.Validate()
	if !errs.Is(err, errs.Invalid) {
		t.Fatalf("want Invalid, got %v", err)
	}
	want := []string{"BMS_INSTANCE_NAME", "BMS_DB_URL", "BMS_DB_MAX_CONNS", "BMS_LOG_FORMAT"}
	got := fieldNames(err)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("fields = %v, want %v", got, want)
	}
}

func TestValidate_DoesNoIO(t *testing.T) {
	c := valid()
	c.Data.Dir = "/nonexistent-dir-for-test"
	c.Secrets.File = "/nonexistent-dir-for-test/secrets.json"
	c.HTTP.Addr = "unresolvable.invalid:8080"
	c.HTTP.AllowPlaintext = true
	if err := c.Validate(); err != nil {
		t.Errorf("Validate must not touch the filesystem or DNS: %v", err)
	}
}

func TestConfig_SlogAnyAndJSONNeverLeakSecrets(t *testing.T) {
	c := mustLoad(t, env("BMS_SESSION_KEY="+sessionKey)...)
	var outs []string
	for _, mk := range []func(*strings.Builder) slog.Handler{
		func(b *strings.Builder) slog.Handler { return slog.NewTextHandler(b, nil) },
		func(b *strings.Builder) slog.Handler { return slog.NewJSONHandler(b, nil) },
	} {
		var b strings.Builder
		slog.New(mk(&b)).Info("start", slog.Any("config", c))
		outs = append(outs, b.String())
	}
	j, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	outs = append(outs, string(j), fmt.Sprintf("%+v", c), fmt.Sprintf("%v", c), fmt.Sprintf("%#v", c))
	for i, o := range outs {
		if strings.Contains(o, marker) {
			t.Errorf("output %d leaks a marker: %s", i, o)
		}
	}
	for _, i := range []int{0, 1, 2, 3} {
		if !strings.Contains(outs[i], "[redacted]") {
			t.Errorf("output %d lacks [redacted]: %s", i, outs[i])
		}
	}
}

func TestDefault_Unset_ShowsUnsetMarker(t *testing.T) {
	out := fmt.Sprintf("%+v", config.Default())
	if !strings.Contains(out, "[unset]") {
		t.Errorf("unset secrets should print [unset]: %s", out)
	}
}

// F31
func TestValidate_SealingKey_ReturnsInvalidWithoutEcho(t *testing.T) {
	for name, k := range map[string]string{
		"short": marker, "31": strings.Repeat(marker, 4)[:31], "1025": strings.Repeat(marker, 103)[:1025],
		"nul": strings.Repeat(marker, 4) + "\x00",
	} {
		t.Run(name, func(t *testing.T) {
			c := valid()
			c.Settings.SealingKey = secret.New(k)
			err := c.Validate()
			requireInvalidOne(t, err, "BMS_SETTINGS_SEALING_KEY")
			noLeak(t, err, marker)
		})
	}
	for _, n := range []int{32, 1024} {
		c := valid()
		c.Settings.SealingKey = secret.New(strings.Repeat("s", n))
		if err := c.Validate(); err != nil {
			t.Errorf("%d bytes should pass: %v", n, err)
		}
	}
	c := valid() // unset is never an error (decision 17)
	c.Settings.SealingKey = secret.Secret{}
	if err := c.Validate(); err != nil {
		t.Errorf("unset key should pass: %v", err)
	}
}
