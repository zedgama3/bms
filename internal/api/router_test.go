package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/lib/httpx"
)

func TestNew_MountsUnderPrefix(t *testing.T) {
	r := newRig(t, withRoutes(alphaSet(t)))
	rec := r.do("GET", "/api/alpha/things/42")
	if rec.Code != 200 {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
	}
	if got := decodeMap(t, rec.Body.Bytes())["id"]; got != "42" {
		t.Fatalf("PathValue(id) = %v, want 42", got)
	}
}

func TestNew_TrailingSlashIsExact(t *testing.T) {
	r := newRig(t, withRoutes(alphaSet(t)))
	if rec := r.do("GET", "/api/alpha/"); rec.Code != 200 || rec.Body.String() != "root" {
		t.Fatalf("/api/alpha/ = %d %q", rec.Code, rec.Body.String())
	}
	for _, p := range []string{"/api/alpha/x", "/api/alpha/things/", "/api/alpha"} {
		t.Run(p, func(t *testing.T) {
			rec := r.do("GET", p)
			if rec.Code == http.StatusMovedPermanently || rec.Header().Get("Location") != "" {
				t.Fatalf("redirected: %d %q", rec.Code, rec.Header().Get("Location"))
			}
			wantProblem(t, rec, 404, httpx.CodeNotFound)
		})
	}
}

func TestNew_WildcardAllowed(t *testing.T) {
	r := newRig(t, withRoutes(alphaSet(t)))
	rec := r.do("GET", "/api/alpha/files/a/b/c.txt")
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := decodeMap(t, rec.Body.Bytes())["path"]; got != "a/b/c.txt" {
		t.Fatalf("path = %v", got)
	}
}

func TestNew_CoreSetMountsLikeAModule(t *testing.T) {
	set := setOf(t, "accounts", route("GET", "/ping", module.AccessPublic, textHandler("pong")))
	r := newRig(t, withRoutes(set))
	if rec := r.do("GET", "/api/accounts/ping"); rec.Code != 200 || rec.Body.String() != "pong" {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
}

func TestNew_ShellSetAbsolute(t *testing.T) {
	r := newRig(t, withAuth(noSession()))
	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{"GET", "/healthz", 200},
		{"GET", "/readyz", 200},
		{"GET", "/api/modules", 200},
		{"GET", "/api/openapi.json", 200},
		{"GET", "/api/admin/modules", 401},
	} {
		if rec := r.do(tc.method, tc.path); rec.Code != tc.want {
			t.Errorf("%s %s = %d, want %d", tc.method, tc.path, rec.Code, tc.want)
		}
	}
	// client-errors only takes POST.
	wantProblem(t, r.do("GET", "/api/client-errors"), 405, httpx.CodeMethodNotAllowed)
	for _, p := range []string{"/api/core/", "/api/core/modules", "/api/core/healthz", "/api/core/openapi.json"} {
		wantProblem(t, r.do("GET", p), 404, httpx.CodeNotFound)
	}
}

func TestNew_EmptyRoutes_ServesShellOnly(t *testing.T) {
	r := newRig(t)
	if rec := r.do("GET", "/api/modules"); rec.Code != 200 {
		t.Fatalf("/api/modules = %d", rec.Code)
	}
	wantProblem(t, r.do("GET", "/api/alpha/"), 404, httpx.CodeNotFound)
}

func TestRouter_HeadServedThroughGet(t *testing.T) {
	r := newRig(t, withRoutes(alphaSet(t)))
	// A recorder does not strip HEAD bodies; a real server does.
	ts := httptest.NewServer(r.server().Handler())
	defer ts.Close()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodHead, ts.URL+"/api/alpha/things/1", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	}()
	if resp.StatusCode != 200 {
		t.Fatalf("HEAD = %d, want 200 through the GET route", resp.StatusCode)
	}
}

func TestRouter_UnknownPath_404JSON(t *testing.T) {
	r := newRig(t, withRoutes(alphaSet(t)))
	for _, p := range []string{"/api/alpha/nothing", "/api/zzz", "/api/", "/api/alpha/things/1/extra"} {
		t.Run(p, func(t *testing.T) {
			rec := r.do("GET", p)
			wantProblem(t, rec, 404, httpx.CodeNotFound)
			if rec.Header().Get("Allow") != "" {
				t.Errorf("404 carries Allow: %q", rec.Header().Get("Allow"))
			}
		})
	}
	if _, ok := spanNamed(r.cap, "GET /{unmatched}"); !ok {
		t.Error("unmatched requests must use the route token /{unmatched}")
	}
	for _, s := range requestSpans(r.cap) {
		if s.Attrs["http.route"] != "" && s.Attrs["http.route"] != "/{unmatched}" {
			t.Errorf("span route %q for an unmatched path", s.Attrs["http.route"])
		}
	}
}

func TestRouter_UncleanOrRedirectPath_404NotRedirect(t *testing.T) {
	r := newRig(t, withRoutes(alphaSet(t)))
	for _, p := range []string{"/api/alpha//x", "/api/alpha/./x", "/api/alpha/../alpha/things/1", "/api/alpha", "/api//alpha/", "/api/alpha/things//1"} {
		t.Run(p, func(t *testing.T) {
			rec := r.do("GET", p)
			if rec.Code >= 300 && rec.Code < 400 {
				t.Fatalf("redirect %d to %q", rec.Code, rec.Header().Get("Location"))
			}
			wantProblem(t, rec, 404, httpx.CodeNotFound)
		})
	}
	if _, ok := spanNamed(r.cap, "GET /{unmatched}"); !ok {
		t.Error("redirect-shaped paths must use the route token /{unmatched}")
	}
}

func TestRouter_WrongMethod_405WithAllow(t *testing.T) {
	pub := module.AccessPublic
	set := setOf(t, "alpha",
		route("GET", "/shifts/{id}", pub, textHandler("one")),
		route("POST", "/shifts/bulk", pub, textHandler("bulk")),
	)
	r := newRig(t, withRoutes(set))
	tests := []struct {
		method, path string
		status       int
		allow        string
	}{
		{"PUT", "/api/alpha/shifts/7", 405, "GET, HEAD"},
		{"PUT", "/api/alpha/shifts/bulk", 405, "GET, HEAD, POST"},
		{"DELETE", "/api/alpha/nothing", 404, ""},
		{"POST", "/api/alpha/shifts/7", 405, "GET, HEAD"},
	}
	for _, tc := range tests {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			rec := r.do(tc.method, tc.path)
			if tc.status == 405 {
				wantProblem(t, rec, 405, httpx.CodeMethodNotAllowed)
			} else {
				wantProblem(t, rec, 404, httpx.CodeNotFound)
			}
			if got := rec.Header().Get("Allow"); got != tc.allow {
				t.Fatalf("Allow = %q, want %q", got, tc.allow)
			}
		})
	}
}

func TestRouter_WrongMethod_LogsAtDebugOnly(t *testing.T) {
	r := newRig(t, withRoutes(alphaSet(t)))
	r.do("PUT", "/api/alpha/things/1")
	r.do("GET", "/api/alpha/nope")
	for _, rec := range r.cap.Records() {
		if rec.Level > slogDebug {
			t.Errorf("anonymous 404/405 logged above debug: %+v", rec)
		}
	}
}
