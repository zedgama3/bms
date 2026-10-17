package api_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/api"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/telemetry/intake"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
)

func TestHealthz_OK(t *testing.T) {
	r := newRig(t)
	rec := r.do("GET", "/healthz")
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"status":"ok"}` {
		t.Fatalf("body = %q", got)
	}
	if strings.Contains(rec.Body.String(), r.opts.Version) {
		t.Error("/healthz carries the version")
	}
	// Healthy even when the database is down.
	r.db.set(errors.New("down"), false)
	if rec := r.do("GET", "/healthz"); rec.Code != 200 {
		t.Fatalf("healthz with the database down = %d", rec.Code)
	}
	if r.db.pingCount() != 0 {
		t.Error("/healthz pinged the database")
	}
}

func TestReadyz_ReadWriteAndReadOnly(t *testing.T) {
	r := newRig(t)
	rec := r.do("GET", "/readyz")
	if got := strings.TrimSpace(rec.Body.String()); rec.Code != 200 || got != `{"database":"ok","mode":"read-write"}` {
		t.Fatalf("read-write: %d %q", rec.Code, got)
	}
	r.db.set(nil, true)
	if err := r.clk.Advance(2 * time.Second); err != nil {
		t.Fatal(err)
	}
	rec = r.do("GET", "/readyz")
	if got := strings.TrimSpace(rec.Body.String()); rec.Code != 200 || got != `{"database":"ok","mode":"read-only"}` {
		t.Fatalf("read-only: %d %q", rec.Code, got)
	}
}

func TestReadyz_PingFails_503(t *testing.T) {
	r := newRig(t)
	r.db.set(errs.E("db.Ping", errs.Unavailable, errors.New("dial tcp "+marker+": refused")), false)
	rec := r.do("GET", "/readyz")
	if got := strings.TrimSpace(rec.Body.String()); rec.Code != 503 || got != `{"database":"unavailable"}` {
		t.Fatalf("%d %q", rec.Code, got)
	}
	if strings.Contains(rec.Body.String(), marker) {
		t.Error("the ping error text is in the body")
	}
	var warned bool
	for _, l := range recordsWith(r.cap, slogWarn) {
		warned = warned || l.Attrs["error.kind"] == "unavailable"
	}
	if !warned {
		t.Errorf("no warn record with the kind: %+v", r.cap.Records())
	}
	// The failure is cached for the same second.
	r.db.set(nil, false)
	if rec := r.do("GET", "/readyz"); rec.Code != 503 {
		t.Fatalf("failure not cached: %d", rec.Code)
	}
	if err := r.clk.Advance(time.Second); err != nil {
		t.Fatal(err)
	}
	if rec := r.do("GET", "/readyz"); rec.Code != 200 {
		t.Fatalf("after a second: %d", rec.Code)
	}
}

func TestReadyz_SingleFlightAndCache(t *testing.T) {
	r := newRig(t)
	h := r.server().Handler()
	var wg sync.WaitGroup
	codes := make(chan int, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- serve(t, h, "GET", "/readyz").Code
		}()
	}
	wg.Wait()
	close(codes)
	for c := range codes {
		if c != 200 {
			t.Errorf("status = %d", c)
		}
	}
	if n := r.db.pingCount(); n != 1 {
		t.Fatalf("pings = %d, want 1", n)
	}
	serve(t, h, "GET", "/readyz")
	if n := r.db.pingCount(); n != 1 {
		t.Fatalf("pings within the second = %d, want 1", n)
	}
	if err := r.clk.Advance(time.Second); err != nil {
		t.Fatal(err)
	}
	serve(t, h, "GET", "/readyz")
	if n := r.db.pingCount(); n != 2 {
		t.Fatalf("pings after a second = %d, want 2", n)
	}
}

func TestHealth_Draining_503(t *testing.T) {
	r := newRig(t)
	srv := r.server()
	if err := srv.Shutdown(t.Context()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	for _, p := range []string{"/healthz", "/readyz"} {
		rec := r.do("GET", p)
		if got := strings.TrimSpace(rec.Body.String()); rec.Code != 503 || got != `{"status":"draining"}` {
			t.Errorf("%s: %d %q", p, rec.Code, got)
		}
	}
}

func hello(enabled bool, name string) module.Node {
	return module.Node{
		Name: name, Enabled: enabled, HasSchema: true,
		Permissions: []authz.PermissionDecl{{Name: authz.Permission(name + ".things.manage"), Scope: authz.ScopeSite}},
		Settings:    []module.SettingDecl{{Key: "limit", Kind: module.SettingInt, Default: "secret-default-zq", Check: func(string) error { return nil }}},
		Publishes:   []signals.Name{signals.Name(name + ".greeted")},
		Subscribes:  []signals.Name{"people.created"},
		Requires:    []module.Capability{"cap-x"},
	}
}

func graphOpts(o *api.Options) {
	o.Graph = module.Graph{
		Modules: []module.Node{hello(true, "hello"), hello(false, "zeta")},
		Edges:   []module.Edge{{From: "hello", To: "people", Kind: module.EdgeSubscribes, Via: "people.created"}},
		Warnings: []module.Warning{
			{Module: "hello", Message: "uses a capability nobody provides"},
		},
	}
}

func TestModules_PublicListsNamesAndEnabled(t *testing.T) {
	r := newRig(t, graphOpts) // no Authenticator: public means public
	rec := r.do("GET", "/api/modules")
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	m := decodeMap(t, rec.Body.Bytes())
	if len(m) != 1 {
		t.Fatalf("top-level keys = %v, want only modules", m)
	}
	list, ok := m["modules"].([]any)
	if !ok || len(list) != 2 {
		t.Fatalf("modules = %v", m["modules"])
	}
	want := []struct {
		name    string
		enabled bool
	}{{"hello", true}, {"zeta", false}}
	for i, w := range want {
		e := obj(t, list[i])
		if len(e) != 2 || e["name"] != w.name || e["enabled"] != w.enabled {
			t.Errorf("entry %d = %v", i, e)
		}
	}
	body := rec.Body.String()
	for _, leak := range []string{"things.manage", "greeted", "people.created", "secret-default", "limit", "capability", "permissions", "has_schema"} {
		if strings.Contains(body, leak) {
			t.Errorf("public module list leaks %q", leak)
		}
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", cc)
	}
}

func TestModules_EmptyGraph(t *testing.T) {
	r := newRig(t)
	rec := r.do("GET", "/api/modules")
	if rec.Code != 200 || decodeMap(t, rec.Body.Bytes())["modules"] == nil {
		t.Fatalf("empty graph: %d %q (modules must be a list, not null)", rec.Code, rec.Body.String())
	}
}

func TestAdminModules_Table(t *testing.T) {
	person := newPerson()
	allow := permFn(func(context.Context, authz.Permission) error { return nil })
	deny := permFn(func(context.Context, authz.Permission) error {
		return errs.E("authz.Allow", errs.Permission, errors.New("no"))
	})
	schemaOK := func(context.Context) ([]db.Status, error) { return nil, nil }
	schemaFails := func(context.Context) ([]db.Status, error) {
		return nil, errs.E("db.Status", errs.Unavailable, errors.New("down"))
	}
	tests := []struct {
		name   string
		auth   api.Authenticator
		perms  api.PermissionChecker
		schema api.SchemaStatus
		want   int
		code   string
	}{
		{"no authenticator", nil, allow, schemaOK, 401, "unauthenticated"},
		{"no session", noSession(), allow, schemaOK, 401, "unauthenticated"},
		{"nil checker", signedIn(person), nil, schemaOK, 403, "permission"},
		{"checker denies", signedIn(person), deny, schemaOK, 403, "permission"},
		{"schema fails", signedIn(person), allow, schemaFails, 503, "unavailable"},
		{"allowed", signedIn(person), allow, schemaOK, 200, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, graphOpts, func(o *api.Options) {
				o.Authenticator, o.Permissions, o.Schema = tc.auth, tc.perms, tc.schema
			})
			rec := r.do("GET", "/api/admin/modules")
			if tc.code != "" {
				wantProblem(t, rec, tc.want, tc.code)
			} else if rec.Code != tc.want {
				t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestAdminModules_AsksForThePinnedPermission(t *testing.T) {
	var asked []authz.Permission
	person := newPerson()
	r := newRig(t, func(o *api.Options) {
		o.Authenticator = signedIn(person)
		o.Permissions = permFn(func(_ context.Context, p authz.Permission) error {
			asked = append(asked, p)
			return nil
		})
	})
	r.do("GET", "/api/admin/modules")
	if len(asked) != 1 || asked[0] != "core.modules.read" || api.PermissionModulesRead != "core.modules.read" {
		t.Fatalf("asked = %v, const = %q", asked, api.PermissionModulesRead)
	}
}

func TestAdminModules_WithPermission_GraphAndSchema(t *testing.T) {
	person := newPerson()
	r := newRig(t, graphOpts, func(o *api.Options) {
		o.Authenticator = signedIn(person)
		o.Permissions = permFn(func(context.Context, authz.Permission) error { return nil })
		o.Schema = func(context.Context) ([]db.Status, error) {
			return []db.Status{
				{Set: "hello", Built: 3, Applied: 3},
				{Set: "zeta", Built: 2, Applied: 1, Err: errors.New("behind " + marker)},
			}, nil
		}
	})
	rec := r.do("GET", "/api/admin/modules")
	if rec.Code != 200 {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q", cc)
	}
	m := decodeMap(t, rec.Body.Bytes())
	if m["version"] != "1.2.3" {
		t.Errorf("version = %v", m["version"])
	}
	graph, ok := m["graph"].(map[string]any)
	if !ok {
		t.Fatalf("graph = %v", m["graph"])
	}
	for _, k := range []string{"modules", "edges", "warnings"} {
		if _, ok := graph[k]; !ok {
			t.Errorf("graph lacks %q", k)
		}
	}
	mods := arr(t, graph["modules"])
	first := obj(t, mods[0])
	if first["name"] != "hello" || first["enabled"] != true || first["has_schema"] != true {
		t.Errorf("first module = %v", first)
	}
	for _, k := range []string{"permissions", "settings", "publishes", "subscribes", "provides", "requires", "uses", "defines", "contributes_to"} {
		if _, ok := first[k]; !ok {
			t.Errorf("module lacks %q: %v", k, first)
		}
	}
	if got := arr(t, first["permissions"]); len(got) != 1 || got[0] != "hello.things.manage" {
		t.Errorf("permissions = %v", got)
	}
	if got := arr(t, first["settings"]); len(got) != 1 || got[0] != "limit" {
		t.Errorf("settings = %v (keys only)", got)
	}
	edge := obj(t, arr(t, graph["edges"])[0])
	if edge["from"] != "hello" || edge["to"] != "people" || edge["kind"] != "subscribes" || edge["via"] != "people.created" {
		t.Errorf("edge = %v", edge)
	}
	warn := obj(t, arr(t, graph["warnings"])[0])
	if warn["module"] != "hello" || warn["message"] == "" {
		t.Errorf("warning = %v", warn)
	}
	schema := arr(t, m["schema"])
	if len(schema) != 2 {
		t.Fatalf("schema = %v", schema)
	}
	s0, s1 := obj(t, schema[0]), obj(t, schema[1])
	if s0["set"] != "hello" || s0["built"] != float64(3) || s0["applied"] != float64(3) || s0["current"] != true {
		t.Errorf("schema 0 = %v", s0)
	}
	if s1["set"] != "zeta" || s1["built"] != float64(2) || s1["applied"] != float64(1) || s1["current"] != false {
		t.Errorf("schema 1 = %v", s1)
	}
	body := rec.Body.String()
	if strings.Contains(body, marker) || strings.Contains(body, "secret-default") {
		t.Error("the schema error text or a setting default is in the admin body")
	}
	checkLowerKeys(t, "", m)
}

// checkLowerKeys fails for any key that is not lower snake case (no Go field names).
func checkLowerKeys(t *testing.T, path string, v any) {
	t.Helper()
	switch x := v.(type) {
	case map[string]any:
		for k, child := range x {
			if k != strings.ToLower(k) {
				t.Errorf("key %s.%s is not lower case", path, k)
			}
			checkLowerKeys(t, path+"."+k, child)
		}
	case []any:
		for _, child := range x {
			checkLowerKeys(t, path, child)
		}
	}
}

func TestNew_AdminGraphMarshalledOnce(t *testing.T) {
	person := newPerson()
	calls := 0
	r := newRig(t, graphOpts, func(o *api.Options) {
		o.Authenticator = signedIn(person)
		o.Permissions = permFn(func(context.Context, authz.Permission) error { return nil })
		o.Schema = func(context.Context) ([]db.Status, error) {
			calls++
			return []db.Status{{Set: "hello", Built: calls, Applied: calls}}, nil
		}
	})
	srv := r.server()
	// Mutating the caller's graph after New must not change the served graph.
	r.opts.Graph.Modules[0].Name = "changed"
	a := decodeMap(t, serve(t, srv.Handler(), "GET", "/api/admin/modules", authCookie()).Body.Bytes())
	b := decodeMap(t, serve(t, srv.Handler(), "GET", "/api/admin/modules", authCookie()).Body.Bytes())
	if fmt.Sprint(a["graph"]) != fmt.Sprint(b["graph"]) {
		t.Error("graph differs between requests")
	}
	if strings.Contains(fmt.Sprint(a["graph"]), "changed") {
		t.Error("graph reflects a mutation made after New")
	}
	sa := obj(t, arr(t, a["schema"])[0])["built"]
	sb := obj(t, arr(t, b["schema"])[0])["built"]
	if sa == sb {
		t.Errorf("schema must be fresh per request, got %v twice", sa)
	}
}

func authCookie() reqOpt { return hdr("Cookie", "x=1") }

// ---- client-errors ----

func validBatch(n int) string {
	var sb strings.Builder
	sb.WriteString(`{"reports":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, `{"kind":"crash","message":"boom","occurred_at":%q}`, epoch.Format(time.RFC3339))
	}
	sb.WriteString(`]}`)
	return sb.String()
}

func TestClientErrors_Table(t *testing.T) {
	t.Run("accepted", func(t *testing.T) {
		r := newRig(t)
		rec := r.do("POST", "/api/client-errors", jsonBody(validBatch(1)))
		if rec.Code != 202 || rec.Body.Len() != 0 {
			t.Fatalf("%d %q", rec.Code, rec.Body.String())
		}
	})
	t.Run("not json", func(t *testing.T) {
		r := newRig(t)
		wantProblem(t, r.do("POST", "/api/client-errors", body("text/plain", validBatch(1))), 415, httpx.CodeUnsupportedMediaType)
	})
	t.Run("over 64 KiB", func(t *testing.T) {
		r := newRig(t)
		big := `{"reports":[{"kind":"crash","message":"` + strings.Repeat("a", 70_000) + `","occurred_at":"` + epoch.Format(time.RFC3339) + `"}]}`
		wantProblem(t, r.do("POST", "/api/client-errors", jsonBody(big)), 413, httpx.CodePayloadTooLarge)
	})
	t.Run("invalid batch", func(t *testing.T) {
		r := newRig(t)
		p := wantProblem(t, r.do("POST", "/api/client-errors", jsonBody(`{"reports":[]}`)), 400, "invalid")
		if len(p.Fields) == 0 || !strings.HasPrefix(p.Fields[0].Field, "reports") {
			t.Fatalf("fields = %+v", p.Fields)
		}
	})
	t.Run("bad report", func(t *testing.T) {
		r := newRig(t)
		p := wantProblem(t, r.do("POST", "/api/client-errors", jsonBody(`{"reports":[{"kind":"nope","message":"","occurred_at":"2026-10-17T12:00:00Z"}]}`)), 400, "invalid")
		if len(p.Fields) < 2 {
			t.Fatalf("fields = %+v", p.Fields)
		}
	})
	t.Run("malformed json", func(t *testing.T) {
		r := newRig(t)
		wantProblem(t, r.do("POST", "/api/client-errors", jsonBody(`{"reports": [`)), 400, "invalid")
	})
	t.Run("rate limited", func(t *testing.T) {
		r := newRig(t)
		for i := 0; i < intake.PerAddressPerMinute/20; i++ {
			if rec := r.do("POST", "/api/client-errors", jsonBody(validBatch(20))); rec.Code != 202 {
				t.Fatalf("batch %d = %d", i, rec.Code)
			}
		}
		rec := r.do("POST", "/api/client-errors", jsonBody(validBatch(20)))
		wantProblem(t, rec, 429, httpx.CodeRateLimited)
		if rec.Header().Get("Retry-After") != "60" {
			t.Errorf("Retry-After = %q", rec.Header().Get("Retry-After"))
		}
	})
	t.Run("rate limit keyed by derived client address", func(t *testing.T) {
		r := newRig(t, func(o *api.Options) { o.TrustedProxies = prefixes("10.0.0.0/8") })
		for i := 0; i < 2; i++ {
			r.do("POST", "/api/client-errors", jsonBody(validBatch(20)), remote("10.0.0.1:1"), hdr("X-Forwarded-For", "198.51.100.1"))
		}
		// A different real client behind the same proxy is not limited.
		rec := r.do("POST", "/api/client-errors", jsonBody(validBatch(20)), remote("10.0.0.1:1"), hdr("X-Forwarded-For", "198.51.100.2"))
		if rec.Code != 202 {
			t.Fatalf("a second client behind the proxy = %d", rec.Code)
		}
	})
	t.Run("public: no session needed", func(t *testing.T) {
		r := newRig(t, withAuth(noSession()))
		if rec := r.do("POST", "/api/client-errors", jsonBody(validBatch(1))); rec.Code != 202 {
			t.Fatalf("status = %d", rec.Code)
		}
	})
	t.Run("stale session still accepted", func(t *testing.T) {
		r := newRig(t, withAuth(authFails(errs.E("x", errs.Unauthenticated, errors.New("stale")))))
		if rec := r.do("POST", "/api/client-errors", jsonBody(validBatch(1))); rec.Code != 202 {
			t.Fatalf("status = %d", rec.Code)
		}
	})
	t.Run("cross-site refused", func(t *testing.T) {
		r := newRig(t)
		wantProblem(t, r.do("POST", "/api/client-errors", jsonBody(validBatch(1)), hdr("Sec-Fetch-Site", "cross-site")), 403, httpx.CodeCrossOrigin)
	})
}

// ---- OpenAPI ----

func TestOpenAPI_Served_WithETagAndNotModified(t *testing.T) {
	r := newRig(t, withRoutes(alphaSet(t)))
	rec := r.do("GET", "/api/openapi.json")
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q", cc)
	}
	etag := rec.Header().Get("ETag")
	sum := sha256.Sum256(rec.Body.Bytes())
	if etag == "" || !strings.Contains(etag, hex.EncodeToString(sum[:])) {
		t.Errorf("ETag = %q, want the SHA-256 of the body", etag)
	}
	if !equalBytes(rec.Body.Bytes(), r.server().OpenAPI()) {
		t.Error("served bytes differ from Server.OpenAPI()")
	}
	rec = r.do("GET", "/api/openapi.json", hdr("If-None-Match", etag))
	if rec.Code != http.StatusNotModified || rec.Body.Len() != 0 {
		t.Fatalf("conditional GET = %d, %d body bytes", rec.Code, rec.Body.Len())
	}
	rec = r.do("GET", "/api/openapi.json", hdr("If-None-Match", `"other"`))
	if rec.Code != 200 {
		t.Fatalf("stale ETag = %d", rec.Code)
	}
}

func equalBytes(a, b []byte) bool { return string(a) == string(b) }

func openapiDoc(t *testing.T, r *rig) map[string]any {
	t.Helper()
	return decodeMap(t, r.server().OpenAPI())
}

func sub(t *testing.T, m map[string]any, keys ...string) map[string]any {
	t.Helper()
	cur := m
	for _, k := range keys {
		next, ok := cur[k].(map[string]any)
		if !ok {
			t.Fatalf("no object at %s (in %v)", strings.Join(keys, "."), cur)
		}
		cur = next
	}
	return cur
}

func TestOpenAPI_MergesFragmentsUnderPrefixes(t *testing.T) {
	pub := module.AccessPublic
	rts := []module.Route{
		route("GET", "/things/{id}", pub, textHandler("x")),
		route("GET", "/", pub, textHandler("x")),
		route("GET", "/files/{path...}", pub, textHandler("x")),
	}
	frag := mustJSON(map[string]any{
		"paths": map[string]any{
			"/things/{id}": map[string]any{"get": map[string]any{"responses": map[string]any{
				"200":     map[string]any{"description": "ok", "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/AlphaThing"}}}},
				"default": map[string]any{"description": "custom default"},
			}}},
			"/":             map[string]any{"get": map[string]any{"responses": map[string]any{"200": map[string]any{"description": "ok"}}}},
			"/files/{path}": map[string]any{"get": map[string]any{"responses": map[string]any{"200": map[string]any{"description": "ok"}}}},
		},
		"components": map[string]any{"schemas": map[string]any{"AlphaThing": map[string]any{"type": "object"}}},
		"tags":       []any{map[string]any{"name": "alpha"}},
	})
	beta := setOf(t, "beta", route("POST", "/x", pub, textHandler("x")))
	beta.OpenAPI = mustJSON(map[string]any{
		"paths":      map[string]any{"/x": map[string]any{"post": map[string]any{"responses": map[string]any{"200": map[string]any{"description": "ok"}}}}},
		"components": map[string]any{"schemas": map[string]any{"BetaThing": map[string]any{"type": "object"}}},
		"tags":       []any{map[string]any{"name": "beta"}},
	})
	r := newRig(t, withRoutes(module.ModuleRoutes{Module: "alpha", Routes: rts, OpenAPI: frag}, beta))
	doc := openapiDoc(t, r)
	if v := fmt.Sprint(doc["openapi"]); !strings.HasPrefix(v, "3.1") {
		t.Errorf("openapi = %v", doc["openapi"])
	}
	info := sub(t, doc, "info")
	if info["title"] != "BMS API" || info["version"] != "1.2.3" {
		t.Errorf("info = %v", info)
	}
	paths := sub(t, doc, "paths")
	for _, p := range []string{"/api/alpha/things/{id}", "/api/alpha/", "/api/alpha/files/{path}", "/api/beta/x"} {
		if _, ok := paths[p]; !ok {
			t.Errorf("paths lack %q", p)
		}
	}
	if _, ok := paths["/things/{id}"]; ok {
		t.Error("an unprefixed path leaked into the document")
	}
	schemas := sub(t, doc, "components", "schemas")
	for _, s := range []string{"AlphaThing", "BetaThing", "Problem", "Field", "ID", "Sensitivity", "TimeRange", "AdminModulesResponse"} {
		if _, ok := schemas[s]; !ok {
			t.Errorf("schemas lack %q", s)
		}
	}
	tags := fmt.Sprint(doc["tags"])
	if !strings.Contains(tags, "alpha") || !strings.Contains(tags, "beta") {
		t.Errorf("tags = %v", doc["tags"])
	}
	// default response: added where absent, kept where the fragment had one.
	responses := sub(t, paths, "/api/alpha/", "get", "responses")
	def := sub(t, responses, "default")
	if !strings.Contains(fmt.Sprint(def), "#/components/schemas/Problem") {
		t.Errorf("default response = %v, want a $ref to Problem", def)
	}
	kept := sub(t, paths, "/api/alpha/things/{id}", "get", "responses", "default")
	if kept["description"] != "custom default" {
		t.Errorf("fragment's own default response was replaced: %v", kept)
	}
}

func TestOpenAPI_EveryShellRouteDocumented(t *testing.T) {
	r := newRig(t)
	paths := sub(t, openapiDoc(t, r), "paths")
	for path, method := range map[string]string{
		"/api/modules": "get", "/api/admin/modules": "get",
		"/api/client-errors": "post", "/api/openapi.json": "get",
	} {
		p, ok := paths[path].(map[string]any)
		if !ok {
			t.Errorf("no path %s", path)
			continue
		}
		if _, ok := p[method]; !ok {
			t.Errorf("%s has no %s operation", path, method)
		}
	}
	for path := range paths {
		if strings.HasPrefix(path, "/api/core") {
			t.Errorf("nothing is mounted at /api/core, found %s", path)
		}
	}
}

func TestOpenAPI_IDSensitivityTimeRangeShapes(t *testing.T) {
	r := newRig(t)
	schemas := sub(t, openapiDoc(t, r), "components", "schemas")
	id := sub(t, schemas, "ID")
	if id["type"] != "string" || id["format"] != "uuid" {
		t.Errorf("ID = %v", id)
	}
	sens := sub(t, schemas, "Sensitivity")
	got := map[string]bool{}
	enum := arr(t, sens["enum"])
	for _, e := range enum {
		got[fmt.Sprint(e)] = true
	}
	for _, want := range []string{"public", "internal", "personal", "sensitive"} {
		if !got[want] {
			t.Errorf("Sensitivity enum %v lacks %q", enum, want)
		}
	}
	if len(enum) != 4 {
		t.Errorf("Sensitivity enum = %v, want four names", enum)
	}
	props := sub(t, schemas, "TimeRange", "properties")
	for _, k := range []string{"start", "end"} {
		p := sub(t, props, k)
		if p["type"] != "string" || p["format"] != "date-time" {
			t.Errorf("TimeRange.%s = %v", k, p)
		}
	}
	prob := sub(t, schemas, "Problem", "properties")
	for _, k := range []string{"status", "code", "ref", "fields"} {
		if _, ok := prob[k]; !ok {
			t.Errorf("Problem schema lacks %q", k)
		}
	}
	admin := sub(t, schemas, "AdminModulesResponse", "properties")
	for _, k := range []string{"version", "graph", "schema"} {
		if _, ok := admin[k]; !ok {
			t.Errorf("AdminModulesResponse lacks %q", k)
		}
	}
}
