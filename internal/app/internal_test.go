package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/api"
	"github.com/zedgama3/bms/internal/config"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
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
	for _, k := range config.Keys() {
		got := valueOf(cfg, k)
		if got == "[unknown key]" {
			t.Errorf("valueOf has no case for %s", k.Env)
		}
		if strings.Contains(got, "ZQ9MARKER7") || strings.Contains(got, "kkkk") {
			t.Errorf("valueOf(%s) leaks a secret: %q", k.Env, got)
		}
		if k.Secret && got != "[redacted]" {
			t.Errorf("valueOf(%s) = %q, want [redacted]", k.Env, got)
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
var coreOrder = []string{"identity", "audit", "settings", "authz", "people", "departments-events", "jobs", "notify", "files"}

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

// Item 19: with the session key injected there is nothing to write, so migrate must not
// create the data directory (a read-only filesystem works).
func TestEnsureSecrets_SessionKeyAlreadySet_CreatesNoDataDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent", "data")
	var cfg config.Config
	cfg.Data.Dir = dir
	cfg.Secrets.File = filepath.Join(dir, "secrets.env")
	cfg.Session.Key = secret.New(strings.Repeat("k", 40))
	p := &process{stderr: &bytes.Buffer{}}
	if _, err := ensureSecrets(context.Background(), p, cfg); err != nil {
		t.Fatalf("ensureSecrets: %v", err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("stat %s = %v, want the directory not to exist", dir, err)
	}
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
