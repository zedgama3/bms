package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/api"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/core/telemetry/intake"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/httpx"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

const (
	marker      = "ZQ9MARKER7"
	emailMarker = "zq9@marker.example"
)

var epoch = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

// ---- fakes ----

type fakeDB struct {
	mu       sync.Mutex
	pingErr  error
	readOnly bool
	pings    int
}

func (f *fakeDB) Ping(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pings++
	return f.pingErr
}

func (f *fakeDB) ReadOnly() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.readOnly
}

func (f *fakeDB) set(pingErr error, readOnly bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pingErr, f.readOnly = pingErr, readOnly
}

func (f *fakeDB) pingCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pings
}

type authFn func(ctx context.Context, r *http.Request) (context.Context, model.PersonID, error)

func (f authFn) Authenticate(ctx context.Context, r *http.Request) (context.Context, model.PersonID, error) {
	return f(ctx, r)
}

type actorKey struct{}

// signedIn authenticates every request as person.
func signedIn(person model.PersonID) authFn {
	return func(ctx context.Context, _ *http.Request) (context.Context, model.PersonID, error) {
		return context.WithValue(ctx, actorKey{}, person), person, nil
	}
}

// noSession reports no session at all.
func noSession() authFn {
	return func(ctx context.Context, _ *http.Request) (context.Context, model.PersonID, error) {
		return ctx, model.PersonID{}, nil
	}
}

func authFails(err error) authFn {
	return func(ctx context.Context, _ *http.Request) (context.Context, model.PersonID, error) {
		return ctx, model.PersonID{}, err
	}
}

type permFn func(ctx context.Context, p authz.Permission) error

func (f permFn) Allow(ctx context.Context, p authz.Permission) error { return f(ctx, p) }

// ---- rig ----

type rig struct {
	t    *testing.T
	cap  *telemetrytest.Capture
	clk  *clock.Fake
	db   *fakeDB
	opts api.Options
	srv  *api.Server
}

func baseOptions(t *testing.T, c *telemetrytest.Capture, clk clock.Clock, d api.Database) api.Options {
	t.Helper()
	in, err := intake.New(intake.Options{Logger: c.Logger(), Meter: c.Meter("intake"), Clock: clk})
	if err != nil {
		t.Fatalf("intake.New: %v", err)
	}
	return api.Options{
		Addr:           "127.0.0.1:18080",
		RequestTimeout: 30 * time.Second,
		MaxBodyBytes:   1 << 20,
		Logger:         c.Logger(),
		Tracer:         c.Tracer("api"),
		Meter:          c.Meter("api"),
		Clock:          clk,
		Version:        "1.2.3",
		Database:       d,
		Intake:         in,
		Schema:         func(context.Context) ([]db.Status, error) { return nil, nil },
	}
}

// newRig builds valid Options (no server yet) and applies mutators.
func newRig(t *testing.T, mut ...func(*api.Options)) *rig {
	t.Helper()
	c := telemetrytest.New(t)
	clk := clock.NewFake(epoch)
	d := &fakeDB{}
	r := &rig{t: t, cap: c, clk: clk, db: d, opts: baseOptions(t, c, clk, d)}
	for _, m := range mut {
		m(&r.opts)
	}
	return r
}

// server builds the Server once; a New error stops the test.
func (r *rig) server() *api.Server {
	r.t.Helper()
	if r.srv == nil {
		s, err := api.New(r.opts)
		if err != nil {
			r.t.Fatalf("api.New: %v", err)
		}
		r.srv = s
	}
	return r.srv
}

func withRoutes(sets ...module.ModuleRoutes) func(*api.Options) {
	return func(o *api.Options) { o.Routes = append(o.Routes, sets...) }
}

func withAuth(a api.Authenticator) func(*api.Options) {
	return func(o *api.Options) { o.Authenticator = a }
}

type reqOpt func(*http.Request)

func hdr(k, v string) reqOpt { return func(r *http.Request) { r.Header.Add(k, v) } }

func remote(addr string) reqOpt { return func(r *http.Request) { r.RemoteAddr = addr } }

func host(h string) reqOpt { return func(r *http.Request) { r.Host = h } }

func body(contentType, b string) reqOpt {
	return func(r *http.Request) {
		r.Body = io.NopCloser(strings.NewReader(b))
		r.ContentLength = int64(len(b))
		if contentType != "" {
			r.Header.Set("Content-Type", contentType)
		}
	}
}

func jsonBody(b string) reqOpt { return body("application/json", b) }

func newRequest(t *testing.T, method, target string, opts ...reqOpt) *http.Request {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, target, nil)
	req.RemoteAddr = "127.0.0.1:40000"
	req.Host = "bms.test"
	for _, o := range opts {
		o(req)
	}
	return req
}

func (r *rig) do(method, target string, opts ...reqOpt) *httptest.ResponseRecorder {
	r.t.Helper()
	return serve(r.t, r.server().Handler(), method, target, opts...)
}

func serve(t *testing.T, h http.Handler, method, target string, opts ...reqOpt) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, newRequest(t, method, target, opts...))
	return rec
}

// ---- assertions and decoding ----

func problemOf(t *testing.T, rec *httptest.ResponseRecorder) httpx.Problem {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, httpx.ContentTypeProblem) {
		t.Fatalf("Content-Type = %q, want %s (status %d, body %q)", ct, httpx.ContentTypeProblem, rec.Code, rec.Body.String())
	}
	var p httpx.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("body is not a problem: %v (%q)", err, rec.Body.String())
	}
	if p.Status != rec.Code {
		t.Fatalf("problem.status = %d, response status %d", p.Status, rec.Code)
	}
	return p
}

func wantProblem(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) httpx.Problem {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d (body %q)", rec.Code, status, rec.Body.String())
	}
	p := problemOf(t, rec)
	if p.Code != code {
		t.Fatalf("code = %q, want %q", p.Code, code)
	}
	return p
}

func decodeMap(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("not a JSON object: %v (%q)", err, b)
	}
	return m
}

func hasField(err error, field string) bool {
	for _, fe := range validate.Fields(err) {
		if fe.Field == field || strings.HasPrefix(fe.Field, field+".") || strings.HasPrefix(fe.Field, field+"[") {
			return true
		}
	}
	return false
}

// ---- routes and fragments ----

func route(method, pattern string, access module.Access, h http.HandlerFunc) module.Route {
	return module.Route{Method: method, Pattern: pattern, Access: access, Handler: h}
}

func textHandler(s string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(s)); err != nil {
			return
		}
	}
}

func failWith(err error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { httpx.Fail(w, r, err) }
}

var wildcardRE = regexp.MustCompile(`\{(\w+)\.\.\.\}`)

// openAPIPath is the OpenAPI path of a route pattern relative to /api/<name>.
func openAPIPath(pattern string) string {
	return wildcardRE.ReplaceAllString(strings.TrimSuffix(pattern, "{$}"), "{$1}")
}

// fragmentFor builds a valid fragment documenting routes (and schemas, if any).
func fragmentFor(t *testing.T, routes []module.Route, schemas map[string]any) []byte {
	t.Helper()
	paths := map[string]map[string]any{}
	for _, r := range routes {
		p := openAPIPath(r.Pattern)
		if paths[p] == nil {
			paths[p] = map[string]any{}
		}
		paths[p][strings.ToLower(r.Method)] = map[string]any{
			"responses": map[string]any{"200": map[string]any{"description": "ok"}},
		}
	}
	frag := map[string]any{"paths": paths}
	if len(schemas) > 0 {
		frag["components"] = map[string]any{"schemas": schemas}
	}
	b, err := json.Marshal(frag)
	if err != nil {
		t.Fatalf("marshal fragment: %v", err)
	}
	return b
}

func setOf(t *testing.T, name string, routes ...module.Route) module.ModuleRoutes {
	t.Helper()
	return module.ModuleRoutes{Module: name, Routes: routes, OpenAPI: fragmentFor(t, routes, nil)}
}

// alphaSet is the general-purpose invented module used by most tests.
func alphaSet(t *testing.T) module.ModuleRoutes {
	t.Helper()
	pub := module.AccessPublic
	return setOf(t, "alpha",
		route("GET", "/", pub, textHandler("root")),
		route("GET", "/things/{id}", pub, func(w http.ResponseWriter, r *http.Request) {
			if err := httpx.WriteJSON(w, http.StatusOK, map[string]string{"id": r.PathValue("id")}); err != nil {
				t.Errorf("WriteJSON: %v", err)
			}
		}),
		route("GET", "/files/{path...}", pub, func(w http.ResponseWriter, r *http.Request) {
			if err := httpx.WriteJSON(w, http.StatusOK, map[string]string{"path": r.PathValue("path")}); err != nil {
				t.Errorf("WriteJSON: %v", err)
			}
		}),
		route("GET", "/private", module.AccessSignedIn, func(w http.ResponseWriter, r *http.Request) {
			_, ok := r.Context().Value(actorKey{}).(model.PersonID)
			if err := httpx.WriteJSON(w, http.StatusOK, map[string]bool{"actor": ok}); err != nil {
				t.Errorf("WriteJSON: %v", err)
			}
		}),
		route("POST", "/echo", module.AccessPublic, func(w http.ResponseWriter, r *http.Request) {
			var in struct {
				Name string `json:"name"`
			}
			if err := httpx.ReadJSON(r, &in); err != nil {
				httpx.Fail(w, r, err)
				return
			}
			if err := httpx.WriteJSON(w, http.StatusOK, in); err != nil {
				t.Errorf("WriteJSON: %v", err)
			}
		}),
	)
}

// ---- telemetry capture helpers ----

// errorRecords are the mapper's per-error log records.
func errorRecords(c *telemetrytest.Capture) []telemetry.LogRecord {
	var out []telemetry.LogRecord
	for _, r := range c.Records() {
		if r.Attrs["error.kind"] != "" || r.Attrs["error_ref"] != "" {
			out = append(out, r)
		}
	}
	return out
}

func recordsWith(c *telemetrytest.Capture, level slog.Level) []telemetry.LogRecord {
	var out []telemetry.LogRecord
	for _, r := range c.Records() {
		if r.Level == level {
			out = append(out, r)
		}
	}
	return out
}

func spanNamed(c *telemetrytest.Capture, name string) (telemetry.SpanData, bool) {
	for _, s := range c.Spans() {
		if s.Name == name {
			return s, true
		}
	}
	return telemetry.SpanData{}, false
}

func requestSpans(c *telemetrytest.Capture) []telemetry.SpanData {
	var out []telemetry.SpanData
	for _, s := range c.Spans() {
		if s.Scope == "api" {
			out = append(out, s)
		}
	}
	return out
}

func newPerson() model.PersonID { return model.NewID[model.PersonID]() }

func prefixes(ss ...string) []netip.Prefix {
	out := make([]netip.Prefix, len(ss))
	for i, s := range ss {
		out[i] = netip.MustParsePrefix(s)
	}
	return out
}

const slogWarn = slog.LevelWarn

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func netipZeroPrefix() netip.Prefix { return netip.Prefix{} }

const slogDebug = slog.LevelDebug

const (
	slogInfo  = slog.LevelInfo
	slogError = slog.LevelError
)

func jsonUnmarshal(b []byte, v any) error {
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("decode %q: %w", b, err)
	}
	return nil
}

func asError(v any) error {
	if e, ok := v.(error); ok {
		return e
	}
	return nil
}

func newRecorder() *httptest.ResponseRecorder { return httptest.NewRecorder() }

// obj and arr assert the JSON shape of a decoded value, failing the test otherwise.
func obj(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("not a JSON object: %v", v)
	}
	return m
}

func arr(t *testing.T, v any) []any {
	t.Helper()
	a, ok := v.([]any)
	if !ok {
		t.Fatalf("not a JSON array: %v", v)
	}
	return a
}
