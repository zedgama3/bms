package regional_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
)

// The route set is exactly the five the spec names, mounted by app under /api/regional.
func TestRoutes_AreExactlyTheFive(t *testing.T) {
	r := newRig(t)
	var got []string
	for _, rt := range r.svc.Routes() {
		got = append(got, rt.Method+" "+rt.Pattern)
		if strings.HasPrefix(rt.Pattern, "/api") {
			t.Errorf("%s %s carries the mount prefix; app adds /api/regional", rt.Method, rt.Pattern)
		}
		if rt.Handler == nil {
			t.Errorf("%s %s has no handler", rt.Method, rt.Pattern)
		}
	}
	want := map[string]bool{
		"GET /public": true, "GET /theme.css": true, "GET /logo": true, "PUT /logo": true, "DELETE /logo": true,
	}
	if len(got) != len(want) {
		t.Errorf("routes = %v, want exactly %v", got, want)
	}
	for _, g := range got {
		if !want[g] {
			t.Errorf("unexpected route %q", g)
		}
	}
}

func routeOf(t testing.TB, routes []httpx.Route, method, pattern string) httpx.Route {
	t.Helper()
	for _, rt := range routes {
		if rt.Method == method && rt.Pattern == pattern {
			return rt
		}
	}
	t.Fatalf("no %s %s route", method, pattern)
	return httpx.Route{}
}

// The three GETs stay public where settings had them; the two writes need a session and
// settings.manage.
func TestRoutes_ArePublicWhereSettingsWere(t *testing.T) {
	r := newRig(t)
	routes := r.svc.Routes()
	for _, p := range []string{"/public", "/theme.css", "/logo"} {
		if !routeOf(t, routes, http.MethodGet, p).Public {
			t.Errorf("GET %s must be public", p)
		}
	}
	for _, m := range []string{http.MethodPut, http.MethodDelete} {
		if routeOf(t, routes, m, "/logo").Public {
			t.Errorf("%s /logo must not be public", m)
		}
	}

	t.Run("the public GETs need no session", func(t *testing.T) {
		// No logo is set: 404, not 401 or 403. (GET /public and /theme.css with no session
		// are in public_integration_test.go: their ETag needs a real settings revision.)
		w, f := r.do(t.Context(), http.MethodGet, "/logo", "")
		if w.Code != http.StatusNotFound {
			t.Errorf("GET /logo with no session and no logo = %d (%v), want 404", w.Code, f.last())
		}
	})
	t.Run("the writes need a session", func(t *testing.T) {
		for _, m := range []string{http.MethodPut, http.MethodDelete} {
			w, f := r.do(t.Context(), m, "/logo", "")
			if w.Code != http.StatusUnauthorized || !errs.Is(f.last(), errs.Unauthenticated) {
				t.Errorf("%s /logo with no session = %d (%v), want 401", m, w.Code, f.last())
			}
		}
	})
	t.Run("the writes need settings.manage", func(t *testing.T) {
		nobody := asPerson(t.Context(), newPerson())
		for _, m := range []string{http.MethodPut, http.MethodDelete} {
			w, f := r.do(nobody, m, "/logo", "")
			if w.Code != http.StatusForbidden || !errs.Is(f.last(), errs.Permission) {
				t.Errorf("%s /logo without settings.manage = %d (%v), want 403", m, w.Code, f.last())
			}
		}
	})
}
