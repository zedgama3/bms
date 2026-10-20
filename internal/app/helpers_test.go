package app_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/zedgama3/bms/internal/app"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// marker is planted in every secret the tests hand to Main; no output may contain it.
const marker = "ZQ9MARKER7"

// sessionKey is a 48-byte key that carries the marker.
var sessionKey = marker + strings.Repeat("k", 38)

// sealingKey is a 48-byte settings sealing key that carries the marker.
var sealingKey = marker + strings.Repeat("s", 38)

// The invented modules below are registered in the process-wide registry, because Main
// builds its registry from module.Default(). Names carry the prefix "zq" so the catalogue
// test can tell them from the real ones. zqneeds requires what zqprobe provides, so
// disabling zqprobe makes Resolve fail.
const (
	probeName = "zqprobe"
	needsName = "zqneeds"
	probeCap  = module.Capability("zqcap")
)

// probeControl steers the invented modules. Tests that touch it must not run in parallel.
var ctl struct {
	sync.Mutex
	factoryErr   error // returned by zqprobe's factory
	factoryReads bool  // zqprobe's factory queries the database
	startErr     error // returned by zqprobe's Start
	dupRoute     bool  // zqprobe declares the same route twice
	events       []string
	stopHook     func(ctx context.Context) // called inside Stop, outside the lock
}

// resetProbe zeroes the controls now and when the test ends.
func resetProbe(t *testing.T) {
	t.Helper()
	reset := func() {
		ctl.Lock()
		defer ctl.Unlock()
		ctl.factoryErr, ctl.factoryReads, ctl.startErr, ctl.dupRoute = nil, false, nil, false
		ctl.events, ctl.stopHook = nil, nil
		for name := range probeFS {
			if name != "0001_init.sql" {
				delete(probeFS, name)
			}
		}
		probeFS["0001_init.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE zqprobe.items (id integer PRIMARY KEY);\n")}
	}
	reset()
	t.Cleanup(reset)
}

func record(ev string) {
	ctl.Lock()
	defer ctl.Unlock()
	ctl.events = append(ctl.events, ev)
}

type probeModule struct{ name string }

func (m *probeModule) Start(context.Context) error {
	record("start:" + m.name)
	ctl.Lock()
	defer ctl.Unlock()
	if m.name == probeName {
		return ctl.startErr
	}
	return nil
}

func (m *probeModule) Stop(ctx context.Context) error {
	record("stop:" + m.name)
	ctl.Lock()
	hook := ctl.stopHook
	ctl.Unlock()
	if hook != nil && m.name == probeName {
		hook(ctx)
	}
	return nil
}

func (*probeModule) ExportPerson(context.Context, model.PersonID) (module.Export, error) {
	return module.Export{}, nil
}

func (*probeModule) DeletePerson(context.Context, model.PersonID) (module.Deletion, error) {
	return module.Deletion{}, nil
}

func (*probeModule) Capabilities() map[module.Capability]any {
	return map[module.Capability]any{probeCap: struct{}{}}
}

func (*probeModule) Routes() []module.Route {
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		if _, err := w.Write([]byte("pong")); err != nil {
			return
		}
	})
	r := module.Route{Method: "GET", Pattern: "/ping", Access: module.AccessPublic, Handler: h}
	ctl.Lock()
	defer ctl.Unlock()
	if ctl.dupRoute {
		return []module.Route{r, r}
	}
	return []module.Route{r}
}

func (*probeModule) OpenAPI() []byte {
	return []byte(`{"paths":{"/ping":{"get":{"responses":{"200":{"description":"ok"}}}}}}`)
}

func probeFactory(ctx context.Context, deps module.Deps) (module.Module, error) {
	ctl.Lock()
	reads, ferr := ctl.factoryReads, ctl.factoryErr
	ctl.Unlock()
	record("factory:" + probeName)
	if reads {
		if _, err := deps.DB.Exec(ctx, "SELECT 1"); err != nil {
			return nil, fmt.Errorf("zqprobe factory read: %w", err)
		}
	}
	if ferr != nil {
		return nil, ferr
	}
	return &probeModule{name: probeName}, nil
}

// needsModule has no routes and provides nothing: only a plain module.
type needsModule struct{}

func (*needsModule) Start(context.Context) error {
	record("start:" + needsName)
	return nil
}

func (*needsModule) Stop(context.Context) error {
	record("stop:" + needsName)
	return nil
}

func (*needsModule) ExportPerson(context.Context, model.PersonID) (module.Export, error) {
	return module.Export{}, nil
}

func (*needsModule) DeletePerson(context.Context, model.PersonID) (module.Deletion, error) {
	return module.Deletion{}, nil
}

func needsFactory(context.Context, module.Deps) (module.Module, error) {
	return &needsModule{}, nil
}

// probeFS holds zqprobe's migrations; integration tests add and change files to provoke
// the schema failures, and restore it with resetProbe.
var probeFS = fstest.MapFS{
	"0001_init.sql": &fstest.MapFile{Data: []byte("CREATE TABLE zqprobe.items (id integer PRIMARY KEY);\n")},
}

func init() {
	module.Register(module.Manifest{
		Name:       probeName,
		Migrations: probeFS,
		Provides:   []module.Capability{probeCap},
	}, probeFactory)
	module.Register(module.Manifest{
		Name:     needsName,
		Requires: []module.Capability{probeCap},
	}, needsFactory)
}

// ---- running Main ----

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p) //nolint:wrapcheck // bytes.Buffer.Write never fails
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

type result struct {
	code        int
	out, errOut string
}

// all is stdout and stderr together, for "nothing leaks" checks.
func (r result) all() string { return r.out + "\n" + r.errOut }

func run(ctx context.Context, t *testing.T, env []string, args ...string) result {
	t.Helper()
	var out, errOut syncBuf
	code := app.Main(ctx, args, env, &out, &errOut)
	return result{code: code, out: out.String(), errOut: errOut.String()}
}

func noMarker(t *testing.T, r result) {
	t.Helper()
	if strings.Contains(r.all(), marker) {
		t.Errorf("output carries the secret marker:\nstdout:\n%s\nstderr:\n%s", r.out, r.errOut)
	}
}

func wantCode(t *testing.T, r result, want int) {
	t.Helper()
	if r.code != want {
		t.Fatalf("exit status = %d, want %d\nstdout:\n%s\nstderr:\n%s", r.code, want, r.out, r.errOut)
	}
}

// ---- environments ----

// freePort reserves a loopback port by binding and releasing it.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a port: %v", err)
	}
	tcp, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener address %v is not TCP", l.Addr())
	}
	port := tcp.Port
	if err := l.Close(); err != nil {
		t.Fatalf("release the port: %v", err)
	}
	return port
}

// closedDBURL is a database URL nothing listens on, with the marker as the password.
func closedDBURL(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("postgres://zquser:%s@127.0.0.1:%d/zqdb?sslmode=disable", marker, freePort(t))
}

// baseEnv is a valid environment for dbURL. Extra entries override earlier ones.
func baseEnv(t *testing.T, dbURL string, extra ...string) []string {
	t.Helper()
	env := []string{
		"BMS_DB_URL=" + dbURL,
		"BMS_SESSION_KEY=" + sessionKey,
		"BMS_SETTINGS_SEALING_KEY=" + sealingKey,
		"BMS_DATA_DIR=" + t.TempDir(),
		fmt.Sprintf("BMS_HTTP_ADDR=127.0.0.1:%d", freePort(t)),
		"BMS_LOG_FORMAT=json",
		"BMS_DB_CONNECT_TIMEOUT=2s",
	}
	return append(env, extra...)
}

// envWithout drops every entry that sets one of the named variables.
func envWithout(env []string, names ...string) []string {
	var out []string
outer:
	for _, e := range env {
		for _, n := range names {
			if strings.HasPrefix(e, n+"=") {
				continue outer
			}
		}
		out = append(out, e)
	}
	return out
}

var errBoom = errors.New("boom")

func kindErr(k errs.Kind) error { return errs.E("test.factory", k, errBoom) }

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// envValue returns the value of name in env, or "".
func envValue(env []string, name string) string {
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, name+"="); ok {
			return v
		}
	}
	return ""
}
