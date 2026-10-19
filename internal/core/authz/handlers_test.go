package authz_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// serve mounts the service's routes under /api/access as app does.
func serve(t testing.TB, svc *authz.Service) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	for _, rt := range svc.Routes() {
		p := rt.Pattern
		if !strings.HasPrefix(p, "/") {
			p = "/" + p
		}
		mux.Handle(rt.Method+" /api/access"+p, rt.Handler)
	}
	return mux
}

func doReq(mux http.Handler, ctx context.Context, method, path, body string) *httptest.ResponseRecorder { //nolint:revive // ctx second: the mux is the subject
	var rd *strings.Reader
	if body != "" {
		rd = strings.NewReader(body)
	} else {
		rd = strings.NewReader("")
	}
	r := httptest.NewRequestWithContext(ctx, method, path, rd)
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func TestRoutes_MatchSpecSet(t *testing.T) {
	r := newUnitRig(t, db.None())
	var got []string
	for _, rt := range r.svc.Routes() {
		if rt.Handler == nil {
			t.Errorf("%s %s has no handler", rt.Method, rt.Pattern)
		}
		got = append(got, rt.Method+" /"+strings.TrimPrefix(rt.Pattern, "/"))
	}
	want := []string{
		"GET /permissions",
		"GET /roles", "POST /roles",
		"GET /roles/{role}", "PUT /roles/{role}", "DELETE /roles/{role}",
		"GET /grants", "POST /grants", "DELETE /grants/{grant}",
		"GET /rules", "POST /rules", "DELETE /rules/{rule}",
	}
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("routes = %v, want %v", got, want)
	}
}

// F62
func TestHandlers_BadID_Returns400(t *testing.T) {
	r := newUnitRig(t, db.None())
	mux := serve(t, r.svc)
	ctx := asSuper(t.Context(), newPerson())
	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/access/roles/not-a-uuid"},
		{"PUT", "/api/access/roles/not-a-uuid"},
		{"DELETE", "/api/access/roles/not-a-uuid"},
		{"DELETE", "/api/access/grants/not-a-uuid"},
		{"DELETE", "/api/access/rules/not-a-uuid"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			body := ""
			if tc.method == "PUT" {
				body = `{"name":"x"}`
			}
			w := doReq(mux, ctx, tc.method, tc.path, body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestHandlers_NoSession_Returns401(t *testing.T) {
	r := newUnitRig(t, db.None())
	mux := serve(t, r.svc)
	id := newPerson().String()
	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/access/permissions"},
		{"GET", "/api/access/roles"},
		{"GET", "/api/access/roles/" + id},
		{"GET", "/api/access/grants"},
		{"GET", "/api/access/rules"},
		{"DELETE", "/api/access/grants/" + id},
		{"DELETE", "/api/access/rules/" + id},
		{"DELETE", "/api/access/roles/" + id},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			w := doReq(mux, t.Context(), tc.method, tc.path, "")
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401; body %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestHandlers_MalformedBody_Returns400(t *testing.T) {
	r := newUnitRig(t, db.None())
	mux := serve(t, r.svc)
	ctx := asSuper(t.Context(), newPerson())
	for _, p := range []string{"/api/access/roles", "/api/access/grants", "/api/access/rules"} {
		w := doReq(mux, ctx, "POST", p, "{not json")
		if w.Code != http.StatusBadRequest {
			t.Errorf("POST %s with bad JSON: status %d, want 400", p, w.Code)
		}
	}
}

func TestOpenAPI_IsJSONWithAccessSchemas(t *testing.T) {
	r := newUnitRig(t, db.None())
	raw := r.svc.OpenAPI()
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("OpenAPI is not JSON: %v", err)
	}
	for _, name := range []string{"AccessPermission", "AccessRole", "AccessRoleInput", "AccessGrant", "AccessGrantInput", "AccessGrantPage", "AccessRule", "AccessRuleInput"} {
		if !strings.Contains(string(raw), `"`+name+`"`) {
			t.Errorf("OpenAPI lacks schema %s", name)
		}
	}
}

// D38 (ag): ?role= takes a role ID or a role key; anything else is a 400 on field role, and
// the check happens before any store is reached (the rig's database refuses every statement).
func TestHandlers_ListGrants_BadRole_Returns400(t *testing.T) {
	r := newUnitRig(t, db.None())
	mux := serve(t, r.svc)
	ctx := asSuper(t.Context(), newPerson())
	for _, role := range []string{"Department_Admin", "-leading", "trailing-", "double--hyphen", "has space", strings.Repeat("a", 65)} {
		t.Run(role, func(t *testing.T) {
			w := doReq(mux, ctx, "GET", "/api/access/grants?role="+url.QueryEscape(role), "")
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body %s", w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), `"role"`) {
				t.Errorf("body does not name the role field: %s", w.Body.String())
			}
		})
	}
}

func TestGrantQuery_RoleAndKey_ReturnsInvalid(t *testing.T) {
	q := authz.GrantQuery{Role: authz.RoleID(newPerson()), RoleKey: "department-admin"}
	wantKind(t, q.Validate(), errs.Invalid)
}
