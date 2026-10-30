package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/zedgama3/bms/internal/api"
	"github.com/zedgama3/bms/internal/config"
	"github.com/zedgama3/bms/internal/contracts/demoseed"
	"github.com/zedgama3/bms/internal/contracts/eventsetup"
	"github.com/zedgama3/bms/internal/contracts/membership"
	"github.com/zedgama3/bms/internal/contracts/personpage"
	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/demo"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

func TestStatusOf_Table(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		conflict int
		want     int
	}{
		{"invalid is usage", errs.E("t", errs.Invalid, errors.New("x")), ExitSchema, ExitUsage},
		{"conflict is the caller's status (schema)", errs.E("t", errs.Conflict, errors.New("x")), ExitSchema, ExitSchema},
		{"conflict is the caller's status (build)", errs.E("t", errs.Conflict, errors.New("x")), ExitUsage, ExitUsage},
		{"unavailable is a failure", errs.E("t", errs.Unavailable, errors.New("x")), ExitSchema, ExitFailure},
		{"canceled is a failure", errs.E("t", errs.Canceled, errors.New("x")), ExitSchema, ExitFailure},
		{"unclassified is a failure", errors.New("x"), ExitSchema, ExitFailure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := statusOf(tc.err, tc.conflict); got != tc.want {
				t.Errorf("statusOf = %d, want %d", got, tc.want)
			}
		})
	}
}

// Every key in the catalogue is rendered by check, and a set secret never shows.
func TestValueOf_EveryKeyRendered_SecretsRedacted(t *testing.T) {
	cfg := config.Default()
	cfg.DB.URL = secret.New("postgres://u:ZQ9MARKER7@h/db")
	cfg.Session.Key = secret.New(strings.Repeat("k", 40))
	cfg.Settings.SealingKey = secret.New(strings.Repeat("s", 40))
	cfg.Files.S3AccessKey = secret.New("AKZQ9MARKER7")
	cfg.Files.S3SecretKey = secret.New("SKZQ9MARKER7")
	for _, k := range config.Keys() {
		got := valueOf(cfg, k)
		if got == "[unknown key]" {
			t.Errorf("valueOf has no case for %s", k.Env)
		}
		if strings.Contains(got, "ZQ9MARKER7") || strings.Contains(got, "kkkk") || strings.Contains(got, "ssss") {
			t.Errorf("valueOf(%s) leaks a secret: %q", k.Env, got)
		}
		if k.Secret && got != "[redacted]" {
			t.Errorf("valueOf(%s) = %q, want [redacted]", k.Env, got)
		}
	}
	// The D53 and D54 keys render their real value, not the unknown-key marker.
	cfg.Demo.Enabled = true
	cfg.Demo.PublicURL = "https://demo.example.org"
	cfg.Telemetry.Usage = false
	want := map[string]string{
		"BMS_DEMO_ENABLED":    "true",
		"BMS_DEMO_PUBLIC_URL": "https://demo.example.org",
		"BMS_TELEMETRY_USAGE": "false",
	}
	found := map[string]bool{}
	for _, k := range config.Keys() {
		if w, ok := want[k.Env]; ok {
			found[k.Env] = true
			if got := valueOf(cfg, k); got != w {
				t.Errorf("valueOf(%s) = %q, want %q", k.Env, got, w)
			}
		}
	}
	for env := range want {
		if !found[env] {
			t.Errorf("config.Keys() lacks %s", env)
		}
	}
}

func TestCountOperations_CountsMethodsOnly(t *testing.T) {
	doc := `{"paths":{"/a":{"get":{},"post":{},"parameters":[]},"/b":{"delete":{},"summary":"x"}}}`
	n, err := countOperations([]byte(doc))
	if err != nil {
		t.Fatalf("countOperations: %v", err)
	}
	if n != 3 {
		t.Errorf("operations = %d, want 3", n)
	}
	if _, err := countOperations([]byte("not json")); !errs.Is(err, errs.Internal) {
		t.Errorf("a malformed document: err = %v, want Internal", err)
	}
}

func TestSay_FailedWriteIsDropped(t *testing.T) {
	var buf bytes.Buffer
	say(&buf, "hello %s", "there")
	sayln(&buf, "!")
	if got := buf.String(); got != "hello there!\n" {
		t.Errorf("output = %q", got)
	}
	say(failingWriter{}, "dropped") // must not panic or block
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("closed pipe") }

func TestEnabledModules_IsRegistryOrderMinusDisabled(t *testing.T) {
	got, err := enabledModules(testRegistryOf(t, "bravo", "alpha", "charlie"), []string{"bravo"})
	if err != nil {
		t.Fatalf("enabledModules: %v", err)
	}
	if want := "alpha,charlie"; strings.Join(got, ",") != want {
		t.Errorf("enabled = %v, want %s", got, want)
	}
}

// ---- F2, constants, assembly ----

func TestVersion_Table(t *testing.T) {
	old := version
	t.Cleanup(func() { version = old })

	for _, tc := range []struct {
		name    string
		inject  string
		want    string // "" means: any token
		invalid bool
	}{
		{"injected tag", "v1.2.3", "v1.2.3", false},
		{"injected describe", "v0.1-3-gabc1234-dirty", "v0.1-3-gabc1234-dirty", false},
		{"injected at the length limit", strings.Repeat("a", 64), strings.Repeat("a", 64), false},
		{"injected one byte over", strings.Repeat("a", 65), "", true},
		{"injected with a space", "v1 2", "", true},
		{"injected with a plus", "v1+dirty", "", true},
		{"not injected", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			version = tc.inject
			got, err := Version()
			if tc.invalid {
				if !errs.Is(err, errs.Invalid) {
					t.Fatalf("err = %v, want kind Invalid", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Version: %v", err)
			}
			if tc.want != "" && got != tc.want {
				t.Errorf("Version = %q, want %q", got, tc.want)
			}
			if checkVersion(got) != nil {
				t.Errorf("Version = %q is not a token of at most 64 bytes", got)
			}
		})
	}
}

func TestFromBuildInfo_Table(t *testing.T) {
	for _, tc := range []struct {
		raw    string
		want   string
		usable bool
	}{
		{"v1.4.0", "v1.4.0", true},
		{"v0.0.0-20261017-abcdef+dirty", "v0.0.0-20261017-abcdef-dirty", true},
		{"(devel)", "", false},
		{"", "", false},
		{"has space", "", false},
	} {
		got, ok := fromBuildInfo(tc.raw)
		if got != tc.want || ok != tc.usable {
			t.Errorf("fromBuildInfo(%q) = (%q, %v), want (%q, %v)", tc.raw, got, ok, tc.want, tc.usable)
		}
	}
}

func TestConstants_Pinned(t *testing.T) {
	for _, tc := range []struct {
		name      string
		got, want time.Duration
	}{
		{"buildTimeout", buildTimeout, 30 * time.Second},
		{"startTimeout", startTimeout, 30 * time.Second},
		{"listenTimeout", listenTimeout, 5 * time.Second},
		{"migrateTimeout", migrateTimeout, 30 * time.Minute},
		{"checkTimeout", checkTimeout, 30 * time.Second},
		{"handlerTimeout", handlerTimeout, 5 * time.Second},
		{"httpMinShare", httpMinShare, time.Second},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
		}
	}
	if openAPIVersion != "dev" {
		t.Errorf("openAPIVersion = %q, want dev", openAPIVersion)
	}
}

func TestAssembly_CorePermsStartWithModulesRead(t *testing.T) {
	a := newAssembly(&process{clock: clock.Real()}, config.Default())
	if len(a.corePerms) == 0 || a.corePerms[0].Name != api.PermissionModulesRead {
		t.Fatalf("corePerms = %v, want core.modules.read first", a.corePerms)
	}
	d := a.corePerms[0]
	if err := d.Validate(); err != nil {
		t.Errorf("the declaration does not validate: %v", err)
	}
	if d.Scope != authz.ScopeSite || d.Reaches != model.SensitivityInternal {
		t.Errorf("declaration = %+v, want site scope reaching internal", d)
	}
}

// ---- F28: parts and commands ----

// coreOrder is the order in which the core's services plug in (app.md "How later core
// services plug in"); coreParts must be a subsequence of it.
var coreOrder = []string{"audit", "identity", "settings", "authz", "people", "departments", "events", "jobs", "notify", "files", "demo"}

func TestParts_OrderAndNames_Pinned(t *testing.T) {
	pos := -1
	seen := map[string]bool{}
	for _, pt := range coreParts {
		if seen[pt.name] {
			t.Errorf("core part %q is listed twice", pt.name)
		}
		seen[pt.name] = true
		i := slices.Index(coreOrder, pt.name)
		if i < 0 {
			t.Errorf("core part %q is not a core service name", pt.name)
		}
		if i <= pos {
			t.Errorf("core part %q is out of dependency order", pt.name)
		}
		pos = i
	}
}

func TestCommands_UniqueNames(t *testing.T) {
	table, err := (&process{parts: coreParts}).commands()
	if err != nil {
		t.Fatalf("commands: %v", err)
	}
	seen := map[string]bool{}
	for _, c := range table {
		if seen[c.name] {
			t.Errorf("command %q appears twice", c.name)
		}
		seen[c.name] = true
	}
	for _, want := range []string{"serve", "migrate", "check", "openapi", "version", "help"} {
		if !seen[want] {
			t.Errorf("the built-in command %q is missing", want)
		}
	}
}

func TestCommands_PartProblems_ReturnInternal(t *testing.T) {
	noop := func(context.Context, *process) int { return ExitOK }
	declares := func(cs ...command) func(*assembly) error {
		return func(a *assembly) error { a.commands = append(a.commands, cs...); return nil }
	}
	for _, tc := range []struct {
		name  string
		parts []part
	}{
		{"a part's command repeats a built-in", []part{{name: "identity", declare: declares(command{name: "serve", run: noop})}}},
		{"two parts share a command", []part{
			{name: "identity", declare: declares(command{name: "rotate-secret", run: noop})},
			{name: "people", declare: declares(command{name: "rotate-secret", run: noop})},
		}},
		{"two parts share a name", []part{{name: "identity"}, {name: "identity"}}},
		{"a part has no name", []part{{}}},
		{"a part is not named after a core area", []part{{name: "bogus"}}},
		{"a command name breaks the grammar", []part{{name: "identity", declare: declares(command{name: "Bad_Name", run: noop})}}},
		{"a command has no run function", []part{{name: "identity", declare: declares(command{name: "export"})}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := (&process{parts: tc.parts}).commands(); !errs.Is(err, errs.Internal) {
				t.Errorf("err = %v, want kind Internal", err)
			}
			var out, errOut bytes.Buffer
			p := &process{args: []string{"help"}, parts: tc.parts, stdout: &out, stderr: &errOut, clock: clock.Real(), registry: module.NewRegistry()}
			if got := p.run(context.Background()); got != ExitFailure {
				t.Errorf("run = %d, want ExitFailure", got)
			}
		})
	}
}

func TestCommands_PartCommandsJoinTheTableAfterBuiltins(t *testing.T) {
	noop := func(context.Context, *process) int { return ExitOK }
	parts := []part{{name: "people", declare: func(a *assembly) error {
		a.commands = append(a.commands, command{name: "export", summary: "export one person", usage: "<person-id>", run: noop})
		return nil
	}}}
	var out, errOut bytes.Buffer
	p := &process{args: []string{"help"}, parts: parts, stdout: &out, stderr: &errOut, clock: clock.Real(), registry: module.NewRegistry()}
	if got := p.run(context.Background()); got != ExitOK {
		t.Fatalf("run = %d, want ExitOK; stderr: %s", got, &errOut)
	}
	if !strings.Contains(out.String(), "export   export one person  <person-id>") {
		t.Errorf("help lacks the part's command with its usage:\n%s", &out)
	}
	// Its argument count is enforced.
	p = &process{args: []string{"export"}, parts: parts, stdout: &out, stderr: &errOut, clock: clock.Real(), registry: module.NewRegistry()}
	if got := p.run(context.Background()); got != ExitUsage {
		t.Errorf("export without its argument = %d, want ExitUsage", got)
	}
}

// ---- F29: provision ----

func provisionAssembly(t *testing.T) (*assembly, *telemetrytest.Capture) {
	t.Helper()
	c := telemetrytest.New(t)
	tr, err := c.Telemetry().Tracer("signals")
	if err != nil {
		t.Fatal(err)
	}
	m, err := c.Telemetry().Meter("signals")
	if err != nil {
		t.Fatal(err)
	}
	disp, err := signals.New(signals.Options{
		Logger: c.Logger(), Tracer: tr, Meter: m, Clock: clock.NewFake(time.Now()), HandlerTimeout: handlerTimeout,
	})
	if err != nil {
		t.Fatalf("signals.New: %v", err)
	}
	a := &assembly{tel: c.Telemetry(), disp: disp, clock: clock.NewFake(time.Now()), dbc: db.None()}
	a.setTelemetry(c.Telemetry())
	return a, c
}

func TestProvision_ScopesAndLoggerFields(t *testing.T) {
	a, c := provisionAssembly(t)
	deps, err := a.provision(context.Background(), module.Manifest{Name: "zqalpha", Publishes: []signals.Name{"zqalpha.done"}})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if deps.Clock != a.clock || deps.DB != a.dbc || deps.Links != nil {
		t.Errorf("Clock, DB or Links wrong: %+v", deps)
	}

	deps.Logger.InfoContext(context.Background(), "hello from the module")
	found := false
	for _, r := range c.Records() {
		if r.Message == "hello from the module" {
			found = true
			if r.Attrs["module"] != "zqalpha" {
				t.Errorf("logger attrs = %v, want module=zqalpha", r.Attrs)
			}
		}
	}
	if !found {
		t.Error("the module's log record was not captured")
	}

	_, span := deps.Tracer.Start(context.Background(), "zqalpha.work")
	span.End()
	scoped := false
	for _, s := range c.Spans() {
		if s.Name == "zqalpha.work" {
			scoped = s.Scope == "module.zqalpha"
		}
	}
	if !scoped {
		t.Errorf("the module's span is not in scope module.zqalpha: %+v", c.Spans())
	}

	// The bus accepts a declared name and refuses an undeclared one.
	if err := deps.Signals.Publish(context.Background(), "zqalpha.undeclared", nil); !errs.Is(err, errs.Invalid) {
		t.Errorf("publishing an undeclared signal: err = %v, want Invalid", err)
	}
	if err := deps.Signals.Publish(context.Background(), "zqalpha.done", nil); err != nil {
		t.Errorf("publishing a declared signal: %v", err)
	}
}

func TestProvision_ScopeConflict_ReturnsKind(t *testing.T) {
	a, _ := provisionAssembly(t)
	m := module.Manifest{Name: "zqalpha"}
	if _, err := a.provision(context.Background(), m); err != nil {
		t.Fatalf("first provision: %v", err)
	}
	_, err := a.provision(context.Background(), m)
	if !errs.Is(err, errs.Conflict) {
		t.Fatalf("second provision: err = %v, want Conflict", err)
	}
	if !strings.Contains(err.Error(), "zqalpha") {
		t.Errorf("the error does not name the module: %v", err)
	}
}

// ---- routes: core route sets come after the modules' ----

func TestAssembly_RouteSetsAppendedAfterModules(t *testing.T) {
	rec := &recorder{}
	reg := recRegistry(t, rec, "zqalpha", "zqbeta")
	parts := []part{{
		name: "identity",
		build: func(_ context.Context, a *assembly) error {
			a.routeSets = append(a.routeSets, module.ModuleRoutes{
				Module: "accounts", Routes: []module.Route{pingRoute()}, OpenAPI: pingFragment,
			})
			return nil
		},
		link: func(_ context.Context, a *assembly) error {
			a.routeSets = append(a.routeSets, module.ModuleRoutes{
				Module: "people", Routes: []module.Route{pingRoute()}, OpenAPI: pingFragment,
			})
			return nil
		},
	}}
	c := telemetrytest.New(t)
	a := newAssembly(&process{clock: clock.Real(), parts: parts, version: "dev"}, config.Default())
	a.setTelemetry(c.Telemetry())
	ctx := context.Background()
	if err := a.resolve(ctx, reg, []string{"zqalpha", "zqbeta"}); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if err := a.newDispatcher(); err != nil {
		t.Fatalf("newDispatcher: %v", err)
	}
	a.dbc = db.None()
	if err := a.buildModules(ctx, skipSchema{}); err != nil {
		t.Fatalf("buildModules: %v", err)
	}
	opts, err := a.apiOptions("dev", noDatabase{}, func(context.Context) ([]db.Status, error) { return nil, nil })
	if err != nil {
		t.Fatalf("apiOptions: %v", err)
	}
	if ev := rec.list(); len(ev) != 0 {
		t.Errorf("building the modules started or stopped something: %v", ev)
	}
	var got []string
	for _, rs := range opts.Routes {
		got = append(got, rs.Module)
	}
	if want := "zqalpha,zqbeta,accounts,people"; strings.Join(got, ",") != want {
		t.Errorf("route sets = %v, want %s (modules in build order, then the core's in part order)", got, want)
	}
	srv, err := newServer(opts)
	if err != nil {
		t.Fatalf("api.New over the sets: %v", err)
	}
	var doc struct {
		Paths map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(srv.OpenAPI(), &doc); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/api/zqalpha/ping", "/api/zqbeta/ping", "/api/accounts/ping", "/api/people/ping"} {
		if _, ok := doc.Paths[p]; !ok {
			t.Errorf("the document lacks %s", p)
		}
	}
}

// ---- check keeps going; interrupts are worded alike ----

func quietEnv(t *testing.T, extra ...string) []string {
	t.Helper()
	return append([]string{
		"BMS_DB_URL=postgres://u:ZQ9MARKER7@127.0.0.1:1/db?sslmode=disable",
		"BMS_SESSION_KEY=" + strings.Repeat("k", 40),
		"BMS_SETTINGS_SEALING_KEY=" + strings.Repeat("s", 40),
		"BMS_DATA_DIR=" + t.TempDir(),
		"BMS_DB_CONNECT_TIMEOUT=1s",
	}, extra...)
}

// A broken OTEL_* variable is reported, and check still reports the database.
func TestCheck_BadOTELVariable_StillReportsDatabase(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Main(context.Background(), []string{"check"}, quietEnv(t, "OTEL_SDK_DISABLED=banana"), &out, &errOut)
	if code != ExitUsage {
		t.Errorf("status = %d, want ExitUsage (the worst problem found)", code)
	}
	for _, want := range []string{"OTEL_SDK_DISABLED", "database: ", "modules", "result: "} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("stdout lacks %q:\n%s", want, &out)
		}
	}
}

// F14: a signal before anything started reads the same everywhere.
func TestMigrate_CanceledBeforeStart_SaysInterrupted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, cmd := range []string{"migrate", "serve"} {
		var out, errOut bytes.Buffer
		if code := Main(ctx, []string{cmd}, quietEnv(t), &out, &errOut); code != ExitFailure {
			t.Errorf("%s: status = %d, want ExitFailure", cmd, code)
		}
		if !strings.Contains(errOut.String(), interruptedText) {
			t.Errorf("%s: stderr = %q, want %q", cmd, &errOut, interruptedText)
		}
	}
}

func TestOpenAPI_OutputIsIndentedWithOneTrailingNewline(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Main(context.Background(), []string{"openapi"}, nil, &out, &errOut); code != ExitOK {
		t.Fatalf("status = %d\n%s", code, &errOut)
	}
	s := out.String()
	if !strings.HasPrefix(s, "{\n  \"") || !strings.HasSuffix(s, "}\n") || strings.HasSuffix(s, "\n\n") {
		t.Errorf("the document is not two-space indented with exactly one trailing newline: %.60q ... %q", s, s[max(0, len(s)-4):])
	}
}

// Item 19: with both generated keys injected there is nothing to write, so migrate must not
// create the data directory (a read-only filesystem works).
func TestEnsureSecrets_SessionKeyAlreadySet_CreatesNoDataDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent", "data")
	var cfg config.Config
	cfg.Data.Dir = dir
	cfg.Secrets.File = filepath.Join(dir, "secrets.env")
	cfg.Session.Key = secret.New(strings.Repeat("k", 40))
	cfg.Settings.SealingKey = secret.New(strings.Repeat("s", 40))
	p := &process{stderr: &bytes.Buffer{}}
	if _, err := ensureSecrets(context.Background(), p, cfg); err != nil {
		t.Fatalf("ensureSecrets: %v", err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("stat %s = %v, want the directory not to exist", dir, err)
	}
}

// Config decision 17 and item 19: with the session key injected, a sealing key that cannot
// be written (the directory or the file is unwritable) is one warning, never an error, and
// the key stays unset.
func TestEnsureSecrets_SessionKeyInjected_UnwritableSealingKey_WarnsAndSucceeds(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := map[string]config.Config{
		"data dir under a file":     {Data: config.Data{Dir: filepath.Join(blocker, "data")}, Secrets: config.Secrets{File: filepath.Join(blocker, "data", "secrets.json")}},
		"secrets file under a file": {Data: config.Data{Dir: root}, Secrets: config.Secrets{File: filepath.Join(blocker, "secrets.json")}},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			cfg.Session.Key = secret.New(strings.Repeat("k", 40))
			var errOut bytes.Buffer
			got, err := ensureSecrets(context.Background(), &process{stderr: &errOut}, cfg)
			if err != nil {
				t.Fatalf("ensureSecrets: %v, want a warning only", err)
			}
			if got.Settings.SealingKey.IsSet() {
				t.Error("the sealing key is set although nothing could be written")
			}
			if strings.Count(errOut.String(), "warning:") != 1 || !strings.Contains(errOut.String(), "BMS_SETTINGS_SEALING_KEY") {
				t.Errorf("stderr = %q, want one warning naming BMS_SETTINGS_SEALING_KEY", errOut.String())
			}
		})
	}
}

// The session key still being missing keeps an unwritable directory an error (F22, F23).
func TestEnsureSecrets_NoSessionKey_UnwritableDir_ReturnsInvalid(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Data: config.Data{Dir: filepath.Join(blocker, "data")}, Secrets: config.Secrets{File: filepath.Join(blocker, "data", "secrets.json")}}
	if _, err := ensureSecrets(context.Background(), &process{stderr: &bytes.Buffer{}}, cfg); !errs.Is(err, errs.Invalid) {
		t.Errorf("err = %v, want Invalid", err)
	}
}

// The notice follows whether the file existed: a new file is "Created ..." whichever fields
// it holds, an existing one gains fields with "Added ...".
func TestEnsureSecrets_NoticeFollowsWhetherTheFileExisted(t *testing.T) {
	env := func(dir string) []string {
		return []string{"BMS_DB_URL=postgres://u:p@127.0.0.1:1/db", "BMS_SESSION_KEY=" + strings.Repeat("k", 40), "BMS_DATA_DIR=" + dir}
	}
	t.Run("new file with only the sealing key", func(t *testing.T) {
		dir := t.TempDir()
		cfg, err := config.Load(context.Background(), env(dir))
		if err != nil {
			t.Fatal(err)
		}
		var errOut bytes.Buffer
		if _, err := ensureSecrets(context.Background(), &process{stderr: &errOut, environ: env(dir)}, cfg); err != nil {
			t.Fatalf("ensureSecrets: %v", err)
		}
		for _, want := range []string{"Created ", "Keep it readable by the bms user only"} {
			if !strings.Contains(errOut.String(), want) {
				t.Errorf("stderr lacks %q:\n%s", want, &errOut)
			}
		}
	})
	t.Run("existing file gains the sealing key", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "secrets.json"), []byte(`{"other":"x"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := config.Load(context.Background(), env(dir))
		if err != nil {
			t.Fatal(err)
		}
		var errOut bytes.Buffer
		if _, err := ensureSecrets(context.Background(), &process{stderr: &errOut, environ: env(dir)}, cfg); err != nil {
			t.Fatalf("ensureSecrets: %v", err)
		}
		if !strings.Contains(errOut.String(), "Added ") || strings.Contains(errOut.String(), "Created ") {
			t.Errorf("stderr = %q, want the Added notice only", &errOut)
		}
	})
}

// Item 21: check exits like serve when no session key exists, and still says what to run.
func TestCheck_NoSessionKey_ExitsUsageAndSaysMigrate(t *testing.T) {
	env := slices.DeleteFunc(quietEnv(t), func(e string) bool { return strings.HasPrefix(e, "BMS_SESSION_KEY=") })
	var out, errOut bytes.Buffer
	if code := Main(context.Background(), []string{"check"}, env, &out, &errOut); code != ExitUsage {
		t.Errorf("status = %d, want ExitUsage\n%s", code, &out)
	}
	for _, want := range []string{"run bms migrate", "result: "} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("stdout lacks %q:\n%s", want, &out)
		}
	}
}

// The debug-logging rule (owner, 2026-10-17): at debug level every command says so.
func TestDebugLogging_EveryCommandSaysSo(t *testing.T) {
	t.Run("check reports it in its configuration section", func(t *testing.T) {
		var out, errOut bytes.Buffer
		Main(context.Background(), []string{"check"}, quietEnv(t, "BMS_LOG_LEVEL=debug"), &out, &errOut)
		if !strings.Contains(out.String(), "log level: debug (logs may contain personal data)") {
			t.Errorf("stdout lacks the log level line:\n%s", &out)
		}
	})
	t.Run("migrate prints the warning to stderr", func(t *testing.T) {
		var out, errOut bytes.Buffer
		Main(context.Background(), []string{"migrate"}, quietEnv(t, "BMS_LOG_LEVEL=debug"), &out, &errOut)
		if !strings.HasPrefix(errOut.String(), debugWarning+"\n") {
			t.Errorf("stderr does not start with the warning:\n%s", &errOut)
		}
	})
	t.Run("serve logs a warning right after bms starting", func(t *testing.T) {
		var out, errOut bytes.Buffer
		Main(context.Background(), []string{"serve"}, quietEnv(t, "BMS_LOG_LEVEL=debug"), &out, &errOut)
		var starting, warned bool
		for line := range strings.SplitSeq(out.String(), "\n") {
			if line == "" {
				continue
			}
			var rec map[string]any
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				t.Fatalf("log line is not JSON: %v", err)
			}
			switch rec["msg"] {
			case "bms starting":
				starting = true
			case debugWarning:
				if !starting {
					t.Error("the warning precedes \"bms starting\"")
				}
				if rec["level"] != "WARN" || rec["log.level"] != "debug" {
					t.Errorf("warning record = %v, want level WARN and log.level=debug", rec)
				}
				warned = true
			}
		}
		if !warned {
			t.Errorf("no debug warning in the log:\n%s", &out)
		}
	})
	t.Run("nothing is said at info level", func(t *testing.T) {
		for _, cmd := range []string{"check", "migrate", "serve"} {
			var out, errOut bytes.Buffer
			Main(context.Background(), []string{cmd}, quietEnv(t), &out, &errOut)
			if strings.Contains(out.String()+errOut.String(), "debug") && strings.Contains(out.String()+errOut.String(), "personal data") {
				t.Errorf("%s at info level mentions debug logging:\n%s%s", cmd, &out, &errOut)
			}
		}
	})
}

// ---- audit (part 1) and identity (part 2) ----

// declared runs the declare hooks of the real coreParts over a fresh assembly.
func declared(t *testing.T) *assembly {
	t.Helper()
	a := newAssembly(&process{clock: clock.Real(), parts: coreParts, version: "dev"}, config.Default())
	if err := a.declare(); err != nil {
		t.Fatalf("declare: %v", err)
	}
	return a
}

func TestCoreParts_Declare_SetsPermissionsSignalAndCommand(t *testing.T) {
	a := declared(t)
	var sets []string
	for _, s := range a.coreSets {
		sets = append(sets, s.Name)
	}
	if got := strings.Join(sets, ","); got != "identity,people,authz,departments,events,audit,settings,jobs,notify,files,demo" {
		t.Errorf("core migration sets = %s, want identity,people,authz,departments,events,audit,settings,jobs,notify,files,demo (db.md order, not part order)", got)
	}
	var perms []string
	for _, d := range a.corePerms {
		perms = append(perms, string(d.Name))
	}
	want := "core.modules.read,audit.search,audit.export,accounts.read,accounts.manage," +
		"settings.read,settings.manage," +
		"access.roles.read,access.roles.manage,access.rules.manage,access.grants.read,access.grants.manage,access.department-grants.manage," +
		"people.directory.read,people.contacts.read,people.profiles.manage,people.data.export,people.data.delete," +
		"departments.manage,departments.details.manage,events.manage," +
		"jobs.read,jobs.manage," +
		"notify.templates.manage,notify.outbox.manage"
	if got := strings.Join(perms, ","); got != want {
		t.Errorf("corePerms = %s, want %s (api first, then each part's in part order)", got, want)
	}
	for _, want := range []signals.Name{
		"identity.account-created", "access.grant-created", "access.grant-revoked",
		"people.person-created", "people.person-deleted", "people.deletion-requested",
		"departments.department-created", "departments.department-archived", "departments.department-restored",
		"events.event-created", "events.event-changed", "events.event-closed", "events.event-reopened",
	} {
		if !slices.Contains(a.coreSignals, want) {
			t.Errorf("coreSignals = %v lacks %s", a.coreSignals, want)
		}
	}
	if len(a.corePoints) != 4 || a.corePoints[3].Name != demoseed.Point || a.corePoints[0].Name != personpage.Point || a.corePoints[1].Name != membership.Point || a.corePoints[2].Name != eventsetup.Point {
		t.Errorf("corePoints = %v, want %s, %s, %s then %s", a.corePoints, personpage.Point, membership.Point, eventsetup.Point, demoseed.Point)
	}
	for _, pt := range a.corePoints {
		if pt.Accepts(struct{}{}) {
			t.Errorf("the %s point accepts a value that is not its provider", pt.Name)
		}
	}
	usage := map[string]string{}
	for _, c := range a.commands {
		usage[c.name] = c.usage
	}
	for name, want := range map[string]string{"create-superuser": "<email>", "export": "<person-id>", "delete": "<person-id>"} {
		if got, ok := usage[name]; !ok || got != want {
			t.Errorf("command %s usage = %q (declared %v), want %q", name, got, ok, want)
		}
	}
}

func TestAddCoreSet_Table(t *testing.T) {
	set := func(name string) db.Set { return db.Set{Name: name, Schema: "core", Files: fstest.MapFS{}} }
	for _, tc := range []struct {
		name string
		add  []string
		want string
		fail bool // errs.Internal, whose value is the zero Kind
	}{
		{"audit then identity is sorted", []string{"audit", "identity"}, "identity,audit", false},
		{"identity then audit stays", []string{"identity", "audit"}, "identity,audit", false},
		{"a later set goes last", []string{"identity", "files", "audit"}, "identity,audit,files", false},
		{"unknown set is internal", []string{"zq"}, "", true},
		{"duplicate set is internal", []string{"audit", "audit"}, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &assembly{}
			var err error
			for _, n := range tc.add {
				if err = a.addCoreSet(set(n)); err != nil {
					break
				}
			}
			if tc.fail {
				if err == nil || !errs.Is(err, errs.Internal) {
					t.Fatalf("err = %v, want Internal", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("addCoreSet: %v", err)
			}
			var got []string
			for _, s := range a.coreSets {
				got = append(got, s.Name)
			}
			if strings.Join(got, ",") != tc.want {
				t.Errorf("sets = %v, want %s", got, tc.want)
			}
		})
	}
}

func TestActorFromSession_Table(t *testing.T) {
	person := model.NewID[model.PersonID]()
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	session := func(super, restricted bool) identity.Session {
		return identity.Session{
			ID: model.NewID[identity.SessionID](), Account: model.NewID[model.AccountID](), Person: person,
			Superuser: super, PasswordChangeRequired: restricted, IssuedAt: now, ExpiresAt: now.Add(time.Hour),
		}
	}
	for _, tc := range []struct {
		name string
		ctx  context.Context
		want audit.Actor
		ok   bool
	}{
		{"no session", context.Background(), audit.Actor{}, false},
		{"participant", identity.WithSession(context.Background(), session(false, false)), audit.Actor{Kind: audit.ActorPerson, Person: person}, true},
		{"superuser is marked", identity.WithSession(context.Background(), session(true, false)), audit.Actor{Kind: audit.ActorPerson, Person: person, Superuser: true}, true},
		{"a restricted session is still the person", identity.WithSession(context.Background(), session(false, true)), audit.Actor{Kind: audit.ActorPerson, Person: person}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := actorFromSession(tc.ctx)
			if ok != tc.ok || got.Kind != tc.want.Kind || got.Person != tc.want.Person || got.Superuser != tc.want.Superuser {
				t.Errorf("actorFromSession = %+v, %v; want %+v, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}

type allowOnly authz.Permission

func (a allowOnly) Allow(_ context.Context, p authz.Permission) error {
	if authz.Permission(a) == p {
		return nil
	}
	return errs.E("test.Allow", errs.Permission, errors.New("no"))
}

func TestLazyPermissions_DeniesUntilAuthzSetsTheChecker(t *testing.T) {
	a := &assembly{}
	l := lazyPermissions{a: a}
	if err := l.Allow(context.Background(), audit.PermissionSearch); !errs.Is(err, errs.Permission) {
		t.Fatalf("with no authorizer: err = %v, want Permission", err)
	}
	a.permissions = allowOnly(audit.PermissionSearch)
	if err := l.Allow(context.Background(), audit.PermissionSearch); err != nil {
		t.Errorf("after authz is wired the adapter must ask it: %v", err)
	}
	if err := l.Allow(context.Background(), audit.PermissionExport); !errs.Is(err, errs.Permission) {
		t.Errorf("another permission: err = %v, want Permission", err)
	}
}

func TestAccountRoutes_PublicMapsToAccessPublic(t *testing.T) {
	h := http.NotFoundHandler()
	got := accountRoutes([]identity.Route{
		{Method: "GET", Pattern: "/session", Public: true, Handler: h},
		{Method: "GET", Pattern: "/", Handler: h},
	})
	if len(got) != 2 || got[0].Access != module.AccessPublic || got[1].Access != module.AccessSignedIn {
		t.Errorf("routes = %+v, want public then signed-in", got)
	}
	for _, r := range auditRoutes([]audit.Route{{Method: "GET", Pattern: "/entries", Handler: h}}) {
		if r.Access != module.AccessSignedIn {
			t.Errorf("audit route %s is %v, want signed-in", r.Pattern, r.Access)
		}
	}
}

// builtCore builds the real parts over a database that refuses everything (the openapi path).
func builtCore(t *testing.T) *assembly {
	t.Helper()
	a, _ := provisionAssembly(t)
	a.cfg = config.Default()
	a.cfg.Instance.Name = "zqtest"
	a.cfg.Data.Dir = t.TempDir()
	a.cfg.Session.Key = secret.New(placeholderSessionKey)
	a.corePerms, a.parts = nil, coreParts
	if err := a.buildParts(context.Background()); err != nil {
		t.Fatalf("buildParts over db.None(): %v", err)
	}
	return a
}

func TestBuildParts_AgainstNoDatabase_SetsAuthenticatorRoutesAndRunnable(t *testing.T) {
	a := builtCore(t)
	if a.authenticator == nil || a.identity == nil || a.deps.audit == nil {
		t.Fatalf("authenticator=%v identity=%v audit=%v, want all set", a.authenticator, a.identity, a.deps.audit)
	}
	var areas []string
	for _, rs := range a.routeSets {
		areas = append(areas, rs.Module)
		if len(rs.Routes) == 0 || len(rs.OpenAPI) == 0 {
			t.Errorf("route set %s has %d routes and %d bytes of fragment", rs.Module, len(rs.Routes), len(rs.OpenAPI))
		}
	}
	if strings.Join(areas, ",") != "audit,accounts,settings,access,people,departments,events,jobs,notify" {
		t.Errorf("route sets = %v, want audit, accounts, settings, access, people, departments, events, jobs, notify", areas)
	}
	var runnables []string
	for _, r := range a.runnables {
		runnables = append(runnables, r.name)
	}
	if strings.Join(runnables, ",") != "audit,settings,notify" {
		t.Errorf("runnables = %v, want the audit importer, then the settings refresher, then the notify sender", runnables)
	}
	if a.settings == nil || a.deps.settings != a.settings {
		t.Errorf("settings = %v, deps.settings = %v, want the one service in both", a.settings, a.deps.settings)
	}
}

// settings' public routes (the public document, the stylesheet and, from 2026-10-21, the logo) need no session; every other one is signed in and checks its
// permission inside the use case.
func TestSettingsRoutes_PublicOnlyThePublicDocumentTheStylesheetAndTheLogo(t *testing.T) {
	a := builtCore(t)
	var got []string
	for _, rs := range a.routeSets {
		if rs.Module != "settings" {
			continue
		}
		for _, r := range rs.Routes {
			if r.Access == module.AccessPublic {
				got = append(got, r.Method+" "+r.Pattern)
			}
		}
	}
	slices.Sort(got)
	if want := []string{"GET /logo", "GET /public", "GET /theme.css"}; !slices.Equal(got, want) {
		t.Errorf("public settings routes = %v, want %v", got, want)
	}
}

// Deps.Settings is scoped to the module by name; a module that declares no setting has no
// area and gets no Reader.
func TestProvision_ScopesSettingsPerModule(t *testing.T) {
	a := builtCore(t)
	deps, err := a.provision(context.Background(), module.Manifest{Name: "zqalpha"})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if deps.Settings != nil {
		t.Errorf("a module with no setting got a Reader: %v", deps.Settings)
	}
	deps, err = a.provision(context.Background(), module.Manifest{
		Name: "audit", Settings: []module.SettingDecl{{Key: "retention", Kind: module.SettingText}},
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if deps.Settings == nil {
		t.Error("a module with a declared area got no Reader")
	}
	if _, err := a.provision(context.Background(), module.Manifest{
		Name: "zqbeta", Settings: []module.SettingDecl{{Key: "k", Kind: module.SettingText}},
	}); !errs.Is(err, errs.Invalid) {
		t.Errorf("a module whose area the service does not know: err = %v, want Invalid", err)
	}
}

// settings is built before authz, so its authorizer denies until authz is wired and then asks it.
func TestLazyAuthorizer_BeforeAuthz_DeniesAndEmptyExtent(t *testing.T) {
	a := &assembly{}
	l := lazyAuthorizer{a: a}
	ctx := context.Background()
	if err := l.Allow(ctx, settings.PermissionRead); !errs.Is(err, errs.Permission) {
		t.Errorf("Allow: err = %v, want Permission", err)
	}
	if err := l.AllowIn(ctx, settings.PermissionRead, model.DepartmentID{1}); !errs.Is(err, errs.Permission) {
		t.Errorf("AllowIn: err = %v, want Permission", err)
	}
	ext, err := l.ExtentOf(ctx, settings.PermissionRead)
	if err != nil || ext.Site || len(ext.Departments) != 0 {
		t.Errorf("ExtentOf = (%+v, %v), want a zero Extent and nil", ext, err)
	}
}

func TestProvision_ScopesAuditPerModule(t *testing.T) {
	a := builtCore(t)
	deps, err := a.provision(context.Background(), module.Manifest{Name: "zqalpha"})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if deps.Audit == nil {
		t.Fatal("Deps.Audit is nil")
	}
	// An action outside the module's area is refused before anything is written.
	err = deps.Audit.Record(context.Background(), audit.Entry{Action: "zqbeta.thing.read", Outcome: audit.Succeeded})
	if !errs.Is(err, errs.Invalid) {
		t.Errorf("a foreign action: err = %v, want Invalid", err)
	}
}

func TestClosers_AuditCloseIsAfterModulesAndBeforeDB(t *testing.T) {
	a := builtCore(t)
	var names []string
	for _, c := range a.closers() {
		names = append(names, c.name)
	}
	if got, want := strings.Join(names, ","), "http,runnables,signals,modules,audit,db,telemetry"; got != want {
		t.Errorf("closers = %s, want %s", got, want)
	}
}

func TestCheckSchemas_CoreAndModuleSets_ReportedTogether(t *testing.T) {
	a := declared(t)
	boom := errs.E("test.CheckSchema", errs.Conflict, errors.New("behind"))
	var checked []string
	schema := schemaFunc(func(_ context.Context, name string, _ fs.FS) error {
		checked = append(checked, name)
		return boom
	})
	err := a.checkSchemas(context.Background(), schema, []module.Manifest{{Name: "zqmod", Migrations: fstest.MapFS{}}, {Name: "zqplain"}})
	if !errs.Is(err, errs.Conflict) {
		t.Fatalf("err = %v, want Conflict", err)
	}
	if got := strings.Join(checked, ","); got != "identity,people,authz,departments,events,audit,settings,jobs,notify,files,demo,zqmod" {
		t.Errorf("checked = %s, want identity,people,authz,departments,events,audit,settings,jobs,notify,files,demo,zqmod (every set, none stopped the rest, no schema-less module)", got)
	}
	// The first problem that is not a Conflict decides the kind, not the last.
	mixed := schemaFunc(func(_ context.Context, name string, _ fs.FS) error {
		switch name {
		case "identity":
			return errs.E("test.CheckSchema", errs.Unavailable, errors.New("down"))
		case "audit":
			return errs.E("test.CheckSchema", errs.Invalid, errors.New("bad"))
		}
		return boom
	})
	if err := a.checkSchemas(context.Background(), mixed, nil); !errs.Is(err, errs.Unavailable) {
		t.Errorf("mixed kinds: err = %v, want the first non-Conflict kind (Unavailable)", err)
	}
	// Current core sets leave the modules to Plan.Build: they are not checked twice.
	checked = nil
	core := schemaFunc(func(_ context.Context, name string, _ fs.FS) error {
		checked = append(checked, name)
		return nil
	})
	if err := a.checkSchemas(context.Background(), core, []module.Manifest{{Name: "zqmod", Migrations: fstest.MapFS{}}}); err != nil {
		t.Errorf("current core sets: %v", err)
	}
	if got := strings.Join(checked, ","); got != "identity,people,authz,departments,events,audit,settings,jobs,notify,files,demo" {
		t.Errorf("checked = %s, want only the core sets when they are current", got)
	}
	unavailable := schemaFunc(func(context.Context, string, fs.FS) error {
		return errs.E("test.CheckSchema", errs.Unavailable, errors.New("down"))
	})
	if err := a.checkSchemas(context.Background(), unavailable, nil); !errs.Is(err, errs.Unavailable) {
		t.Errorf("a non-conflict problem: err = %v, want Unavailable", err)
	}
	if err := a.checkSchemas(context.Background(), skipSchema{}, nil); err != nil {
		t.Errorf("skipSchema: %v", err)
	}
}

type schemaFunc func(ctx context.Context, name string, files fs.FS) error

func (f schemaFunc) CheckSchema(ctx context.Context, name string, files fs.FS) error {
	return f(ctx, name, files)
}

func TestImporter_StartCreatesSpoolDirAndStopWaits(t *testing.T) {
	a := builtCore(t)
	r := a.runnables[0]
	ctx := context.Background()
	if err := r.stop(ctx); err != nil {
		t.Fatalf("stop before start: %v", err)
	}
	// Run needs a database that answers; db.None() refuses, so the importer only logs and
	// retries; start and stop still hold.
	if err := r.start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	if st, err := os.Stat(filepath.Join(a.cfg.Data.Dir, filepath.FromSlash(audit.SpoolDirName))); err != nil || !st.IsDir() {
		t.Errorf("the spool directory was not created: %v", err)
	}
	sctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := r.stop(sctx); err != nil {
		t.Errorf("stop: %v", err)
	}
}

func TestImporter_StartWithUnusableDataDir_ReturnsInvalid(t *testing.T) {
	a := builtCore(t)
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	im := &importer{log: a.deps.audit, logger: a.logger, dir: filepath.Join(blocker, "audit", "spool")}
	if err := im.start(context.Background()); !errs.Is(err, errs.Invalid) {
		t.Fatalf("start under a regular file: err = %v, want Invalid", err)
	}
}

func TestCheck_AuditSpool_CountsWaitingAndFailedFiles(t *testing.T) {
	cfg := config.Default()
	cfg.Data.Dir = t.TempDir()
	var out bytes.Buffer
	r := &checkReport{out: &out}
	r.auditSpool(cfg)
	if !strings.Contains(out.String(), "audit spool: empty") {
		t.Errorf("no spool directory: %q", out.String())
	}
	dir := filepath.Join(cfg.Data.Dir, filepath.FromSlash(audit.SpoolDirName))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"1-1-aa.jsonl", "2-1-bb.jsonl", "3-1-cc.jsonl.failed", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, n), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	out.Reset()
	r.auditSpool(cfg)
	if !strings.Contains(out.String(), "audit spool: 2 waiting, 1 failed") {
		t.Errorf("spool report = %q", out.String())
	}
	if r.problems != 0 {
		t.Errorf("a spool is not a problem (serve would not refuse): %d problems", r.problems)
	}
}

func TestCreateSuperuser_UsageAndSessionKey_ExitUsage(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Main(context.Background(), []string{"create-superuser"}, quietEnv(t), &out, &errOut); code != ExitUsage {
		t.Errorf("no address: status = %d, want ExitUsage", code)
	}
	env := slices.DeleteFunc(quietEnv(t), func(e string) bool { return strings.HasPrefix(e, "BMS_SESSION_KEY=") })
	out.Reset()
	errOut.Reset()
	code := Main(context.Background(), []string{"create-superuser", "admin@example.org"}, env, &out, &errOut)
	if code != ExitUsage || !strings.Contains(errOut.String(), "BMS_SESSION_KEY") {
		t.Errorf("no session key: status = %d, stderr %q; want ExitUsage naming BMS_SESSION_KEY (F6)", code, &errOut)
	}
	if strings.Contains(out.String()+errOut.String(), "admin@example.org") {
		t.Error("the address was echoed")
	}
}

func TestRequireSessionKey_Table(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  string
		ok   bool
	}{
		{"unset", "", false},
		{"a real key", strings.Repeat("k", 40), true},
		{"the documentation placeholder", placeholderSessionKey, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Default()
			if tc.key != "" {
				cfg.Session.Key = secret.New(tc.key)
			}
			err := requireSessionKey(cfg)
			if tc.ok {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				return
			}
			if !errs.Is(err, errs.Invalid) {
				t.Fatalf("err = %v, want Invalid", err)
			}
			if tc.key != "" {
				fe := validate.Fields(err)
				if len(fe) != 1 || fe[0].Field != "session.key" {
					t.Errorf("fields = %+v, want one on session.key", fe)
				}
			}
		})
	}
}

func TestLazyTitles_NoneUntilAuthzSetsTheSource(t *testing.T) {
	a := &assembly{}
	l := lazyTitles{a: a}
	person := model.NewID[model.PersonID]()
	if got, err := l.TitlesOf(context.Background(), person); err != nil || got != nil {
		t.Fatalf("with no source: %v, %v; want nil, nil", got, err)
	}
	a.titles = titlesStub{"Gate lead"}
	if got, err := l.TitlesOf(context.Background(), person); err != nil || len(got) != 1 || got[0] != "Gate lead" {
		t.Errorf("after authz is wired: %v, %v", got, err)
	}
}

type titlesStub []string

func (s titlesStub) TitlesOf(context.Context, model.PersonID) ([]string, error) { return s, nil }

func TestCheck_AuditSpoolUnreadable_ExitUsage(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Data.Dir = blocker
	var out bytes.Buffer
	r := &checkReport{out: &out}
	r.auditSpool(cfg)
	if r.status != ExitUsage || r.problems != 1 || !strings.Contains(out.String(), "audit spool:") {
		t.Errorf("status = %d, problems = %d, output %q; want ExitUsage and one problem", r.status, r.problems, out.String())
	}
}

func TestCreateSuperuser_UnknownDisabledModule_ExitUsageBeforeTheDatabase(t *testing.T) {
	var out, errOut bytes.Buffer
	env := quietEnv(t, "BMS_MODULES_DISABLED=zqnope")
	code := Main(context.Background(), []string{"create-superuser", "admin@example.org"}, env, &out, &errOut)
	if code != ExitUsage || !strings.Contains(errOut.String(), "BMS_MODULES_DISABLED") {
		t.Errorf("status = %d, stderr %q; want ExitUsage naming BMS_MODULES_DISABLED", code, &errOut)
	}
	if strings.Contains(out.String()+errOut.String(), "admin@example.org") {
		t.Error("the address was echoed")
	}
}

// ---- authz (part 4) and people (part 5) ----

func TestCoreParts_StarterRoles_AreAuthzKeys(t *testing.T) {
	a := declared(t)
	want := map[string][]string{
		"core.modules.read": authz.SiteAdminRoles,
		"audit.search":      {authz.RoleAuditReviewer},
		"audit.export":      {authz.RoleAuditReviewer},
		"accounts.read":     {authz.RoleAccountAdmin},
		"accounts.manage":   {authz.RoleAccountAdmin},
		"settings.read":     {authz.RoleSettingsAdmin},
		"settings.manage":   {authz.RoleSettingsAdmin},
	}
	for _, d := range a.corePerms {
		w, ok := want[string(d.Name)]
		if !ok {
			continue
		}
		if !slices.Equal(d.StarterRoles, w) {
			t.Errorf("%s StarterRoles = %v, want %v", d.Name, d.StarterRoles, w)
		}
		delete(want, string(d.Name))
	}
	if len(want) > 0 {
		t.Errorf("declarations missing from corePerms: %v", want)
	}
}

func TestBuildParts_AuthzAndPeople_FillTheLazySeams(t *testing.T) {
	a := builtCore(t)
	if a.authz == nil || a.people == nil || a.deps.authz == nil || a.deps.people == nil {
		t.Fatalf("authz=%v people=%v deps.authz=%v deps.people=%v, want all set", a.authz, a.people, a.deps.authz, a.deps.people)
	}
	if a.departments == nil || a.events == nil || a.deps.departments == nil || a.deps.events == nil {
		t.Fatalf("departments=%v events=%v deps.departments=%v deps.events=%v, want all set", a.departments, a.events, a.deps.departments, a.deps.events)
	}
	if a.permissions == nil || a.titles == nil || a.lister == nil || a.names == nil {
		t.Errorf("permissions=%v titles=%v lister=%v names=%v, want all set", a.permissions, a.titles, a.lister, a.names)
	}
	deps, err := a.provision(context.Background(), module.Manifest{Name: "zqalpha"})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if deps.Authz == nil || deps.People == nil || deps.Departments == nil || deps.Events == nil {
		t.Errorf("provision: Authz=%v People=%v Departments=%v Events=%v, want all set", deps.Authz, deps.People, deps.Departments, deps.Events)
	}
}

func TestLazyAdapters_BeforeTheirPartsAreBuilt_AnswerNothing(t *testing.T) {
	a := &assembly{}
	ctx := context.Background()
	if err := (lazyPermissions{a: a}).Allow(ctx, "x.y"); !errs.Is(err, errs.Permission) {
		t.Errorf("Allow = %v, want Permission", err)
	}
	if got, err := (lazyLister{a: a}).PermissionsOf(ctx, model.PersonID{}); err != nil || got == nil || len(got) != 0 {
		t.Errorf("PermissionsOf = %v, %v, want an empty non-nil list", got, err)
	}
	if got, err := (lazyNames{a: a}).PlayaNameOf(ctx, model.PersonID{}); err != nil || got != "" {
		t.Errorf("PlayaNameOf = %q, %v, want empty", got, err)
	}
	if got, err := (lazyTitles{a: a}).TitlesOf(ctx, model.PersonID{}); err != nil || got != nil {
		t.Errorf("TitlesOf = %v, %v, want nil", got, err)
	}
	if got, err := (lazyDepartments{a: a}).Active(ctx, model.DepartmentID{}); !errs.Is(err, errs.Unsupported) || got {
		t.Errorf("Active = %v, %v, want false and Unsupported", got, err)
	}
}

func TestAuditActor_Command_IsTheOperatorOnlyWithoutASession(t *testing.T) {
	a := &assembly{}
	person := model.NewID[model.PersonID]()
	withSession := identity.WithSession(context.Background(), identity.Session{Person: person})
	for _, tc := range []struct {
		name     string
		operator bool
		ctx      context.Context
		want     audit.Actor
		ok       bool
	}{
		{"not a command, no session", false, context.Background(), audit.Actor{}, false},
		{"a command, no session", true, context.Background(), audit.Actor{Kind: audit.ActorOperator}, true},
		{"a command, a session", true, withSession, audit.Actor{Kind: audit.ActorPerson, Person: person}, true},
		{"not a command, a session", false, withSession, audit.Actor{Kind: audit.ActorPerson, Person: person}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a.operator = tc.operator
			got, ok := a.auditActor(tc.ctx)
			if ok != tc.ok || !reflect.DeepEqual(got, tc.want) {
				t.Errorf("auditActor = %+v, %v; want %+v, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestMigrateStatus_FailingFileIsExit1_MigratorConflictIsExit3(t *testing.T) {
	driver := func(kind errs.Kind) error {
		return errs.E("test.Migrate", kind, fmt.Errorf("zqprobe 0002_x: %w", &db.Error{Code: "23503", Constraint: "x_fkey"}))
	}
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{"a changed applied file", errs.E("test.Migrate", errs.Conflict, errors.New("changed")), ExitSchema},
		{"another run holds the lock", errs.E("test.Migrate", errs.Conflict, errors.New("locked")), ExitSchema},
		{"a statement fails Invalid (a foreign key preflight)", driver(errs.Invalid), ExitFailure},
		{"a statement fails Conflict (a unique violation)", driver(errs.Conflict), ExitFailure},
		{"the database is down", errs.E("test.Migrate", errs.Unavailable, errors.New("down")), ExitFailure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := migrateStatus(tc.err); got != tc.want {
				t.Errorf("migrateStatus = %d, want %d", got, tc.want)
			}
		})
	}
}

// demoStatus: 2 for demo off and the demo's own conflicts, the migrate command's mapping for
// the reset's migrate step (3 when the schema is behind), 1 for anything else, including a
// provider's Conflict with or without a field.
func TestDemoStatus_Table(t *testing.T) {
	conflict := func(field, code string) error {
		return errs.E("test", errs.Conflict, &validate.FieldError{Field: field, Code: code, Reason: "x"})
	}
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"demo off", errs.E("test", errs.Unsupported, errors.New("off")), ExitUsage},
		{"seeded conflict", conflict(demo.ConflictField, demo.CodeSeeded), ExitUsage},
		{"not_empty conflict", conflict(demo.ConflictField, demo.CodeNotEmpty), ExitUsage},
		{"not_demo conflict", conflict(demo.ConflictField, demo.CodeNotDemo), ExitUsage},
		{"the reset's migrate step found the schema behind", fmt.Errorf("reset: %w: %w", demo.ErrMigrate, errs.E("test", errs.Conflict, errors.New("schema behind"))), ExitSchema},
		{"the reset's migrate step failed otherwise", fmt.Errorf("reset: %w: %w", demo.ErrMigrate, errs.E("test", errs.Unavailable, errors.New("down"))), ExitFailure},
		{"a provider conflict without a field", fmt.Errorf("module:volunteers: %w", errs.E("test", errs.Conflict, errors.New("clash"))), ExitFailure},
		{"provider conflict with a field", fmt.Errorf("module:volunteers: %w", conflict("person", "unnamed")), ExitFailure},
		{"a wrapped demo conflict", fmt.Errorf("seed: %w", conflict(demo.ConflictField, demo.CodeSeeded)), ExitUsage},
		{"permission", errs.E("test", errs.Permission, errors.New("no")), ExitFailure},
		{"invalid", errs.E("test", errs.Invalid, errors.New("bad")), ExitFailure},
		{"unavailable", errs.E("test", errs.Unavailable, errors.New("down")), ExitFailure},
		{"internal", errors.New("unclassified"), ExitFailure},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := demoStatus(tc.err); got != tc.want {
				t.Errorf("demoStatus = %d, want %d", got, tc.want)
			}
		})
	}
}
