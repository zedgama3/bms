package hello_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/hello"
)

// bareMux mounts the module's routes at /api/hello on a ServeMux with no Failer, so
// httpx.Fail writes its fallback status table.
func bareMux(t *testing.T, m *hello.Module) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	for _, r := range m.Routes() {
		if err := r.Validate(); err != nil {
			t.Fatalf("route %s %s: %v", r.Method, r.Pattern, err)
		}
		mux.Handle(r.Method+" /api/hello"+r.Pattern, r.Handler)
	}
	return mux
}

func do(t *testing.T, h http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), method, target, nil))
	return rec
}

func problem(t *testing.T, rec *httptest.ResponseRecorder) httpx.Problem {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, httpx.ContentTypeProblem) {
		t.Fatalf("Content-Type = %q, want %s (status %d, body %q)", ct, httpx.ContentTypeProblem, rec.Code, rec.Body.String())
	}
	var p httpx.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("not a problem body: %v (%q)", err, rec.Body.String())
	}
	if p.Status != rec.Code {
		t.Fatalf("problem.status = %d, response %d", p.Status, rec.Code)
	}
	return p
}

func TestRoutes_AreTheThreeSpecified(t *testing.T) {
	f := newFx(t, cfg{})
	want := map[string]module.Access{
		"GET /public-greeting": module.AccessPublic,
		"GET /greeting":        module.AccessSignedIn,
		"POST /greetings":      module.AccessSignedIn,
	}
	got := f.mod.Routes()
	if len(got) != len(want) {
		t.Fatalf("Routes = %d, want %d", len(got), len(want))
	}
	for _, r := range got {
		acc, ok := want[r.Method+" "+r.Pattern]
		if !ok {
			t.Errorf("unexpected route %s %s", r.Method, r.Pattern)
			continue
		}
		if r.Access != acc {
			t.Errorf("%s %s: access %v, want %v", r.Method, r.Pattern, r.Access, acc)
		}
	}
}

func TestHandlers_PublicGreeting_Returns200AndBody(t *testing.T) {
	f := newFx(t, cfg{seams: hello.Seams{Actor: noActor, Allow: hello.DenyAll{}}})
	f.start(t)
	rec := do(t, bareMux(t, f.mod), "GET", "/api/hello/public-greeting")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q", ct)
	}
	var body struct {
		Greeting string `json:"greeting"`
		Banners  []struct {
			Module string `json:"module"`
			Key    string `json:"key"`
		} `json:"banners"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body: %v", err)
	}
	if body.Greeting != "howdy" || len(body.Banners) != 1 || body.Banners[0].Module != "hello" ||
		body.Banners[0].Key != "module.hello.banner.welcome" {
		t.Errorf("body = %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"banners":null`) {
		t.Errorf("banners must never be null")
	}
}

func TestHandlers_Greeting_Returns200AndBody(t *testing.T) {
	f := newFx(t, cfg{})
	f.start(t)
	seedRows(t, f.store, alice, 3)
	f.deliver(t, alice)
	rec := do(t, bareMux(t, f.mod), "GET", "/api/hello/greeting")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
	}
	obj := mustJSONObject(t, rec.Body.Bytes())
	if len(obj) != 3 {
		t.Errorf("body keys = %v, want greeting, mine, seen", obj)
	}
	var g hello.Greeting
	if err := json.Unmarshal(rec.Body.Bytes(), &g); err != nil {
		t.Fatal(err)
	}
	if g.Greeting != "howdy" || g.Mine != 3 || g.Seen != 1 {
		t.Errorf("body = %+v", g)
	}
}

func TestHandlers_Greet_Returns201AndBodyWithoutPerson(t *testing.T) {
	f := newFx(t, cfg{})
	f.start(t)
	rec := do(t, bareMux(t, f.mod), "POST", "/api/hello/greetings")
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
	}
	obj := mustJSONObject(t, rec.Body.Bytes())
	if len(obj) != 3 {
		t.Errorf("body keys = %v, want id, greeting, created_at", obj)
	}
	var out struct {
		ID        string    `json:"id"`
		Greeting  string    `json:"greeting"`
		CreatedAt time.Time `json:"created_at"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if _, err := model.ParseID[model.PersonID](out.ID); err != nil {
		t.Errorf("id %q is not a UUID: %v", out.ID, err)
	}
	if out.Greeting != "howdy" || !out.CreatedAt.Equal(epoch) {
		t.Errorf("body = %+v", out)
	}
	if strings.Contains(rec.Body.String(), alice.String()) {
		t.Errorf("the body carries the person's ID")
	}
	rows := f.store.Rows()
	if len(rows) != 1 || rows[0].Person != alice {
		t.Errorf("rows = %+v", rows)
	}
}

func TestHandlers_Greet_IgnoresRequestBody(t *testing.T) {
	f := newFx(t, cfg{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), "POST", "/api/hello/greetings", strings.NewReader(`{"greeting":"x"`+strings.Repeat("!", 5000)))
	req.Header.Set("Content-Type", "text/plain")
	bareMux(t, f.mod).ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (no body is read)", rec.Code)
	}
}

func TestHandlers_GetDoesNotChangeState(t *testing.T) {
	f := newFx(t, cfg{})
	f.start(t)
	mux := bareMux(t, f.mod)
	for _, p := range []string{"/api/hello/public-greeting", "/api/hello/greeting"} {
		if rec := do(t, mux, "GET", p); rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d", p, rec.Code)
		}
	}
	if len(f.store.Rows()) != 0 || len(f.sig.Published()) != 0 || len(f.db.Calls()) != 0 {
		t.Errorf("a GET changed state")
	}
}

func TestHandlers_WrongMethod_Returns405(t *testing.T) {
	f := newFx(t, cfg{})
	mux := bareMux(t, f.mod)
	if rec := do(t, mux, "POST", "/api/hello/greeting"); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /greeting = %d, want 405", rec.Code)
	}
	if rec := do(t, mux, "GET", "/api/hello/greetings"); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /greetings = %d, want 405", rec.Code)
	}
}

// F30
func TestHandlers_Fail_WritesStatusByKind(t *testing.T) {
	denied := hello.DenyAll{}
	cases := []struct {
		name   string
		seams  hello.Seams
		ro     bool
		method string
		path   string
		status int
		code   string
	}{
		{"greeting no actor", hello.Seams{Actor: noActor}, false, "GET", "/api/hello/greeting", 401, "unauthenticated"},
		{"greet no actor", hello.Seams{Actor: noActor}, false, "POST", "/api/hello/greetings", 401, "unauthenticated"},
		{"greeting denied", hello.Seams{Allow: denied}, false, "GET", "/api/hello/greeting", 403, "permission"},
		{"greet denied", hello.Seams{Allow: denied}, false, "POST", "/api/hello/greetings", 403, "permission"},
		{"greet read-only", hello.Seams{}, true, "POST", "/api/hello/greetings", 503, "unavailable"},
		{"public source unavailable", hello.Seams{Greeting: wordFails(errs.E("t", errs.Unavailable, errors.New("x")))}, false, "GET", "/api/hello/public-greeting", 503, "unavailable"},
		{"public source timeout", hello.Seams{Greeting: wordFails(errs.E("t", errs.Timeout, errors.New("x")))}, false, "GET", "/api/hello/public-greeting", 504, "timeout"},
		{"public source internal", hello.Seams{Greeting: wordFails(errs.E("t", errs.Internal, errors.New("x")))}, false, "GET", "/api/hello/public-greeting", 500, "internal"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFx(t, cfg{seams: tc.seams, readOnly: tc.ro})
			f.start(t)
			rec := do(t, bareMux(t, f.mod), tc.method, tc.path)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, tc.status, rec.Body.String())
			}
			if p := problem(t, rec); p.Code != tc.code {
				t.Errorf("code = %q, want %q", p.Code, tc.code)
			}
		})
	}
}

func TestOpenAPI_Fragment_DeclaresSchemasAndShapes(t *testing.T) {
	f := newFx(t, cfg{})
	var frag struct {
		Tags []struct {
			Name string `json:"name"`
		} `json:"tags"`
		Paths      map[string]map[string]json.RawMessage `json:"paths"`
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					Ref    string `json:"$ref"`
					Format string `json:"format"`
					Type   string `json:"type"`
				} `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(f.mod.OpenAPI(), &frag); err != nil {
		t.Fatalf("fragment is not JSON: %v", err)
	}
	if len(frag.Tags) != 1 || frag.Tags[0].Name != "hello" {
		t.Errorf("tags = %+v, want hello", frag.Tags)
	}
	if len(frag.Paths) != 3 {
		t.Errorf("paths = %d, want 3", len(frag.Paths))
	}
	for _, name := range []string{"HelloPublicGreeting", "HelloBanner", "HelloGreeting", "HelloGreeted"} {
		if _, ok := frag.Components.Schemas[name]; !ok {
			t.Errorf("schema %s missing", name)
		}
	}
	greeted := frag.Components.Schemas["HelloGreeted"].Properties
	if !strings.HasSuffix(greeted["id"].Ref, "/ID") {
		t.Errorf("HelloGreeted.id = %+v, want a $ref to ID", greeted["id"])
	}
	if greeted["created_at"].Format != "date-time" {
		t.Errorf("HelloGreeted.created_at = %+v, want format date-time", greeted["created_at"])
	}
	if _, leaked := greeted["person"]; leaked {
		t.Errorf("HelloGreeted exposes the person")
	}
	if _, ok := frag.Paths["/greetings"]["post"]; !ok {
		t.Errorf("POST /greetings missing from the fragment")
	}
}

func TestOpenAPI_ReturnsACopy(t *testing.T) {
	f := newFx(t, cfg{})
	a := f.mod.OpenAPI()
	if len(a) == 0 {
		t.Fatal("empty fragment")
	}
	a[0] = 'X'
	if b := f.mod.OpenAPI(); b[0] == 'X' {
		t.Errorf("OpenAPI shares its backing array with callers")
	}
}
