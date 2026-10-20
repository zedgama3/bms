//go:build integration

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/zedgama3/bms/internal/api"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
)

const itMarker = "ZQ9MARKER7"

// lockedBuf is a bytes.Buffer safe for a writer goroutine and a reader.
type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	n, err := l.b.Write(p)
	if err != nil {
		return n, fmt.Errorf("lockedBuf: %w", err)
	}
	return n, nil
}

func (l *lockedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// itDB returns the URL of a throw-away database.
func itDB(t *testing.T) *url.URL {
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
	return u
}

func itFreeAddr(t *testing.T) string {
	t.Helper()
	l, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a port: %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("release the port: %v", err)
	}
	return addr
}

func itEnv(t *testing.T, dbURL string) (env []string, addr string) {
	t.Helper()
	addr = itFreeAddr(t)
	return []string{
		"BMS_DB_URL=" + dbURL,
		"BMS_SESSION_KEY=" + itMarker + strings.Repeat("k", 38),
		"BMS_SETTINGS_SEALING_KEY=" + itMarker + strings.Repeat("s", 38),
		"BMS_DATA_DIR=" + t.TempDir(),
		"BMS_HTTP_ADDR=" + addr,
		"BMS_LOG_FORMAT=json",
		"BMS_DB_CONNECT_TIMEOUT=5s",
	}, addr
}

// itResult is what Main printed and returned.
type itResult struct {
	code        int
	out, errOut string
}

func itMain(ctx context.Context, env []string, args ...string) itResult {
	var out, errOut lockedBuf
	code := Main(ctx, args, env, &out, &errOut)
	return itResult{code, out.String(), errOut.String()}
}

// itServing runs `serve` in process and returns once /readyz answers 200.
type itServing struct {
	cancel context.CancelFunc
	done   chan itResult
	out    *lockedBuf
}

func itServe(t *testing.T, env []string, addr string) *itServing {
	t.Helper()
	if r := itMain(t.Context(), env, "migrate"); r.code != ExitOK {
		t.Fatalf("migrate: status %d\n%s\n%s", r.code, r.out, r.errOut)
	}
	ctx, cancel := context.WithCancel(t.Context())
	s := &itServing{cancel: cancel, done: make(chan itResult, 1), out: &lockedBuf{}}
	go func() {
		var errOut lockedBuf
		code := Main(ctx, []string{"serve"}, env, s.out, &errOut)
		s.done <- itResult{code, s.out.String(), errOut.String()}
	}()
	t.Cleanup(cancel)

	wctx, wcancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer wcancel()
	for {
		if code, _ := itGet(wctx, t, addr, "/readyz"); code == http.StatusOK {
			return s
		}
		select {
		case r := <-s.done:
			t.Fatalf("serve exited with %d before it was ready\n%s\n%s", r.code, r.out, r.errOut)
		default:
		}
		if err := clock.Wait(wctx, clock.Real(), 25*time.Millisecond); err != nil {
			t.Fatalf("serve never became ready: %v", err)
		}
	}
}

func (s *itServing) stop(t *testing.T) itResult {
	t.Helper()
	s.cancel()
	select {
	case r := <-s.done:
		return r
	case <-time.After(30 * time.Second):
		t.Fatal("serve did not stop")
		return itResult{}
	}
}

func itGet(ctx context.Context, t *testing.T, addr, path string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, ""
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			t.Errorf("close body: %v", cerr)
		}
	}()
	var b bytes.Buffer
	if _, err := b.ReadFrom(resp.Body); err != nil {
		return resp.StatusCode, ""
	}
	return resp.StatusCode, b.String()
}

// itRecords parses the JSON log lines of out.
func itRecords(t *testing.T, out string) []map[string]any {
	t.Helper()
	var recs []map[string]any
	for line := range strings.SplitSeq(out, "\n") {
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("log line is not JSON: %q", line)
		}
		recs = append(recs, rec)
	}
	return recs
}

func itCount(recs []map[string]any, level, msgPart string) int {
	n := 0
	for _, r := range recs {
		m, ok := r["msg"].(string)
		if ok && r["level"] == level && strings.Contains(m, msgPart) {
			n++
		}
	}
	return n
}

// failingServe is a server whose accept loop fails at once.
type failingServe struct{ httpServer }

func (failingServe) Serve() error {
	return errs.E("test.Serve", errs.Unavailable, errors.New("accept failed"))
}

// F13
func TestServe_ServeFails_RunsShutdownExit1(t *testing.T) {
	old := newServer
	newServer = func(o api.Options) (httpServer, error) {
		srv, err := api.New(o)
		if err != nil {
			return nil, err //nolint:wrapcheck // a test seam
		}
		return failingServe{srv}, nil
	}
	t.Cleanup(func() { newServer = old })

	env, _ := itEnv(t, itDB(t).String())
	if r := itMain(t.Context(), env, "migrate"); r.code != ExitOK {
		t.Fatalf("migrate: %d\n%s", r.code, r.errOut)
	}
	r := itMain(t.Context(), env, "serve")
	if r.code != ExitFailure {
		t.Fatalf("status = %d, want ExitFailure\n%s\n%s", r.code, r.out, r.errOut)
	}
	recs := itRecords(t, r.out)
	if itCount(recs, "ERROR", "stopped accepting") != 1 {
		t.Errorf("want one error record for the failed accept loop:\n%s", r.out)
	}
	reason := ""
	steps := map[string]bool{}
	for _, rec := range recs {
		if rec["msg"] == "stopping" {
			if v, ok := rec["reason"].(string); ok {
				reason = v
			}
		}
		if n, ok := rec["name"].(string); ok && rec["msg"] == "step stopped" {
			steps[n] = true
		}
	}
	if reason != "serve_error" {
		t.Errorf("stopping reason = %q, want serve_error", reason)
	}
	for _, want := range []string{"http", "runnables", "signals", "modules", "db", "telemetry"} {
		if !steps[want] {
			t.Errorf("shutdown step %q did not run; steps: %v", want, steps)
		}
	}
	if !strings.Contains(r.errOut, "accept failed") {
		t.Errorf("stderr lacks the failure: %q", r.errOut)
	}
}

// F17
func TestServe_NoUIBuild_WarnsAndServesAPI(t *testing.T) {
	old := webFS
	webFS = func() (fs.FS, error) { return nil, errs.E("web.FS", errs.NotFound, errors.New("no build")) }
	t.Cleanup(func() { webFS = old })

	env, addr := itEnv(t, itDB(t).String())
	s := itServe(t, env, addr)
	if code, _ := itGet(t.Context(), t, addr, "/api/modules"); code != http.StatusOK {
		t.Errorf("GET /api/modules = %d, want 200", code)
	}
	if code, _ := itGet(t.Context(), t, addr, "/"); code != http.StatusNotFound {
		t.Errorf("GET / = %d, want 404 without a UI", code)
	}
	r := s.stop(t)
	if r.code != ExitOK {
		t.Fatalf("status = %d, want ExitOK\n%s", r.code, r.errOut)
	}
	recs := itRecords(t, r.out)
	if n := itCount(recs, "WARN", "serving without a UI"); n != 1 {
		t.Errorf("%d warnings about the missing UI, want exactly 1", n)
	}
	for _, rec := range recs {
		if rec["msg"] == "listening" && rec["ui"] != "absent" {
			t.Errorf("listening record ui = %v, want absent", rec["ui"])
		}
	}
}

// F18
func TestServe_UIVersionMismatch_WarnsOnce(t *testing.T) {
	built := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<!doctype html><title>x</title>")}}
	for _, tc := range []struct {
		name     string
		uiVer    func(binary string) (string, error)
		wantWarn int
	}{
		{"different", func(string) (string, error) { return "some-other-build", nil }, 1},
		{"missing", func(string) (string, error) { return "", errs.E("web.Version", errs.NotFound, errors.New("none")) }, 1},
		{"invalid", func(string) (string, error) { return "", errs.E("web.Version", errs.Invalid, errors.New("bad")) }, 1},
		{"same", func(b string) (string, error) { return b, nil }, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			oldFS, oldVer := webFS, webVersion
			webFS = func() (fs.FS, error) { return built, nil }
			webVersion = func() (string, error) {
				v, err := Version()
				if err != nil {
					t.Fatalf("Version: %v", err)
				}
				return tc.uiVer(v)
			}
			t.Cleanup(func() { webFS, webVersion = oldFS, oldVer })

			env, addr := itEnv(t, itDB(t).String())
			s := itServe(t, env, addr)
			r := s.stop(t)
			if r.code != ExitOK {
				t.Fatalf("status = %d, want ExitOK\n%s", r.code, r.errOut)
			}
			recs := itRecords(t, r.out)
			if n := itCount(recs, "WARN", "web build"); n != tc.wantWarn {
				t.Errorf("%d web build warnings, want %d\n%s", n, tc.wantWarn, r.out)
			}
			for _, rec := range recs {
				if rec["msg"] == "listening" && rec["ui"] != "built" {
					t.Errorf("listening record ui = %v, want built", rec["ui"])
				}
			}
		})
	}
}

// F8: credentials the server refuses are the operator's input.
func TestServe_DBInvalid_Exit2(t *testing.T) {
	u := itDB(t)
	user := u.User.Username()
	u.User = url.UserPassword(user, "wrong-"+itMarker)
	env, _ := itEnv(t, u.String())
	r := itMain(t.Context(), env, "serve")
	if r.code != ExitUsage {
		t.Fatalf("status = %d, want ExitUsage\n%s\n%s", r.code, r.out, r.errOut)
	}
	if strings.Contains(r.out+r.errOut, itMarker) || strings.Contains(r.out+r.errOut, u.String()) {
		t.Errorf("output carries the password or the URL:\n%s\n%s", r.out, r.errOut)
	}
}

// F14: a signal during a migration cancels the current file; the command says to rerun.
func TestMigrate_Interrupted_Exit1(t *testing.T) {
	reg := module.NewRegistry()
	err := reg.Add(module.Manifest{
		Name:       "zqslow",
		Migrations: fstest.MapFS{"0001_slow.sql": &fstest.MapFile{Data: []byte("SELECT pg_sleep(120);")}},
	}, func(context.Context, module.Deps) (module.Module, error) { return nil, nil }) //nolint:nilnil // never built by migrate
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	env, _ := itEnv(t, itDB(t).String())
	var out, errOut lockedBuf
	p := &process{
		args: []string{"migrate"}, environ: env, lookup: lookupIn(env), stdout: &out, stderr: &errOut,
		clock: clock.Real(), registry: reg, version: "dev",
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan int, 1)
	go func() { done <- runMigrate(ctx, p) }()

	deadline, dcancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer dcancel()
	for !strings.Contains(errOut.String(), "database connected") {
		if err := clock.Wait(deadline, clock.Real(), 10*time.Millisecond); err != nil {
			t.Fatalf("migrate never connected: %v\n%s", err, &errOut)
		}
	}
	// The slow file runs for two minutes; once connected, the next statements are in flight.
	if err := clock.Wait(deadline, clock.Real(), 200*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case code := <-done:
		if code != ExitFailure {
			t.Errorf("status = %d, want ExitFailure\n%s", code, &errOut)
		}
	case <-deadline.Done():
		t.Fatal("migrate did not stop after the interrupt")
	}
	if !strings.Contains(errOut.String(), "interrupted") {
		t.Errorf("stderr does not say it was interrupted:\n%s", &errOut)
	}
}

// F11: a runnable whose start fails. The runnable already started is stopped, then the
// modules (in reverse order of starting), and the status is 1.
func TestServe_RunnableStartFails_StopsInReverseExit1(t *testing.T) {
	rec := &recorder{}
	reg := recRegistry(t, rec, "zqalpha")
	mk := func(name string, startErr error) runnable {
		return runnable{
			name:  name,
			start: func(context.Context) error { rec.add("start:" + name); return startErr },
			stop:  func(context.Context) error { rec.add("stop:" + name); return nil },
		}
	}
	parts := []part{{
		name: "jobs",
		build: func(_ context.Context, a *assembly) error {
			a.runnables = []runnable{
				mk("first", nil),
				mk("second", errs.E("test.second", errs.Unavailable, errors.New("cannot start"))),
				mk("third", nil),
			}
			return nil
		},
	}}
	env, _ := itEnv(t, itDB(t).String())
	var out, errOut lockedBuf
	p := &process{
		args: []string{"serve"}, environ: env, lookup: lookupIn(env), stdout: &out, stderr: &errOut,
		clock: clock.Real(), registry: reg, parts: parts, version: "dev",
	}
	if code := runServe(t.Context(), p); code != ExitFailure {
		t.Fatalf("status = %d, want ExitFailure\n%s\n%s", code, &out, &errOut)
	}
	want := []string{"start:zqalpha", "start:first", "start:second", "stop:first", "stop:zqalpha"}
	if got := rec.list(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("events = %v, want %v (the third never starts; stop runs in reverse)", got, want)
	}
	if !strings.Contains(errOut.String(), "cannot start") {
		t.Errorf("stderr lacks the failure: %q", errOut.String())
	}
	steps := map[string]bool{}
	for _, rec := range itRecords(t, out.String()) {
		if n, ok := rec["name"].(string); ok && rec["msg"] == "step stopped" {
			steps[n] = true
		}
	}
	for _, name := range []string{"db", "telemetry"} {
		if !steps[name] {
			t.Errorf("closer %q did not run after the failed start; steps: %v", name, steps)
		}
	}
}
