package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/zedgama3/bms/internal/app"
	"github.com/zedgama3/bms/internal/lib/errs"
)

var builtinCommands = []string{"serve", "migrate", "check", "openapi", "version", "help"}

func TestExitCodes_Pinned(t *testing.T) {
	for name, tc := range map[string][2]int{
		"ExitOK": {app.ExitOK, 0}, "ExitFailure": {app.ExitFailure, 1},
		"ExitUsage": {app.ExitUsage, 2}, "ExitSchema": {app.ExitSchema, 3},
	} {
		if tc[0] != tc[1] {
			t.Errorf("%s = %d, want %d", name, tc[0], tc[1])
		}
	}
}

// F1
func TestMain_UnknownCommand_UsageExit2(t *testing.T) {
	r := run(t.Context(), t, nil, "frobnicate")
	wantCode(t, r, app.ExitUsage)
	if !strings.Contains(r.errOut, `unknown command "frobnicate"`) || !strings.Contains(r.errOut, "usage: bms <command>") {
		t.Errorf("stderr lacks the unknown-command line or usage:\n%s", r.errOut)
	}
	if r.out != "" {
		t.Errorf("stdout = %q, want empty", r.out)
	}
}

// F1: Main never panics for odd operator input.
func TestMain_OddArguments_Exit2NoPanic(t *testing.T) {
	for _, args := range [][]string{{""}, {"-x"}, {"SERVE"}, {"--"}, {"serve\x00"}, {" "}, {"../etc"}} {
		t.Run(fmt.Sprintf("%q", args), func(t *testing.T) {
			wantCode(t, run(t.Context(), t, nil, args...), app.ExitUsage)
		})
	}
}

// F1
func TestMain_NoArgsAndHelp_UsageExit0(t *testing.T) {
	for _, args := range [][]string{nil, {"help"}, {"-h"}, {"--help"}} {
		t.Run(fmt.Sprintf("%v", args), func(t *testing.T) {
			r := run(t.Context(), t, nil, args...)
			wantCode(t, r, app.ExitOK)
			if r.errOut != "" {
				t.Errorf("stderr = %q, want empty", r.errOut)
			}
			if !strings.HasPrefix(r.out, "usage: bms <command>\n") {
				t.Errorf("stdout does not start with the usage line:\n%s", r.out)
			}
			if !strings.Contains(r.out, "BMS_") || !strings.Contains(r.out, "bms check") {
				t.Errorf("stdout lacks the configuration hint:\n%s", r.out)
			}
		})
	}
}

func TestHelp_ListsEveryCommandInOrder(t *testing.T) {
	r := run(t.Context(), t, nil, "help")
	wantCode(t, r, app.ExitOK)
	last := -1
	for _, name := range builtinCommands {
		re := regexp.MustCompile(`(?m)^\s*` + name + `\s+\S`)
		loc := re.FindStringIndex(r.out)
		if loc == nil {
			t.Fatalf("help has no line for %q:\n%s", name, r.out)
		}
		if loc[0] <= last {
			t.Errorf("command %q is out of table order:\n%s", name, r.out)
		}
		last = loc[0]
	}
}

func TestHelp_CommandDashH_PrintsThatLineAlone(t *testing.T) {
	r := run(t.Context(), t, nil, "serve", "-h")
	wantCode(t, r, app.ExitOK)
	if !strings.Contains(r.out, "serve") {
		t.Errorf("stdout lacks the serve line: %q", r.out)
	}
	for _, other := range []string{"migrate", "openapi"} {
		if strings.Contains(r.out, other) {
			t.Errorf("stdout names %q, want only serve's line: %q", other, r.out)
		}
	}
}

// F1: commands that take no arguments refuse them.
func TestMain_ExtraArgs_Exit2(t *testing.T) {
	for _, name := range []string{"serve", "migrate", "check", "openapi", "version"} {
		t.Run(name, func(t *testing.T) {
			env := baseEnv(t, closedDBURL(t))
			r := run(t.Context(), t, env, name, "surplus")
			wantCode(t, r, app.ExitUsage)
			noMarker(t, r)
		})
	}
}

// F2 and the output format.
func TestVersion_Output(t *testing.T) {
	v, err := app.Version()
	if err != nil {
		t.Fatalf("Version() failed in a test build: %v", err)
	}
	if v == "" || len(v) > 64 {
		t.Fatalf("Version() = %q, want a token of 1..64 bytes", v)
	}
	r := run(t.Context(), t, nil, "version")
	wantCode(t, r, app.ExitOK)
	want := fmt.Sprintf("bms %s (%s %s/%s)\n", v, runtime.Version(), runtime.GOOS, runtime.GOARCH)
	if r.out != want {
		t.Errorf("stdout = %q, want %q", r.out, want)
	}
	if r.errOut != "" {
		t.Errorf("stderr = %q, want empty", r.errOut)
	}
}

// Decision 5: version, help and openapi read no environment.
func TestVersionHelpOpenAPI_IgnoreEnvironment(t *testing.T) {
	bad := []string{"BMS_DB_MAX_CONNS=abc", "OTEL_SDK_DISABLED=banana", "BMS_SESSION_KEY=" + marker}
	for _, name := range []string{"version", "help", "openapi"} {
		t.Run(name, func(t *testing.T) {
			r := run(t.Context(), t, bad, name)
			wantCode(t, r, app.ExitOK)
			noMarker(t, r)
		})
	}
}

// F3
func TestMain_ConfigInvalid_PrintsEveryFieldExit2(t *testing.T) {
	env := []string{
		"BMS_DB_MAX_CONNS=abc",
		"BMS_HTTP_REQUEST_TIMEOUT=soon",
		"BMS_SESSION_KEY=" + marker, // too short; must not be echoed
		// BMS_DB_URL missing
	}
	for _, name := range []string{"serve", "migrate", "check"} {
		t.Run(name, func(t *testing.T) {
			r := run(t.Context(), t, env, name)
			wantCode(t, r, app.ExitUsage)
			for _, v := range []string{"BMS_DB_URL", "BMS_DB_MAX_CONNS", "BMS_HTTP_REQUEST_TIMEOUT", "BMS_SESSION_KEY"} {
				if !strings.Contains(r.all(), v) {
					t.Errorf("output does not name %s:\n%s", v, r.all())
				}
			}
			noMarker(t, r)
		})
	}
}

// F4
func TestMain_BadOTELVariable_Exit2(t *testing.T) {
	for _, name := range []string{"serve", "migrate", "check"} {
		t.Run(name, func(t *testing.T) {
			env := baseEnv(t, closedDBURL(t), "OTEL_SDK_DISABLED=banana")
			r := run(t.Context(), t, env, name)
			wantCode(t, r, app.ExitUsage)
			if !strings.Contains(r.all(), "OTEL_SDK_DISABLED") {
				t.Errorf("output does not name the variable:\n%s", r.all())
			}
			noMarker(t, r)
		})
	}
}

// F5
func TestEnabledModules_UnknownDisabledName_ReturnsInvalid(t *testing.T) {
	for _, name := range []string{"serve", "migrate", "check"} {
		t.Run(name, func(t *testing.T) {
			env := baseEnv(t, closedDBURL(t), "BMS_MODULES_DISABLED=nosuchmod,alsonone")
			r := run(t.Context(), t, env, name)
			wantCode(t, r, app.ExitUsage)
			for _, want := range []string{"BMS_MODULES_DISABLED", "nosuchmod", "alsonone", probeName} {
				if !strings.Contains(r.all(), want) {
					t.Errorf("output lacks %q (every bad name, and the compiled-in names):\n%s", want, r.all())
				}
			}
		})
	}
}

// F6
func TestCommand_SessionKeyMissing_ReturnsInvalidNamingSecretsFile(t *testing.T) {
	env := envWithout(baseEnv(t, closedDBURL(t)), "BMS_SESSION_KEY")
	r := run(t.Context(), t, env, "serve")
	wantCode(t, r, app.ExitUsage)
	dir := envValue(env, "BMS_DATA_DIR")
	if !strings.Contains(r.errOut, "BMS_SESSION_KEY") || !strings.Contains(r.errOut, filepath.Join(dir, "secrets.json")) {
		t.Errorf("stderr must name the variable and the secrets file:\n%s", r.errOut)
	}
	if !strings.Contains(r.errOut, "bms migrate") {
		t.Errorf("stderr must tell the operator to run bms migrate:\n%s", r.errOut)
	}
	noMarker(t, r)
}

// Item 21 (supersedes decision 6): check exits 2 like serve when no session key is set, and
// still tells the operator to run bms migrate.
func TestCheck_SessionKeyMissing_Exit2(t *testing.T) {
	env := envWithout(baseEnv(t, closedDBURL(t)), "BMS_SESSION_KEY")
	r := run(t.Context(), t, env, "check")
	wantCode(t, r, app.ExitUsage)
	if !strings.Contains(r.out, "run bms migrate") {
		t.Errorf("stdout must mark the unset generated key with 'run bms migrate':\n%s", r.out)
	}
	if !strings.Contains(r.out, "bms migrate") {
		t.Errorf("stdout must tell the operator to run bms migrate:\n%s", r.out)
	}
	noMarker(t, r)
}

// F7
func TestServe_ResolveFails_PrintsAllExit2(t *testing.T) {
	// zqneeds requires a capability only zqprobe provides.
	for _, name := range []string{"serve", "migrate", "check"} {
		t.Run(name, func(t *testing.T) {
			env := baseEnv(t, closedDBURL(t), "BMS_MODULES_DISABLED="+probeName)
			r := run(t.Context(), t, env, name)
			wantCode(t, r, app.ExitUsage)
			if !strings.Contains(r.all(), needsName) {
				t.Errorf("output must name the module with the unmet requirement:\n%s", r.all())
			}
			noMarker(t, r)
		})
	}
}

// F8: the URL is never echoed.
func TestServe_DBUnreachable_Exit1WithoutURL(t *testing.T) {
	url := closedDBURL(t)
	r := run(t.Context(), t, baseEnv(t, url), "serve")
	wantCode(t, r, app.ExitFailure)
	noMarker(t, r)
	if strings.Contains(r.all(), url) {
		t.Errorf("output carries the database URL:\n%s", r.all())
	}
}

// F14
func TestServe_InterruptedDuringStart_StopsStartedExit1(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r := run(ctx, t, baseEnv(t, closedDBURL(t)), "serve")
	wantCode(t, r, app.ExitFailure)
	noMarker(t, r)
}

// F24
func TestCheck_ConfigInvalid_Exit2(t *testing.T) {
	r := run(t.Context(), t, []string{"BMS_DB_MAX_CONNS=abc"}, "check")
	wantCode(t, r, app.ExitUsage)
	if !strings.Contains(r.all(), "configuration: ") || !strings.Contains(r.all(), "problem") {
		t.Errorf("output lacks the 'configuration: <n> problem(s)' heading:\n%s", r.all())
	}
}

// F24, F8, decision 4 (output on stdout, logs on stderr), Data (names only for PG*).
func TestCheck_DBUnreachable_ReportsAndExit1(t *testing.T) {
	url := closedDBURL(t)
	env := baseEnv(t, url, "PGPASSWORD="+marker, "OTEL_SERVICE_NAME=zqsvc")
	r := run(t.Context(), t, env, "check")
	wantCode(t, r, app.ExitFailure)
	noMarker(t, r)
	for _, want := range []string{
		"configuration", "BMS_DB_URL = [redacted]", "BMS_SESSION_KEY = [redacted]",
		"secrets file: ", "(absent)", "log: json info", "OTLP export: off", "PG* variables set: ",
		"PGPASSWORD", "modules", "database: ", "result: ",
	} {
		if !strings.Contains(r.out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, r.out)
		}
	}
	if !regexp.MustCompile(`zqprobe\s+enabled\s+schema yes`).MatchString(r.out) {
		t.Errorf("stdout lacks the zqprobe module line:\n%s", r.out)
	}
	if !regexp.MustCompile(`zqneeds\s+enabled\s+schema no`).MatchString(r.out) {
		t.Errorf("stdout lacks the zqneeds module line:\n%s", r.out)
	}
	if strings.Contains(r.all(), url) {
		t.Errorf("output carries the database URL")
	}
	assertNoLogLines(t, "stdout", r.out)
}

func TestCheck_DisabledModuleListedAsDisabled(t *testing.T) {
	env := baseEnv(t, closedDBURL(t), "BMS_MODULES_DISABLED="+needsName)
	r := run(t.Context(), t, env, "check")
	wantCode(t, r, app.ExitFailure) // the database is still unreachable
	if !regexp.MustCompile(`zqneeds\s+disabled`).MatchString(r.out) {
		t.Errorf("stdout lacks 'zqneeds disabled':\n%s", r.out)
	}
}

// Decision 4: JSON logs never interleave with a non-serve command's stdout.
func assertNoLogLines(t *testing.T, what, s string) {
	t.Helper()
	for line := range strings.SplitSeq(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), `{"`) {
			t.Errorf("%s carries a JSON log line: %s", what, line)
		}
	}
}

func (r result) outBytes() []byte { return []byte(r.out) }

// F22
func TestMigrate_SecretsDirUnwritable_Exit2(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	writeFile(t, blocker, "not a directory")
	env := envWithout(baseEnv(t, closedDBURL(t)), "BMS_SESSION_KEY", "BMS_DATA_DIR")
	env = append(env, "BMS_DATA_DIR="+dir, "BMS_SECRETS_FILE="+filepath.Join(blocker, "secrets.json"))
	r := run(t.Context(), t, env, "migrate")
	wantCode(t, r, app.ExitUsage) // not 1: the database was never touched
	noMarker(t, r)
}

// F23
func TestMigrate_DataDirUnwritable_Exit2(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	writeFile(t, blocker, "not a directory")
	env := envWithout(baseEnv(t, closedDBURL(t)), "BMS_SESSION_KEY", "BMS_DATA_DIR")
	env = append(env, "BMS_DATA_DIR="+filepath.Join(blocker, "data"),
		"BMS_SECRETS_FILE="+filepath.Join(t.TempDir(), "secrets.json")) // so Load itself succeeds
	r := run(t.Context(), t, env, "migrate")
	wantCode(t, r, app.ExitUsage)
	if !strings.Contains(r.errOut, "BMS_DATA_DIR") {
		t.Errorf("stderr must name BMS_DATA_DIR:\n%s", r.errOut)
	}
	noMarker(t, r)
}

// F14 (migrate half, no database needed): a canceled context stops it with status 1.
func TestMigrate_CanceledContext_Exit1(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r := run(ctx, t, baseEnv(t, closedDBURL(t)), "migrate")
	wantCode(t, r, app.ExitFailure)
	noMarker(t, r)
}

// ---- openapi ----

func openAPIDoc(t *testing.T, r result) map[string]any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(r.outBytes()))
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		t.Fatalf("stdout is not a JSON document: %v\n%s", err, r.out)
	}
	if rest := strings.TrimSpace(r.out[dec.InputOffset():]); rest != "" {
		t.Errorf("stdout holds more than the document: %q", rest)
	}
	if !strings.HasSuffix(r.out, "}\n") {
		t.Errorf("the document must end with exactly one newline")
	}
	return doc
}

func TestOpenAPI_NoEnvironmentNoIO(t *testing.T) {
	resetProbe(t)
	l, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	var accepted atomic.Int64
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			accepted.Add(1)
			if err := c.Close(); err != nil {
				return
			}
		}
	}()
	// A database URL pointing at the listener must be ignored: openapi reads no environment.
	env := []string{fmt.Sprintf("BMS_DB_URL=postgres://u:%s@%s/db?sslmode=disable", marker, l.Addr()), "OTEL_SDK_DISABLED=banana"}
	r := run(t.Context(), t, env, "openapi")
	if err := l.Close(); err != nil {
		t.Errorf("close listener: %v", err)
	}
	<-done
	wantCode(t, r, app.ExitOK)
	noMarker(t, r)
	if n := accepted.Load(); n != 0 {
		t.Errorf("openapi contacted the database address %d time(s)", n)
	}
	doc := openAPIDoc(t, r)
	info := asMap(t, doc["info"])
	if info["version"] != "dev" {
		t.Errorf("info.version = %v, want dev", info["version"])
	}
	paths := asMap(t, doc["paths"])
	if _, ok := paths["/api/zqprobe/ping"]; !ok {
		t.Errorf("document lacks /api/zqprobe/ping; paths: %v", paths)
	}
}

func TestOpenAPI_DisabledModulesStillDocumented(t *testing.T) {
	resetProbe(t)
	r := run(t.Context(), t, []string{"BMS_MODULES_DISABLED=" + probeName + ",nosuchmod"}, "openapi")
	wantCode(t, r, app.ExitOK)
	paths := asMap(t, openAPIDoc(t, r)["paths"])
	if _, ok := paths["/api/zqprobe/ping"]; !ok {
		t.Errorf("a disabled module must still be documented; paths: %v", paths)
	}
}

func TestOpenAPI_IsDeterministic(t *testing.T) {
	resetProbe(t)
	a := run(t.Context(), t, nil, "openapi")
	b := run(t.Context(), t, nil, "openapi")
	wantCode(t, a, app.ExitOK)
	if a.out != b.out {
		t.Errorf("two runs produced different documents")
	}
}

// F25
func TestOpenAPI_FactoryReads_Exit1NamesModule(t *testing.T) {
	resetProbe(t)
	ctl.Lock()
	ctl.factoryReads = true
	ctl.Unlock()
	r := run(t.Context(), t, nil, "openapi")
	wantCode(t, r, app.ExitFailure)
	if !strings.Contains(r.errOut, probeName) {
		t.Errorf("stderr must name the module:\n%s", r.errOut)
	}
	if strings.TrimSpace(r.out) != "" {
		t.Errorf("stdout must stay empty on failure, got %q", r.out)
	}
}

// F9 (unit half): the exit status follows the factory error's kind.
func TestServe_FactoryFails_Exit1(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind errs.Kind
		want int
	}{
		{"internal", errs.Internal, app.ExitFailure},
		{"unavailable", errs.Unavailable, app.ExitFailure},
		{"timeout", errs.Timeout, app.ExitFailure},
		{"invalid", errs.Invalid, app.ExitUsage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetProbe(t)
			ctl.Lock()
			ctl.factoryErr = kindErr(tc.kind)
			ctl.Unlock()
			r := run(t.Context(), t, nil, "openapi")
			wantCode(t, r, tc.want)
			if !strings.Contains(r.errOut, probeName) {
				t.Errorf("stderr must name the failing module:\n%s", r.errOut)
			}
		})
	}
}

// F10
func TestServe_APINewFails_Exit2(t *testing.T) {
	resetProbe(t)
	ctl.Lock()
	ctl.dupRoute = true
	ctl.Unlock()
	r := run(t.Context(), t, nil, "openapi")
	wantCode(t, r, app.ExitUsage)
}

// Main returns for every command's failure path (it never calls os.Exit).
func TestMain_NeverCallsOSExit(t *testing.T) {
	for _, name := range builtinCommands {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			_ = run(ctx, t, []string{"BMS_DB_MAX_CONNS=abc"}, name) // returning at all is the assertion
		})
	}
}

func asMap(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("%v is not a JSON object", v)
	}
	return m
}

// Item 19: with the session key injected migrate writes no secrets file, so it neither
// creates the data directory nor complains that it cannot. The database is unreachable
// here, so the command fails later with status 1, never with status 2.
func TestMigrate_SessionKeyInjected_MissingDataDir_NotCreatedNoUsageError(t *testing.T) {
	data := filepath.Join(t.TempDir(), "missing-parent", "data")
	env := envWithout(baseEnv(t, closedDBURL(t)), "BMS_DATA_DIR")
	env = append(env, "BMS_DATA_DIR="+data, "BMS_SECRETS_FILE="+filepath.Join(t.TempDir(), "secrets.json"))
	r := run(t.Context(), t, env, "migrate")
	if r.code == app.ExitUsage {
		t.Fatalf("migrate exited 2 although the session key is injected:\n%s", r.all())
	}
	if strings.Contains(r.errOut, "BMS_DATA_DIR") {
		t.Errorf("stderr complains about the data directory:\n%s", r.errOut)
	}
	if _, err := os.Stat(filepath.Dir(data)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("migrate created the data directory or its parent (stat err = %v)", err)
	}
	noMarker(t, r)
}

// Item 19, the other half: with no key, migrate still creates what it must write.
func TestMigrate_NoSessionKey_CreatesDataDir(t *testing.T) {
	data := filepath.Join(t.TempDir(), "made", "data")
	env := envWithout(baseEnv(t, closedDBURL(t)), "BMS_SESSION_KEY", "BMS_DATA_DIR")
	env = append(env, "BMS_DATA_DIR="+data)
	r := run(t.Context(), t, env, "migrate")
	if r.code == app.ExitUsage {
		t.Fatalf("migrate exited 2:\n%s", r.all())
	}
	if _, err := os.Stat(filepath.Join(data, "secrets.json")); err != nil {
		t.Errorf("migrate did not create the secrets file: %v", err)
	}
	noMarker(t, r)
}

// D39: an injected session key is never written, but the sealing key is still generated
// (into the secrets file, alone) when no source has it.
func TestMigrate_SessionKeyInjected_NoSealingKey_GeneratesOnlyTheSealingKey(t *testing.T) {
	data := filepath.Join(t.TempDir(), "made", "data")
	env := envWithout(baseEnv(t, closedDBURL(t)), "BMS_SETTINGS_SEALING_KEY", "BMS_DATA_DIR")
	env = append(env, "BMS_DATA_DIR="+data)
	r := run(t.Context(), t, env, "migrate")
	if r.code == app.ExitUsage {
		t.Fatalf("migrate exited 2:\n%s", r.all())
	}
	b, err := os.ReadFile(filepath.Join(data, "secrets.json")) //nolint:gosec // test reads its own temp file
	if err != nil {
		t.Fatalf("migrate did not create the secrets file: %v", err)
	}
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("the secrets file is not a JSON object of strings: %v", err)
	}
	if m["settings_sealing_key"] == "" {
		t.Errorf("the sealing key was not generated: %v", slices.Collect(maps.Keys(m)))
	}
	if _, ok := m["session_key"]; ok {
		t.Error("the injected session key was written to the secrets file")
	}
	if !strings.Contains(r.errOut, "seals settings") {
		t.Errorf("stderr must name the key it added:\n%s", r.errOut)
	}
	noMarker(t, r)
}

// ---- debug logging rule (owner, 2026-10-17) ----

const debugWarningText = "debug logging is on: logs may contain personal data"

// logRecords decodes the JSON log lines in s, ignoring anything else.
func logRecords(s string) []map[string]any {
	var out []map[string]any
	for line := range strings.SplitSeq(s, "\n") {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err == nil {
			out = append(out, rec)
		}
	}
	return out
}

func msgOf(rec map[string]any) string {
	return fmt.Sprint(rec["msg"])
}

func TestServe_DebugLevel_WarnsAfterStarting(t *testing.T) {
	env := baseEnv(t, closedDBURL(t), "BMS_LOG_LEVEL=debug")
	r := run(t.Context(), t, env, "serve")
	wantCode(t, r, app.ExitFailure)
	recs := logRecords(r.out)
	at := -1
	for i, rec := range recs {
		if msgOf(rec) == "bms starting" {
			at = i
			break
		}
	}
	if at < 0 || at+1 >= len(recs) {
		t.Fatalf("no 'bms starting' record followed by another:\n%s", r.out)
	}
	next := recs[at+1]
	if !strings.HasPrefix(msgOf(next), debugWarningText) {
		t.Fatalf("the record after 'bms starting' is %q, want the debug warning", msgOf(next))
	}
	if lvl := fmt.Sprint(next["level"]); !strings.EqualFold(lvl, "WARN") {
		t.Errorf("debug warning level = %q, want WARN", lvl)
	}
	if got := fmt.Sprint(next["log.level"]); got != "debug" {
		t.Errorf("debug warning attr log.level = %q, want debug", got)
	}
	noMarker(t, r)
}

func TestMigrate_DebugLevel_PrintsWarning(t *testing.T) {
	env := baseEnv(t, closedDBURL(t), "BMS_LOG_LEVEL=debug")
	r := run(t.Context(), t, env, "migrate")
	if !strings.Contains(r.errOut, debugWarningText) {
		t.Errorf("stderr lacks the debug warning:\n%s", r.errOut)
	}
	if strings.Contains(r.out, debugWarningText) {
		t.Errorf("the warning must go to stderr, not stdout:\n%s", r.out)
	}
	noMarker(t, r)
}

func TestCheck_DebugLevel_PrintsLogLevelLine(t *testing.T) {
	env := baseEnv(t, closedDBURL(t), "BMS_LOG_LEVEL=debug")
	r := run(t.Context(), t, env, "check")
	if !strings.Contains(r.out, "log level: debug (logs may contain personal data)") {
		t.Errorf("check's configuration section lacks the debug line:\n%s", r.out)
	}
	assertNoLogLines(t, "stdout", r.out)
	if !strings.Contains(r.errOut, debugWarningText) {
		t.Errorf("stderr lacks the debug warning:\n%s", r.errOut)
	}
	noMarker(t, r)
}

func TestDebugWarning_AbsentAtInfoAndOtherLevels(t *testing.T) {
	for _, level := range []string{"", "info", "warn", "error"} {
		for _, name := range []string{"serve", "migrate", "check"} {
			t.Run(name+"/"+level, func(t *testing.T) {
				extra := []string{}
				if level != "" {
					extra = append(extra, "BMS_LOG_LEVEL="+level)
				}
				r := run(t.Context(), t, baseEnv(t, closedDBURL(t), extra...), name)
				if strings.Contains(r.all(), debugWarningText) || strings.Contains(r.all(), "log level: debug") {
					t.Errorf("debug warning shown at level %q:\n%s", level, r.all())
				}
			})
		}
	}
}
