//go:build integration

package app_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/zedgama3/bms/internal/app"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
)

func probeEvents() []string {
	ctl.Lock()
	defer ctl.Unlock()
	return append([]string(nil), ctl.events...)
}

const waitBudget = 30 * time.Second

// tempDB creates a database for the test and returns its URL and an admin function.
func tempDB(t *testing.T) (dbURL string, admin func(sql string)) {
	t.Helper()
	pool := dbtest.TempDB(t)
	var name string
	if err := pool.QueryRow(t.Context(), "SELECT current_database()").Scan(&name); err != nil {
		t.Fatalf("current_database: %v", err)
	}
	u, err := url.Parse(os.Getenv("BMS_TEST_DB_DSN"))
	if err != nil {
		t.Fatal("BMS_TEST_DB_DSN is not a URL")
	}
	u.Path = "/" + name
	return u.String(), func(sql string) {
		t.Helper()
		if _, err := pool.Exec(t.Context(), strings.ReplaceAll(sql, "$DB", `"`+name+`"`)); err != nil {
			t.Fatalf("admin statement failed: %v", err)
		}
	}
}

// integrationEnv is a valid environment over a fresh database with a session key.
func integrationEnv(t *testing.T, extra ...string) (env []string, admin func(string)) {
	t.Helper()
	resetProbe(t)
	dbURL, admin := tempDB(t)
	return baseEnv(t, dbURL, extra...), admin
}

func mustMigrate(t *testing.T, env []string) {
	t.Helper()
	r := run(t.Context(), t, env, "migrate")
	wantCode(t, r, app.ExitOK)
	noMarker(t, r)
}

// ---- migrate ----

func TestMigrate_CreatesSecretsFileOnceAndLoadReadsIt(t *testing.T) {
	env, _ := integrationEnv(t)
	env = envWithout(env, "BMS_SESSION_KEY")
	dir := envValue(env, "BMS_DATA_DIR")
	path := filepath.Join(dir, "secrets.json")

	r := run(t.Context(), t, env, "migrate")
	wantCode(t, r, app.ExitOK)
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("migrate did not create the secrets file: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
	}
	if !strings.Contains(r.errOut, path) || !strings.Contains(r.errOut, "Back it up") {
		t.Errorf("stderr must carry the notice naming the file:\n%s", r.errOut)
	}
	first, err := os.ReadFile(path) //nolint:gosec // test reads its own temp file
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]string
	if err := json.Unmarshal(first, &m); err != nil || m["session_key"] == "" {
		t.Fatalf("secrets file lacks session_key: %v", err)
	}
	// serve and check (which load it) accept the generated key.
	if c := run(t.Context(), t, env, "check"); c.code != app.ExitOK {
		t.Errorf("check after migrate = %d\n%s", c.code, c.all())
	}

	r2 := run(t.Context(), t, env, "migrate")
	wantCode(t, r2, app.ExitOK)
	if strings.Contains(r2.errOut, "Back it up") {
		t.Errorf("the second migrate printed the notice again:\n%s", r2.errOut)
	}
	second, err := os.ReadFile(path) //nolint:gosec // test reads its own temp file
	if err != nil || string(first) != string(second) {
		t.Errorf("secrets file changed on the second migrate (%v)", err)
	}
}

// Item 19: with BMS_SESSION_KEY injected and a data directory that cannot exist, migrate
// succeeds and creates no data directory.
func TestMigrate_SessionKeyInjected_UnwritableDataDir_SucceedsCreatesNothing(t *testing.T) {
	env, _ := integrationEnv(t)
	env = envWithout(env, "BMS_DATA_DIR")
	root := t.TempDir()
	blocker := filepath.Join(root, "blocker")
	writeFile(t, blocker, "not a directory")
	missing := filepath.Join(root, "absent-parent", "data")
	for name, dir := range map[string]string{"parent_is_a_file": filepath.Join(blocker, "data"), "parent_missing": missing} {
		t.Run(name, func(t *testing.T) {
			e := append(append([]string(nil), env...), "BMS_DATA_DIR="+dir, "BMS_SECRETS_FILE="+filepath.Join(t.TempDir(), "secrets.json"))
			r := run(t.Context(), t, e, "migrate")
			wantCode(t, r, app.ExitOK)
			noMarker(t, r)
			if _, err := os.Stat(dir); err == nil {
				t.Errorf("migrate created the data directory %s", dir)
			}
		})
	}
	if _, err := os.Stat(filepath.Dir(missing)); err == nil {
		t.Errorf("migrate created the missing parent directory")
	}
}

func TestMigrate_EmptyDatabase_AppliesCoreThenModulesInOrder(t *testing.T) {
	env, _ := integrationEnv(t)
	r := run(t.Context(), t, env, "migrate")
	wantCode(t, r, app.ExitOK)
	noMarker(t, r)
	lines := strings.Split(strings.TrimSpace(r.out), "\n")
	re := regexp.MustCompile(`^applied (\S+) (\d{4}_\S+) \(.+\)$`)
	var sets []string
	for _, l := range lines {
		if m := re.FindStringSubmatch(l); m != nil {
			sets = append(sets, m[1])
		}
	}
	if len(sets) < 2 || sets[0] != "db" {
		t.Fatalf("applied sets = %v, want db first then the modules:\n%s", sets, r.out)
	}
	if !strings.Contains(r.out, "applied zqprobe 0001_init (") {
		t.Errorf("stdout lacks the zqprobe line:\n%s", r.out)
	}
	if !regexp.MustCompile(`applied \d+ migration\(s\)`).MatchString(lines[len(lines)-1]) {
		t.Errorf("last line = %q, want 'applied <n> migration(s)'", lines[len(lines)-1])
	}
	assertNoLogLines(t, "stdout", r.out)
	if c := run(t.Context(), t, env, "check"); c.code != app.ExitOK {
		t.Errorf("check after migrate = %d (status must be current)\n%s", c.code, c.all())
	}
}

func TestMigrate_Idempotent_PrintsNothingToApply(t *testing.T) {
	env, _ := integrationEnv(t)
	mustMigrate(t, env)
	r := run(t.Context(), t, env, "migrate")
	wantCode(t, r, app.ExitOK)
	if !strings.Contains(r.out, "nothing to apply") || strings.Contains(r.out, "applied zq") {
		t.Errorf("stdout = %q, want 'nothing to apply'", r.out)
	}
}

// F19
func TestMigrate_ReadOnly_Exit1NothingApplied(t *testing.T) {
	env, admin := integrationEnv(t)
	admin("ALTER DATABASE $DB SET default_transaction_read_only = on")
	r := run(t.Context(), t, env, "migrate")
	wantCode(t, r, app.ExitFailure)
	noMarker(t, r)
	if !strings.Contains(r.errOut, "copy that writes") {
		t.Errorf("stderr must say to run migrate on the copy that writes:\n%s", r.errOut)
	}
	if strings.Contains(r.out, "applied") {
		t.Errorf("nothing may be applied:\n%s", r.out)
	}
}

// F20
func TestMigrate_Conflict_Exit3(t *testing.T) {
	env, _ := integrationEnv(t)
	mustMigrate(t, env)
	probeFS["0001_init.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE zqprobe.items (id bigint PRIMARY KEY);\n")}
	r := run(t.Context(), t, env, "migrate")
	wantCode(t, r, app.ExitSchema)
	noMarker(t, r)
}

// F21
func TestMigrate_FailingFile_Exit1NamesFile(t *testing.T) {
	env, _ := integrationEnv(t)
	mustMigrate(t, env)
	probeFS["0002_broken.sql"] = &fstest.MapFile{Data: []byte("THIS IS NOT SQL;\n")}
	r := run(t.Context(), t, env, "migrate")
	wantCode(t, r, app.ExitFailure)
	if !strings.Contains(r.errOut, "0002_broken") || !strings.Contains(r.errOut, probeName) {
		t.Errorf("stderr must name the set and the file:\n%s", r.errOut)
	}
	noMarker(t, r)
}

// ---- check ----

func TestCheck_AllGood_Exit0PrintsEverySection(t *testing.T) {
	env, _ := integrationEnv(t)
	mustMigrate(t, env)
	r := run(t.Context(), t, env, "check")
	wantCode(t, r, app.ExitOK)
	for _, want := range []string{"configuration", "modules", "database: ok, read-write", "schema", "api: ", "ui: ", "result: ok"} {
		if !strings.Contains(r.out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, r.out)
		}
	}
	if !regexp.MustCompile(`api: \d+ operations`).MatchString(r.out) {
		t.Errorf("stdout lacks the operation count:\n%s", r.out)
	}
	if !regexp.MustCompile(`zqprobe\s+built 1\s+applied 1\s+current`).MatchString(r.out) {
		t.Errorf("stdout lacks the zqprobe schema line:\n%s", r.out)
	}
	assertNoLogLines(t, "stdout", r.out)
}

func TestCheck_SchemaBehind_Exit3(t *testing.T) {
	env, _ := integrationEnv(t)
	mustMigrate(t, env)
	probeFS["0002_more.sql"] = &fstest.MapFile{Data: []byte("ALTER TABLE zqprobe.items ADD COLUMN note text;\n")}
	r := run(t.Context(), t, env, "check")
	wantCode(t, r, app.ExitSchema)
	if !regexp.MustCompile(`zqprobe\s+built 2\s+applied 1`).MatchString(r.out) {
		t.Errorf("stdout must show built 2 applied 1:\n%s", r.out)
	}
	if !strings.Contains(r.out, "database: ok") || !strings.Contains(r.out, "result: ") {
		t.Errorf("check must continue past the schema problem:\n%s", r.out)
	}
}

func TestCheck_NeverPrintsSecrets(t *testing.T) {
	env, _ := integrationEnv(t)
	mustMigrate(t, env)
	dsn := envValue(env, "BMS_DB_URL")
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	r := run(t.Context(), t, env, "check")
	all := r.all()
	if strings.Contains(all, dsn) || strings.Contains(all, sessionKey) {
		t.Errorf("check printed a secret")
	}
	if pw, ok := u.User.Password(); ok && pw != "" && strings.Contains(all, pw) {
		t.Errorf("check printed the database password")
	}
	noMarker(t, r)
}

func TestCheck_ReadOnlyDatabase_ReportsReadOnly(t *testing.T) {
	env, admin := integrationEnv(t)
	mustMigrate(t, env)
	admin("ALTER DATABASE $DB SET default_transaction_read_only = on")
	r := run(t.Context(), t, env, "check")
	wantCode(t, r, app.ExitOK)
	if !strings.Contains(r.out, "read-only") {
		t.Errorf("stdout must report the read-only mode:\n%s", r.out)
	}
}

// ---- serve ----

type server struct {
	base   string
	cancel context.CancelFunc
	done   chan result
	out    *syncBuf
}

func hostOf(env []string) string { return envValue(env, "BMS_HTTP_ADDR") }

func httpGet(ctx context.Context, t *testing.T, target string) (int, http.Header, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	cl := &http.Client{Timeout: 5 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return 0, nil, err.Error()
	}
	body, rerr := io.ReadAll(resp.Body)
	if cerr := resp.Body.Close(); cerr != nil && rerr == nil {
		rerr = cerr
	}
	if rerr != nil {
		t.Fatalf("read body: %v", rerr)
	}
	return resp.StatusCode, resp.Header, string(body)
}

// startServe runs `serve` in a goroutine and returns once /readyz answers 200.
func startServe(t *testing.T, env []string) *server {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	s := &server{base: "http://" + hostOf(env), cancel: cancel, done: make(chan result, 1), out: &syncBuf{}}
	var errOut syncBuf
	go func() {
		code := app.Main(ctx, []string{"serve"}, env, s.out, &errOut)
		s.done <- result{code: code, out: s.out.String(), errOut: errOut.String()}
	}()
	t.Cleanup(cancel)

	wctx, wcancel := context.WithTimeout(t.Context(), waitBudget)
	defer wcancel()
	for {
		select {
		case r := <-s.done:
			t.Fatalf("serve exited with %d before it was ready\nstdout:\n%s\nstderr:\n%s", r.code, r.out, r.errOut)
		default:
		}
		if code, _, _ := httpGet(wctx, t, s.base+"/readyz"); code == http.StatusOK {
			return s
		}
		if err := clock.Wait(wctx, clock.Real(), 25*time.Millisecond); err != nil {
			t.Fatalf("serve did not become ready: %v", err)
		}
	}
}

func (s *server) stop(t *testing.T) result {
	t.Helper()
	s.cancel()
	select {
	case r := <-s.done:
		return r
	case <-time.After(waitBudget):
		t.Fatal("serve did not stop")
		return result{}
	}
}

type logRecord map[string]any

func (r logRecord) msg() string {
	if s, ok := r["msg"].(string); ok {
		return s
	}
	return ""
}

func parseLogs(t *testing.T, stdout string) []logRecord {
	t.Helper()
	var out []logRecord
	for line := range strings.SplitSeq(strings.TrimSpace(stdout), "\n") {
		if line == "" {
			continue
		}
		var rec logRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("serve stdout holds a line that is not JSON: %q", line)
		}
		out = append(out, rec)
	}
	return out
}

func lastIndexOfMsg(recs []logRecord, msg string) int {
	for i := len(recs) - 1; i >= 0; i-- {
		if recs[i].msg() == msg {
			return i
		}
	}
	return -1
}

func indexOfMsg(recs []logRecord, msg string) int {
	for i, r := range recs {
		if r.msg() == msg {
			return i
		}
	}
	return -1
}

func TestServe_EndToEnd_StartsServesStops(t *testing.T) {
	env, _ := integrationEnv(t)
	mustMigrate(t, env)
	s := startServe(t, env)
	ctx := t.Context()

	if got := probeEvents(); len(got) < 2 || got[0] != "factory:zqprobe" {
		t.Errorf("events = %v, want the factory first", got)
	}
	if ev := strings.Join(probeEvents(), ","); !strings.Contains(ev, "start:zqprobe") {
		t.Errorf("Set.Start must have run before the server answers: %s", ev)
	}
	if code, _, _ := httpGet(ctx, t, s.base+"/healthz"); code != http.StatusOK {
		t.Errorf("/healthz = %d", code)
	}
	code, hdr, body := httpGet(ctx, t, s.base+"/api/zqprobe/ping")
	if code != http.StatusOK || body != "pong" || hdr.Get("X-Trace-Id") == "" {
		t.Errorf("/api/zqprobe/ping = %d %q trace %q", code, body, hdr.Get("X-Trace-Id"))
	}
	if code, _, _ := httpGet(ctx, t, s.base+"/api/zqprobe/nope"); code != http.StatusNotFound {
		t.Errorf("unknown route = %d, want 404", code)
	}
	code, _, body = httpGet(ctx, t, s.base+"/api/modules")
	if code != http.StatusUnauthorized && code != http.StatusOK {
		t.Errorf("/api/modules = %d", code)
	}
	if code == http.StatusOK && !strings.Contains(body, probeName) {
		t.Errorf("/api/modules must list zqprobe: %s", body)
	}

	r := s.stop(t)
	wantCode(t, r, app.ExitOK)
	noMarker(t, r)
	recs := parseLogs(t, r.out)
	assertLifecycleLines(t, recs, r.out)
	if ev := strings.Join(probeEvents(), ","); !strings.Contains(ev, "stop:zqprobe") {
		t.Errorf("modules were not stopped: %s", ev)
	}
	// The closers' records: http first, telemetry last, in the order of decision 10.
	var names []string
	for _, rec := range recs {
		if n, ok := rec["name"].(string); ok && rec.msg() == "step stopped" && rec["duration_ms"] != nil {
			names = append(names, n)
		}
	}
	order := []string{"http", "signals", "modules", "db", "telemetry"}
	pos := 0
	for _, n := range names {
		if pos < len(order) && strings.Contains(strings.ToLower(n), order[pos]) {
			pos++
		}
	}
	if pos != len(order) {
		t.Errorf("shutdown step records %v do not follow %v", names, order)
	}
	if strings.Contains(r.errOut, "panic") {
		t.Errorf("stderr: %s", r.errOut)
	}
}

func TestServe_CollectorDown_StartsServesStops(t *testing.T) {
	env, _ := integrationEnv(t, fmt.Sprintf("OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:%d", freePort(t)))
	mustMigrate(t, env)
	s := startServe(t, env)
	if code, _, _ := httpGet(t.Context(), t, s.base+"/api/zqprobe/ping"); code != http.StatusOK {
		t.Errorf("ping = %d with the collector down", code)
	}
	start := time.Now()
	r := s.stop(t)
	wantCode(t, r, app.ExitOK)
	if d := time.Since(start); d > 15*time.Second {
		t.Errorf("stop took %v, want within the shutdown budget", d)
	}
	noMarker(t, r)
}

func TestServe_ReadOnlyDatabase_StartsAndReadyzSaysReadOnly(t *testing.T) {
	env, admin := integrationEnv(t)
	mustMigrate(t, env)
	admin("ALTER DATABASE $DB SET default_transaction_read_only = on")
	s := startServe(t, env)
	_, _, body := httpGet(t.Context(), t, s.base+"/readyz")
	if !strings.Contains(body, "read-only") {
		t.Errorf("/readyz body = %q, want it to say read-only", body)
	}
	_, hdr, _ := httpGet(t.Context(), t, s.base+"/api/zqprobe/ping")
	if hdr.Get("X-Read-Only") != "true" {
		t.Errorf("X-Read-Only = %q, want true", hdr.Get("X-Read-Only"))
	}
	wantCode(t, s.stop(t), app.ExitOK)
}

// F9
func TestServe_SchemaOutOfDate_Exit3NamesEveryModule(t *testing.T) {
	env, _ := integrationEnv(t) // no migrate
	r := run(t.Context(), t, env, "serve")
	wantCode(t, r, app.ExitSchema)
	if !strings.Contains(r.errOut, probeName) || !strings.Contains(r.errOut, "bms migrate") {
		t.Errorf("stderr must name the module and say to run bms migrate:\n%s", r.errOut)
	}
	noMarker(t, r)
}

// F11
func TestServe_ModuleStartFails_StopsAndExit1(t *testing.T) {
	env, _ := integrationEnv(t)
	mustMigrate(t, env)
	ctl.Lock()
	ctl.startErr = kindErr(errs.Unavailable)
	ctl.Unlock()
	r := run(t.Context(), t, env, "serve")
	wantCode(t, r, app.ExitFailure)
	noMarker(t, r)
	if ev := strings.Join(probeEvents(), ","); strings.Contains(ev, "start:zqneeds") {
		t.Errorf("a module started after the failing one: %s", ev)
	}
	if code, _, _ := httpGet(t.Context(), t, "http://"+hostOf(env)+"/healthz"); code != 0 {
		t.Errorf("the listener was opened before Start failed (status %d)", code)
	}
}

// F12 and the start order: Start runs before Listen, so a taken port stops the modules.
func TestServe_AddrInUse_Exit1AfterStoppingModules(t *testing.T) {
	env, _ := integrationEnv(t)
	mustMigrate(t, env)
	l, err := new(net.ListenConfig).Listen(t.Context(), "tcp", hostOf(env))
	if err != nil {
		t.Fatalf("hold the port: %v", err)
	}
	defer func() {
		if err := l.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	}()
	r := run(t.Context(), t, env, "serve")
	wantCode(t, r, app.ExitFailure)
	noMarker(t, r)
	ev := strings.Join(probeEvents(), ",")
	iStart, iStop := strings.Index(ev, "start:zqprobe"), strings.Index(ev, "stop:zqprobe")
	if iStart < 0 || iStop < iStart {
		t.Errorf("events = %s, want start then stop of zqprobe", ev)
	}
}

// F15: a module whose Stop hangs until its deadline; the closers behind it still run.
func TestServe_ModuleStopTimesOut_Exit1RestStillRun(t *testing.T) {
	env, _ := integrationEnv(t, "BMS_HTTP_SHUTDOWN_TIMEOUT=2s")
	mustMigrate(t, env)
	ctl.Lock()
	ctl.stopHook = func(ctx context.Context) { <-ctx.Done() }
	ctl.Unlock()
	s := startServe(t, env)
	start := time.Now()
	r := s.stop(t)
	wantCode(t, r, app.ExitFailure)
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("shutdown took %v with a 2s budget", d)
	}
	recs := parseLogs(t, r.out)
	var names []string
	for _, rec := range recs {
		if n, ok := rec["name"].(string); ok && rec.msg() == "step stopped" {
			names = append(names, n)
		}
	}
	joined := strings.ToLower(strings.Join(names, ","))
	for _, want := range []string{"db", "telemetry"} {
		if !strings.Contains(joined, want) {
			t.Errorf("closer %q did not run after the hung module; steps: %v", want, names)
		}
	}
	noMarker(t, r)
}

// assertLifecycleLines checks the four lifecycle lines in order. "stopped" is the final
// line: it is unique (the closers log "step stopped") and comes after every closer record.
func assertLifecycleLines(t *testing.T, recs []logRecord, raw string) {
	t.Helper()
	prev := -1
	for _, msg := range []string{"bms starting", "listening", "stopping"} {
		i := indexOfMsg(recs, msg)
		if i < 0 {
			t.Fatalf("no %q record in:\n%s", msg, raw)
		}
		if i <= prev {
			t.Errorf("%q is out of order", msg)
		}
		prev = i
	}
	n := 0
	for _, r := range recs {
		if r.msg() == "stopped" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d records say exactly \"stopped\", want one (closers log \"step stopped\")", n)
	}
	final := lastIndexOfMsg(recs, "stopped")
	if final <= prev {
		t.Fatalf("the final \"stopped\" line is missing or out of order in:\n%s", raw)
	}
	for i, r := range recs {
		if r.msg() == "step stopped" && (i <= prev || i > final) {
			t.Errorf("a closer record sits outside stopping..stopped (index %d)", i)
		}
	}
}

// Spec: TestServe_CancelCtx_StopsCleanlyExit0. Cancelling the process context is a clean
// stop: status 0, the four lifecycle lines in order, every module stopped.
func TestServe_CancelCtx_StopsCleanlyExit0(t *testing.T) {
	env, _ := integrationEnv(t)
	mustMigrate(t, env)
	s := startServe(t, env)
	r := s.stop(t)
	wantCode(t, r, app.ExitOK)
	noMarker(t, r)
	assertLifecycleLines(t, parseLogs(t, r.out), r.out)
	ev := strings.Join(probeEvents(), ",")
	if !strings.Contains(ev, "stop:zqprobe") || !strings.Contains(ev, "stop:zqneeds") {
		t.Errorf("every module must be stopped: %s", ev)
	}
}

// Spec: TestServe_StartOrder. The factory runs before Start; a module starts after the
// module it requires; Start runs before the listener (the port-held case proves it) and
// before the server answers.
func TestServe_StartOrder(t *testing.T) {
	env, _ := integrationEnv(t)
	mustMigrate(t, env)
	s := startServe(t, env)
	ev := probeEvents()
	wantStart := s.stop(t)
	wantCode(t, wantStart, app.ExitOK)
	idx := func(want string) int {
		for i, e := range ev {
			if e == want {
				return i
			}
		}
		return -1
	}
	fp, fn := idx("factory:zqprobe"), idx("start:zqprobe")
	if fp < 0 || fn < 0 || fp > fn {
		t.Errorf("events = %v, want the factory before Start", ev)
	}
	if a, b := idx("start:zqprobe"), idx("start:zqneeds"); a < 0 || b < 0 || a > b {
		t.Errorf("events = %v, want zqprobe started before zqneeds (which requires it)", ev)
	}
	// Both modules were already started when /readyz first answered 200.
	if idx("start:zqneeds") < 0 {
		t.Errorf("Set.Start had not completed when the server became ready: %v", ev)
	}
	recs := parseLogs(t, wantStart.out)
	if indexOfMsg(recs, "bms starting") > indexOfMsg(recs, "listening") {
		t.Error("'bms starting' must precede 'listening'")
	}
}
