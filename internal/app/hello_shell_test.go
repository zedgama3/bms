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
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/authz/authztest"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/notify/notifytest"
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

// hsServer builds the real shell over the production hello module with no Deps.Authz: the
// module fails closed.
func hsServer(t *testing.T, auth api.Authenticator) *api.Server {
	t.Helper()
	return hsServerWith(t, auth, nil)
}

// hsServerWith is hsServer with Deps.Authz set to az (M2 wiring).
func hsServerWith(t *testing.T, auth api.Authenticator, az authz.Authorizer) *api.Server {
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
		return module.Deps{Logger: c.Logger(), Tracer: c.Tracer("module." + m.Name), Clock: clk, DB: dbtest.NewFake(), Signals: sig, Authz: az, Notify: notifytest.NewFake(m.Name)}, nil
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

// hsAuthz is an authztest.Fake that reads the actor the way app's closure does, from the
// identity session in ctx.
func hsAuthz() *authztest.Fake {
	az := authztest.NewFake()
	az.SetActor(func(ctx context.Context) (authz.Actor, bool) {
		p, ok := identity.PersonFrom(ctx)
		return authz.Actor{Person: p}, ok
	})
	return az
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

// The production wiring reads the actor with identity.PersonFrom and asks Deps.Authz: a
// signed-in request reaches the handler, the actor is present and, while the person holds
// nothing (or Provision gave the module no authorizer), the permission is refused, so 403,
// never 2xx. Without a session in the context (identity.WithSession is what Authenticate
// does) there is no actor: 401.
func TestHelloShell_SignedIn_WithoutTheGrant_Returns403(t *testing.T) {
	person := model.NewID[model.PersonID]()
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	auth := hsAuth(func(ctx context.Context, _ *http.Request) (context.Context, model.PersonID, error) {
		return identity.WithSession(ctx, identity.Session{
			ID:        model.NewID[identity.SessionID](),
			Account:   model.NewID[model.AccountID](),
			Person:    person,
			IssuedAt:  now,
			ExpiresAt: now.Add(time.Hour),
		}), person, nil
	})
	for _, tc := range []struct {
		name string
		srv  *api.Server
	}{
		{"no authorizer given", hsServer(t, auth)},
		{"an authorizer that holds nothing", hsServerWith(t, auth, hsAuthz())},
	} {
		for _, rt := range []struct{ method, path string }{
			{"GET", "/api/hello/greeting"},
			{"POST", "/api/hello/greetings"},
		} {
			t.Run(tc.name+" "+rt.method+" "+rt.path, func(t *testing.T) {
				rec := hsDo(t, tc.srv, rt.method, rt.path)
				if rec.Code != 403 {
					t.Fatalf("status = %d, want 403 (actor present, permission denied)", rec.Code)
				}
				if p := hsProblem(t, rec); p.Code != "permission" || p.Status != 403 {
					t.Errorf("problem = %+v", p)
				}
			})
		}
	}
}

// A signed-in person who holds hello.greetings.read through Deps.Authz (a participant, once
// authz seeds the starter role) passes the permission check. The database here is
// dbtest.Fake, whose QueryRow finds no row, so the count fails 404 after the check; the 200
// is asserted against a real database in the integration test.
func TestHelloShell_SignedIn_WithTheGrant_PassesThePermissionCheck(t *testing.T) {
	person := model.NewID[model.PersonID]()
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	az := hsAuthz()
	az.Grant(person, hello.PermissionGreetingsRead)
	srv := hsServerWith(t, hsAuth(func(ctx context.Context, _ *http.Request) (context.Context, model.PersonID, error) {
		return identity.WithSession(ctx, identity.Session{
			ID:        model.NewID[identity.SessionID](),
			Account:   model.NewID[model.AccountID](),
			Person:    person,
			IssuedAt:  now,
			ExpiresAt: now.Add(time.Hour),
		}), person, nil
	}), az)
	rec := hsDo(t, srv, "GET", "/api/hello/greeting")
	if rec.Code == 401 || rec.Code == 403 {
		t.Fatalf("status = %d (%s), want the request past authentication and the permission check", rec.Code, rec.Body.String())
	}
}

// A person the shell authenticated but whose ctx carries no identity session (a seam that
// returns only the person) is no actor for the module: it fails closed with 401.
func TestHelloShell_PersonWithoutSessionInContext_FailsClosed(t *testing.T) {
	person := model.NewID[model.PersonID]()
	srv := hsServer(t, hsAuth(func(ctx context.Context, _ *http.Request) (context.Context, model.PersonID, error) {
		return ctx, person, nil
	}))
	rec := hsDo(t, srv, "GET", "/api/hello/greeting")
	if rec.Code != 401 {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
