package app_test

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/api"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/signals/signalstest"
	"github.com/zedgama3/bms/internal/core/telemetry/intake"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/httpx"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/hello"
)

type hsNoSchema struct{}

func (hsNoSchema) CheckSchema(context.Context, string, fs.FS) error { return nil }

type hsDB struct{}

func (hsDB) Ping(context.Context) error { return nil }
func (hsDB) ReadOnly() bool             { return false }

type hsAuth func(ctx context.Context, r *http.Request) (context.Context, model.PersonID, error)

func (a hsAuth) Authenticate(ctx context.Context, r *http.Request) (context.Context, model.PersonID, error) {
	return a(ctx, r)
}

// hsServer builds the real shell over the production hello module (M0 wiring).
func hsServer(t *testing.T, auth api.Authenticator) *api.Server {
	t.Helper()
	c := telemetrytest.New(t)
	clk := clock.NewFake(time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC))
	reg := module.NewRegistry()
	if err := reg.Add(hello.Manifest, hello.New); err != nil {
		t.Fatalf("Add: %v", err)
	}
	plan, err := reg.Resolve(module.Env{Enabled: []string{"hello"}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	set, err := plan.Build(t.Context(), hsNoSchema{}, func(_ context.Context, m module.Manifest) (module.Deps, error) {
		sig, err := signalstest.NewFake(m.Name, m.Publishes, m.Subscribes)
		if err != nil {
			t.Fatalf("signalstest.NewFake: %v", err)
		}
		return module.Deps{Logger: c.Logger(), Tracer: c.Tracer("module." + m.Name), Clock: clk, DB: dbtest.NewFake(), Signals: sig}, nil
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	in, err := intake.New(intake.Options{Logger: c.Logger(), Meter: c.Meter("intake"), Clock: clk})
	if err != nil {
		t.Fatalf("intake.New: %v", err)
	}
	srv, err := api.New(api.Options{
		Addr: "127.0.0.1:18080", RequestTimeout: 30 * time.Second, MaxBodyBytes: 1 << 20,
		Logger: c.Logger(), Tracer: c.Tracer("api"), Meter: c.Meter("api"), Clock: clk, Version: "1.2.3",
		Routes: set.Routes(), Database: hsDB{}, Intake: in, Graph: plan.Graph(),
		Schema:        func(context.Context) ([]db.Status, error) { return nil, nil },
		Authenticator: auth,
	})
	if err != nil {
		t.Fatalf("api.New (mounting and the fragment check): %v", err)
	}
	return srv
}

func hsDo(t *testing.T, srv *api.Server, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, target, nil)
	req.RemoteAddr = "127.0.0.1:40000"
	req.Host = "bms.test"
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func hsProblem(t *testing.T, rec *httptest.ResponseRecorder) httpx.Problem {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, httpx.ContentTypeProblem) {
		t.Fatalf("Content-Type = %q (status %d, body %q)", ct, rec.Code, rec.Body.String())
	}
	var p httpx.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("not a problem body: %v", err)
	}
	return p
}

func TestHelloShell_OpenAPI_MatchesRoutes(t *testing.T) {
	srv := hsServer(t, nil)
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(srv.OpenAPI(), &doc); err != nil {
		t.Fatalf("OpenAPI: %v", err)
	}
	for _, w := range []struct{ path, method string }{
		{"/api/hello/public-greeting", "get"},
		{"/api/hello/greeting", "get"},
		{"/api/hello/greetings", "post"},
	} {
		if _, ok := doc.Paths[w.path][w.method]; !ok {
			t.Errorf("the assembled document lacks %s %s", w.method, w.path)
		}
	}
}

func TestHelloShell_NoSession_SignedInRoutesAnswer401(t *testing.T) {
	srv := hsServer(t, nil)
	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/hello/greeting"},
		{"POST", "/api/hello/greetings"},
	} {
		rec := hsDo(t, srv, tc.method, tc.path)
		if rec.Code != 401 {
			t.Fatalf("%s %s = %d, want 401", tc.method, tc.path, rec.Code)
		}
		if p := hsProblem(t, rec); p.Code != "unauthenticated" || p.Status != 401 {
			t.Errorf("%s %s problem = %+v", tc.method, tc.path, p)
		}
	}
}

func TestHelloShell_PublicGreeting_NeedsNoSession(t *testing.T) {
	rec := hsDo(t, hsServer(t, nil), "GET", "/api/hello/public-greeting")
	if rec.Code != 200 {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
	}
	var body struct {
		Greeting string `json:"greeting"`
		Banners  []struct {
			Module string `json:"module"`
			Key    string `json:"key"`
		} `json:"banners"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Greeting != hello.DefaultGreeting || len(body.Banners) != 1 || body.Banners[0].Module != "hello" {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestHelloShell_UnknownPath_Returns404Problem(t *testing.T) {
	rec := hsDo(t, hsServer(t, nil), "GET", "/api/hello/nope")
	if rec.Code != 404 {
		t.Fatalf("status = %d", rec.Code)
	}
	if p := hsProblem(t, rec); p.Code != httpx.CodeNotFound {
		t.Errorf("code = %q", p.Code)
	}
}

// The M0 production wiring has no actor and denies everything: a session reaches the
// handler and the module still fails closed (401, never 2xx).
func TestHelloShell_SignedInWithM0Wiring_FailsClosed(t *testing.T) {
	person := model.NewID[model.PersonID]()
	srv := hsServer(t, hsAuth(func(ctx context.Context, _ *http.Request) (context.Context, model.PersonID, error) {
		return ctx, person, nil
	}))
	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/hello/greeting"},
		{"POST", "/api/hello/greetings"},
	} {
		rec := hsDo(t, srv, tc.method, tc.path)
		if rec.Code != 401 && rec.Code != 403 {
			t.Errorf("%s %s = %d, want 401 or 403", tc.method, tc.path, rec.Code)
		}
	}
}
